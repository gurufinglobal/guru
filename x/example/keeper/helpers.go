package keeper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

func normalizeProjectName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeExternalRef(ref string) string {
	return strings.TrimSpace(ref)
}

func normalizeLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(labels))
	out := make([]string, 0, len(labels))

	for _, raw := range labels {
		label := strings.ToLower(strings.TrimSpace(raw))
		if label == "" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}

	return out
}

// canonicalDueAt는 due_at 미지정(zero) 값을 "매우 먼 미래"로 치환한다.
// 이렇게 하면 due_at 기반 정렬/인덱스 조회에서 미지정 작업이 앞쪽으로 몰리는 문제를 피할 수 있다.
func canonicalDueAt(dueAt time.Time) time.Time {
	if dueAt.IsZero() {
		return time.Unix(0, math.MaxInt64).UTC()
	}
	return dueAt.UTC()
}

func taskDueUnixNano(task exampletypes.Task) int64 {
	return canonicalDueAt(task.DueAt).UnixNano()
}

func (k Keeper) ensureAuthority(authority string) error {
	if authority != k.authority {
		return errorsmod.Wrapf(exampletypes.ErrInvalidAuthority, "got=%s expected=%s", authority, k.authority)
	}
	return nil
}

func (k Keeper) getParams(ctx context.Context) (exampletypes.Params, error) {
	params, err := k.ParamsItem.Get(ctx)
	switch {
	case err == nil:
		return params, nil
	case errors.Is(err, collections.ErrNotFound):
		return exampletypes.DefaultParams(), nil
	default:
		return exampletypes.Params{}, err
	}
}

func (k Keeper) setParams(ctx context.Context, params exampletypes.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	return k.ParamsItem.Set(ctx, params)
}

func getItemUint64(ctx context.Context, item collections.Item[uint64]) (uint64, error) {
	v, err := item.Get(ctx)
	switch {
	case err == nil:
		return v, nil
	case errors.Is(err, collections.ErrNotFound):
		return 0, nil
	default:
		return 0, err
	}
}

func addItemUint64(ctx context.Context, item collections.Item[uint64], delta int64) (uint64, error) {
	current, err := getItemUint64(ctx, item)
	if err != nil {
		return 0, err
	}

	var next uint64
	if delta >= 0 {
		next = current + uint64(delta)
	} else {
		decrease := uint64(-delta)
		if decrease > current {
			return 0, fmt.Errorf("%w: counter underflow", exampletypes.ErrConflict)
		}
		next = current - decrease
	}

	if err := item.Set(ctx, next); err != nil {
		return 0, err
	}
	return next, nil
}

func (k Keeper) addTaskStatusCount(ctx context.Context, status exampletypes.TaskStatus, delta int64) error {
	current, err := k.TaskStatusCounts.Get(ctx, status)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if errors.Is(err, collections.ErrNotFound) {
		current = 0
	}

	var next uint64
	if delta >= 0 {
		next = current + uint64(delta)
	} else {
		decrease := uint64(-delta)
		if decrease > current {
			return fmt.Errorf("%w: task status count underflow", exampletypes.ErrConflict)
		}
		next = current - decrease
	}

	if next == 0 {
		return k.TaskStatusCounts.Remove(ctx, status)
	}
	return k.TaskStatusCounts.Set(ctx, status, next)
}

func (k Keeper) addAssigneeOpenTaskCount(ctx context.Context, assignee string, delta int64) error {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" || delta == 0 {
		return nil
	}

	current, err := k.AssigneeOpenTaskCounts.Get(ctx, assignee)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if errors.Is(err, collections.ErrNotFound) {
		current = 0
	}

	var next int64 = int64(current) + delta
	if next <= 0 {
		return k.AssigneeOpenTaskCounts.Remove(ctx, assignee)
	}

	return k.AssigneeOpenTaskCounts.Set(ctx, assignee, uint64(next))
}

func (k Keeper) getProject(ctx context.Context, projectID uint64) (exampletypes.Project, error) {
	project, err := k.ProjectStore.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return exampletypes.Project{}, errorsmod.Wrapf(exampletypes.ErrProjectNotFound, "project_id=%d", projectID)
		}
		return exampletypes.Project{}, err
	}
	return project, nil
}

func (k Keeper) getTask(ctx context.Context, taskID uint64) (exampletypes.Task, error) {
	task, err := k.Tasks.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return exampletypes.Task{}, errorsmod.Wrapf(exampletypes.ErrTaskNotFound, "task_id=%d", taskID)
		}
		return exampletypes.Task{}, err
	}
	return task, nil
}

func (k Keeper) ensureProjectReadable(ctx context.Context, project exampletypes.Project, actor string) error {
	if actor == project.Owner {
		return nil
	}

	hasMember, err := k.Members.Has(ctx, collections.Join(project.Id, actor))
	if err != nil {
		return err
	}
	if !hasMember {
		return errorsmod.Wrapf(exampletypes.ErrPermissionDenied, "actor %s is not a project member", actor)
	}
	return nil
}

func (k Keeper) ensureProjectRole(
	ctx context.Context,
	project exampletypes.Project,
	actor string,
	minRole exampletypes.MemberRole,
) error {
	if actor == project.Owner {
		return nil
	}

	member, err := k.Members.Get(ctx, collections.Join(project.Id, actor))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return errorsmod.Wrap(exampletypes.ErrPermissionDenied, "actor is not a project member")
		}
		return err
	}

	if !member.Role.IsValid() {
		return errorsmod.Wrap(exampletypes.ErrInvalidRole, "stored role is invalid")
	}
	if member.Role < minRole {
		return errorsmod.Wrapf(
			exampletypes.ErrPermissionDenied,
			"required role %s, got %s",
			minRole.String(),
			member.Role.String(),
		)
	}
	return nil
}

func (k Keeper) ensureAssigneeBelongsToProject(
	ctx context.Context,
	project exampletypes.Project,
	assignee string,
) error {
	if strings.TrimSpace(assignee) == "" {
		return nil
	}
	if assignee == project.Owner {
		return nil
	}

	ok, err := k.Members.Has(ctx, collections.Join(project.Id, assignee))
	if err != nil {
		return err
	}
	if !ok {
		return errorsmod.Wrapf(exampletypes.ErrInvalidRequest, "assignee %s is not a project member", assignee)
	}
	return nil
}

func (k Keeper) projectMemberCount(ctx context.Context, projectID uint64) (uint32, error) {
	var count uint32
	err := k.Members.Walk(
		ctx,
		collections.NewPrefixedPairRange[uint64, string](projectID),
		func(_ collections.Pair[uint64, string], _ exampletypes.ProjectMember) (bool, error) {
			count++
			return false, nil
		},
	)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (k Keeper) projectTaskCount(ctx context.Context, projectID uint64) (uint32, error) {
	iter, err := k.Tasks.Indexes.Project.MatchExact(ctx, projectID)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var count uint32
	for ; iter.Valid(); iter.Next() {
		count++
	}
	return count, nil
}

func (k Keeper) appendTaskEvent(
	ctx context.Context,
	projectID uint64,
	taskID uint64,
	actor string,
	eventType string,
	payloadJSON string,
) (exampletypes.TaskEvent, error) {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}

	eventID, err := k.TaskEventSequence.Next(ctx)
	if err != nil {
		return exampletypes.TaskEvent{}, err
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	event := exampletypes.TaskEvent{
		Id:          eventID,
		TaskId:      taskID,
		ProjectId:   projectID,
		Actor:       actor,
		EventType:   eventType,
		PayloadJson: payloadJSON,
		CreatedAt:   now,
	}

	if err := k.TaskEventStore.Set(ctx, eventID, event); err != nil {
		return exampletypes.TaskEvent{}, err
	}
	if err := k.TaskEventsByTask.Set(ctx, collections.Join(taskID, eventID)); err != nil {
		return exampletypes.TaskEvent{}, err
	}
	if err := k.TaskEventsByProjectTask.Set(ctx, collections.Join3(projectID, taskID, eventID)); err != nil {
		return exampletypes.TaskEvent{}, err
	}
	if _, err := addItemUint64(ctx, k.TaskEventCount, 1); err != nil {
		return exampletypes.TaskEvent{}, err
	}

	return event, nil
}

func (k Keeper) setTaskLabelKeys(ctx context.Context, task exampletypes.Task, add bool) error {
	for _, label := range normalizeLabels(task.Labels) {
		key := collections.Join4(task.ProjectId, label, int32(task.Status), task.Id)
		var err error
		if add {
			err = k.TaskLabelsByProjectStatus.Set(ctx, key)
		} else {
			err = k.TaskLabelsByProjectStatus.Remove(ctx, key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
