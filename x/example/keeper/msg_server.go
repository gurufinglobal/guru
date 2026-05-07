package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

var _ exampletypes.MsgServer = Keeper{}

// NewMsgServerImpl은 gRPC Msg service 구현체를 반환한다.
func NewMsgServerImpl(k Keeper) exampletypes.MsgServer {
	return k
}

func (k Keeper) CreateProject(ctx context.Context, msg *exampletypes.MsgCreateProject) (*exampletypes.MsgCreateProjectResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	params, err := k.getParams(ctx)
	if err != nil {
		return nil, err
	}

	ownerIter, err := k.ProjectStore.Indexes.Owner.MatchExact(ctx, msg.Creator)
	if err != nil {
		return nil, err
	}
	defer ownerIter.Close()

	var owned uint32
	for ; ownerIter.Valid(); ownerIter.Next() {
		owned++
	}
	if owned >= params.MaxProjectsPerOwner {
		return nil, errorsmod.Wrapf(
			exampletypes.ErrLimitExceeded,
			"owner already has %d projects (limit=%d)",
			owned,
			params.MaxProjectsPerOwner,
		)
	}

	projectID, err := k.ProjectSequence.Next(ctx)
	if err != nil {
		return nil, err
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	project := exampletypes.Project{
		Id:           projectID,
		Owner:        msg.Creator,
		Name:         strings.TrimSpace(msg.Name),
		Description:  msg.Description,
		MetadataJson: msg.MetadataJson,
		Archived:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := k.ProjectStore.Set(ctx, projectID, project); err != nil {
		if errors.Is(err, collections.ErrConflict) {
			return nil, errorsmod.Wrap(exampletypes.ErrConflict, err.Error())
		}
		return nil, err
	}

	// 소유자는 항상 ADMIN으로 간주하며, 명시 멤버 엔트리도 함께 생성한다.
	memberKey := collections.Join(projectID, msg.Creator)
	member := exampletypes.ProjectMember{
		ProjectId: projectID,
		Address:   msg.Creator,
		Role:      exampletypes.MemberRole_MEMBER_ROLE_ADMIN,
		JoinedAt:  now,
	}
	if err := k.Members.Set(ctx, memberKey, member); err != nil {
		return nil, err
	}
	if err := k.ProjectAdmins.Set(ctx, memberKey); err != nil {
		return nil, err
	}

	if _, err := addItemUint64(ctx, k.ProjectCount, 1); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeCreateProject,
			sdk.NewAttribute(exampletypes.AttributeKeyProjectID, fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute(exampletypes.AttributeKeyOwner, msg.Creator),
		),
	)

	return &exampletypes.MsgCreateProjectResponse{ProjectId: projectID}, nil
}

func (k Keeper) UpdateProject(ctx context.Context, msg *exampletypes.MsgUpdateProject) (*exampletypes.MsgUpdateProjectResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	project, err := k.getProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Editor, exampletypes.MemberRole_MEMBER_ROLE_EDITOR); err != nil {
		return nil, err
	}

	if name := strings.TrimSpace(msg.Name); name != "" {
		project.Name = name
	}
	if msg.Description != "" {
		project.Description = msg.Description
	}
	if msg.MetadataJson != "" {
		project.MetadataJson = msg.MetadataJson
	}
	project.Archived = msg.Archived
	project.UpdatedAt = sdk.UnwrapSDKContext(ctx).BlockTime().UTC()

	if err := k.ProjectStore.Set(ctx, project.Id, project); err != nil {
		if errors.Is(err, collections.ErrConflict) {
			return nil, errorsmod.Wrap(exampletypes.ErrConflict, err.Error())
		}
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeUpdateProject,
			sdk.NewAttribute(exampletypes.AttributeKeyProjectID, fmt.Sprintf("%d", project.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Editor),
		),
	)

	return &exampletypes.MsgUpdateProjectResponse{}, nil
}

func (k Keeper) AddProjectMember(ctx context.Context, msg *exampletypes.MsgAddProjectMember) (*exampletypes.MsgAddProjectMemberResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	project, err := k.getProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Actor, exampletypes.MemberRole_MEMBER_ROLE_ADMIN); err != nil {
		return nil, err
	}

	// owner를 다시 추가하는 요청은 멱등(idempotent)하게 성공 처리한다.
	if msg.Member == project.Owner {
		return &exampletypes.MsgAddProjectMemberResponse{}, nil
	}

	key := collections.Join(project.Id, msg.Member)

	existingMember, err := k.Members.Get(ctx, key)
	exists := err == nil
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	if !exists {
		params, err := k.getParams(ctx)
		if err != nil {
			return nil, err
		}
		count, err := k.projectMemberCount(ctx, project.Id)
		if err != nil {
			return nil, err
		}
		if count >= params.MaxMembersPerProject {
			return nil, errorsmod.Wrapf(
				exampletypes.ErrLimitExceeded,
				"project already has %d members (limit=%d)",
				count,
				params.MaxMembersPerProject,
			)
		}
	}

	joinedAt := sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	if exists {
		joinedAt = existingMember.JoinedAt
	}

	member := exampletypes.ProjectMember{
		ProjectId: project.Id,
		Address:   msg.Member,
		Role:      msg.Role,
		JoinedAt:  joinedAt,
	}
	if err := k.Members.Set(ctx, key, member); err != nil {
		return nil, err
	}

	if msg.Role == exampletypes.MemberRole_MEMBER_ROLE_ADMIN {
		if err := k.ProjectAdmins.Set(ctx, key); err != nil {
			return nil, err
		}
	} else {
		if err := k.ProjectAdmins.Remove(ctx, key); err != nil {
			return nil, err
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeAddProjectMember,
			sdk.NewAttribute(exampletypes.AttributeKeyProjectID, fmt.Sprintf("%d", project.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Actor),
			sdk.NewAttribute(exampletypes.AttributeKeyMember, msg.Member),
			sdk.NewAttribute(exampletypes.AttributeKeyRole, msg.Role.String()),
		),
	)

	return &exampletypes.MsgAddProjectMemberResponse{}, nil
}

func (k Keeper) RemoveProjectMember(ctx context.Context, msg *exampletypes.MsgRemoveProjectMember) (*exampletypes.MsgRemoveProjectMemberResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	project, err := k.getProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Actor, exampletypes.MemberRole_MEMBER_ROLE_ADMIN); err != nil {
		return nil, err
	}
	if msg.Member == project.Owner {
		return nil, errorsmod.Wrap(exampletypes.ErrOwnerRemovalForbidden, "cannot remove project owner")
	}

	key := collections.Join(project.Id, msg.Member)
	hasMember, err := k.Members.Has(ctx, key)
	if err != nil {
		return nil, err
	}
	if !hasMember {
		return nil, errorsmod.Wrapf(exampletypes.ErrMemberNotFound, "project_id=%d member=%s", project.Id, msg.Member)
	}

	if err := k.Members.Remove(ctx, key); err != nil {
		return nil, err
	}
	if err := k.ProjectAdmins.Remove(ctx, key); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeRemoveProjectMember,
			sdk.NewAttribute(exampletypes.AttributeKeyProjectID, fmt.Sprintf("%d", project.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Actor),
			sdk.NewAttribute(exampletypes.AttributeKeyMember, msg.Member),
		),
	)

	return &exampletypes.MsgRemoveProjectMemberResponse{}, nil
}

func (k Keeper) CreateTask(ctx context.Context, msg *exampletypes.MsgCreateTask) (*exampletypes.MsgCreateTaskResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	project, err := k.getProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Creator, exampletypes.MemberRole_MEMBER_ROLE_EDITOR); err != nil {
		return nil, err
	}
	if err := k.ensureAssigneeBelongsToProject(ctx, project, msg.Assignee); err != nil {
		return nil, err
	}

	params, err := k.getParams(ctx)
	if err != nil {
		return nil, err
	}
	taskCount, err := k.projectTaskCount(ctx, project.Id)
	if err != nil {
		return nil, err
	}
	if taskCount >= params.MaxTasksPerProject {
		return nil, errorsmod.Wrapf(
			exampletypes.ErrLimitExceeded,
			"project already has %d tasks (limit=%d)",
			taskCount,
			params.MaxTasksPerProject,
		)
	}

	taskID, err := k.TaskSequence.Next(ctx)
	if err != nil {
		return nil, err
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	task := exampletypes.Task{
		Id:           taskID,
		ProjectId:    project.Id,
		ExternalRef:  normalizeExternalRef(msg.ExternalRef),
		Title:        strings.TrimSpace(msg.Title),
		Description:  msg.Description,
		Creator:      msg.Creator,
		Assignee:     strings.TrimSpace(msg.Assignee),
		Status:       exampletypes.TaskStatus_TASK_STATUS_TODO,
		Priority:     msg.Priority,
		Labels:       normalizeLabels(msg.Labels),
		MetadataJson: msg.MetadataJson,
		DueAt:        canonicalDueAt(msg.DueAt),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := k.Tasks.Set(ctx, taskID, task); err != nil {
		if errors.Is(err, collections.ErrConflict) {
			return nil, errorsmod.Wrap(exampletypes.ErrConflict, err.Error())
		}
		return nil, err
	}
	if err := k.LegacyTaskPriority.Set(ctx, taskID, task.Priority); err != nil {
		return nil, err
	}
	if err := k.setTaskLabelKeys(ctx, task, true); err != nil {
		return nil, err
	}
	if _, err := addItemUint64(ctx, k.TaskCount, 1); err != nil {
		return nil, err
	}
	if err := k.addTaskStatusCount(ctx, task.Status, 1); err != nil {
		return nil, err
	}
	if exampletypes.IsOpenTaskStatus(task.Status) {
		if err := k.addAssigneeOpenTaskCount(ctx, task.Assignee, 1); err != nil {
			return nil, err
		}
	}
	if err := k.RecentTaskIDs.Push(ctx, taskID); err != nil {
		return nil, err
	}

	taskEvent, err := k.appendTaskEvent(ctx, task.ProjectId, task.Id, msg.Creator, "task_created", `{"kind":"create"}`)
	if err != nil {
		return nil, err
	}
	if err := k.EventMessageByEventID.Set(ctx, taskEvent.Id, msg); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeCreateTask,
			sdk.NewAttribute(exampletypes.AttributeKeyProjectID, fmt.Sprintf("%d", task.ProjectId)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskID, fmt.Sprintf("%d", task.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskEventID, fmt.Sprintf("%d", taskEvent.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Creator),
			sdk.NewAttribute(exampletypes.AttributeKeyAssignee, task.Assignee),
		),
	)

	return &exampletypes.MsgCreateTaskResponse{TaskId: taskID}, nil
}

func (k Keeper) AssignTask(ctx context.Context, msg *exampletypes.MsgAssignTask) (*exampletypes.MsgAssignTaskResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	task, err := k.getTask(ctx, msg.TaskId)
	if err != nil {
		return nil, err
	}
	project, err := k.getProject(ctx, task.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Operator, exampletypes.MemberRole_MEMBER_ROLE_EDITOR); err != nil {
		return nil, err
	}
	if err := k.ensureAssigneeBelongsToProject(ctx, project, msg.Assignee); err != nil {
		return nil, err
	}

	oldAssignee := task.Assignee
	newAssignee := strings.TrimSpace(msg.Assignee)

	task.Assignee = newAssignee
	task.UpdatedAt = sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	if err := k.Tasks.Set(ctx, task.Id, task); err != nil {
		return nil, err
	}

	if exampletypes.IsOpenTaskStatus(task.Status) && oldAssignee != newAssignee {
		if err := k.addAssigneeOpenTaskCount(ctx, oldAssignee, -1); err != nil {
			return nil, err
		}
		if err := k.addAssigneeOpenTaskCount(ctx, newAssignee, 1); err != nil {
			return nil, err
		}
	}

	payload := fmt.Sprintf(`{"old_assignee":"%s","new_assignee":"%s"}`, oldAssignee, newAssignee)
	taskEvent, err := k.appendTaskEvent(ctx, task.ProjectId, task.Id, msg.Operator, "task_assigned", payload)
	if err != nil {
		return nil, err
	}
	if err := k.EventMessageByEventID.Set(ctx, taskEvent.Id, msg); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeAssignTask,
			sdk.NewAttribute(exampletypes.AttributeKeyTaskID, fmt.Sprintf("%d", task.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskEventID, fmt.Sprintf("%d", taskEvent.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Operator),
			sdk.NewAttribute(exampletypes.AttributeKeyAssignee, newAssignee),
		),
	)

	return &exampletypes.MsgAssignTaskResponse{}, nil
}

func (k Keeper) UpdateTaskStatus(ctx context.Context, msg *exampletypes.MsgUpdateTaskStatus) (*exampletypes.MsgUpdateTaskStatusResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	task, err := k.getTask(ctx, msg.TaskId)
	if err != nil {
		return nil, err
	}
	project, err := k.getProject(ctx, task.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Operator, exampletypes.MemberRole_MEMBER_ROLE_EDITOR); err != nil {
		return nil, err
	}

	prev := task
	task.Status = msg.Status
	task.UpdatedAt = sdk.UnwrapSDKContext(ctx).BlockTime().UTC()

	if err := k.Tasks.Set(ctx, task.Id, task); err != nil {
		return nil, err
	}

	if prev.Status != task.Status {
		if err := k.addTaskStatusCount(ctx, prev.Status, -1); err != nil {
			return nil, err
		}
		if err := k.addTaskStatusCount(ctx, task.Status, 1); err != nil {
			return nil, err
		}

		if err := k.setTaskLabelKeys(ctx, prev, false); err != nil {
			return nil, err
		}
		if err := k.setTaskLabelKeys(ctx, task, true); err != nil {
			return nil, err
		}

		if prev.Assignee != "" {
			switch {
			case exampletypes.IsOpenTaskStatus(prev.Status) && !exampletypes.IsOpenTaskStatus(task.Status):
				if err := k.addAssigneeOpenTaskCount(ctx, prev.Assignee, -1); err != nil {
					return nil, err
				}
			case !exampletypes.IsOpenTaskStatus(prev.Status) && exampletypes.IsOpenTaskStatus(task.Status):
				if err := k.addAssigneeOpenTaskCount(ctx, prev.Assignee, 1); err != nil {
					return nil, err
				}
			}
		}
	}

	payload := fmt.Sprintf(`{"from":"%s","to":"%s","reason":"%s"}`, prev.Status.String(), task.Status.String(), msg.Reason)
	taskEvent, err := k.appendTaskEvent(ctx, task.ProjectId, task.Id, msg.Operator, "task_status_updated", payload)
	if err != nil {
		return nil, err
	}
	if err := k.EventMessageByEventID.Set(ctx, taskEvent.Id, msg); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeUpdateTaskStatus,
			sdk.NewAttribute(exampletypes.AttributeKeyTaskID, fmt.Sprintf("%d", task.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskEventID, fmt.Sprintf("%d", taskEvent.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Operator),
			sdk.NewAttribute(exampletypes.AttributeKeyStatus, task.Status.String()),
			sdk.NewAttribute(exampletypes.AttributeKeyReason, msg.Reason),
		),
	)

	return &exampletypes.MsgUpdateTaskStatusResponse{}, nil
}

func (k Keeper) AppendTaskEvent(ctx context.Context, msg *exampletypes.MsgAppendTaskEvent) (*exampletypes.MsgAppendTaskEventResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	task, err := k.getTask(ctx, msg.TaskId)
	if err != nil {
		return nil, err
	}
	project, err := k.getProject(ctx, task.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectReadable(ctx, project, msg.Actor); err != nil {
		return nil, err
	}

	taskEvent, err := k.appendTaskEvent(ctx, task.ProjectId, task.Id, msg.Actor, msg.EventType, msg.PayloadJson)
	if err != nil {
		return nil, err
	}
	if err := k.EventMessageByEventID.Set(ctx, taskEvent.Id, msg); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeAppendTaskEvent,
			sdk.NewAttribute(exampletypes.AttributeKeyTaskID, fmt.Sprintf("%d", task.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskEventID, fmt.Sprintf("%d", taskEvent.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Actor),
		),
	)

	return &exampletypes.MsgAppendTaskEventResponse{TaskEventId: taskEvent.Id}, nil
}

func (k Keeper) DeleteTask(ctx context.Context, msg *exampletypes.MsgDeleteTask) (*exampletypes.MsgDeleteTaskResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	task, err := k.getTask(ctx, msg.TaskId)
	if err != nil {
		return nil, err
	}
	project, err := k.getProject(ctx, task.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := k.ensureProjectRole(ctx, project, msg.Operator, exampletypes.MemberRole_MEMBER_ROLE_ADMIN); err != nil {
		return nil, err
	}

	if err := k.setTaskLabelKeys(ctx, task, false); err != nil {
		return nil, err
	}
	if err := k.Tasks.Remove(ctx, task.Id); err != nil {
		return nil, err
	}
	if err := k.LegacyTaskPriority.Remove(ctx, task.Id); err != nil {
		return nil, err
	}

	if _, err := addItemUint64(ctx, k.TaskCount, -1); err != nil {
		return nil, err
	}
	if err := k.addTaskStatusCount(ctx, task.Status, -1); err != nil {
		return nil, err
	}
	if exampletypes.IsOpenTaskStatus(task.Status) {
		if err := k.addAssigneeOpenTaskCount(ctx, task.Assignee, -1); err != nil {
			return nil, err
		}
	}

	taskEvent, err := k.appendTaskEvent(ctx, task.ProjectId, task.Id, msg.Operator, "task_deleted", `{"kind":"delete"}`)
	if err != nil {
		return nil, err
	}
	if err := k.EventMessageByEventID.Set(ctx, taskEvent.Id, msg); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeDeleteTask,
			sdk.NewAttribute(exampletypes.AttributeKeyTaskID, fmt.Sprintf("%d", task.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyTaskEventID, fmt.Sprintf("%d", taskEvent.Id)),
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Operator),
		),
	)

	return &exampletypes.MsgDeleteTaskResponse{}, nil
}

func (k Keeper) MigrateLegacyTaskValues(ctx context.Context, msg *exampletypes.MsgMigrateLegacyTaskValues) (*exampletypes.MsgMigrateLegacyTaskValuesResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if err := k.ensureAuthority(msg.Authority); err != nil {
		return nil, err
	}

	limit := msg.Limit
	if limit == 0 {
		limit = 1000
	}

	// collections.Range를 활용해 범위를 명시적으로 정의한다.
	// 여기서는 전체 범위를 순회하지만, 추후 특정 key 대역만 점진 이전할 때 같은 패턴을 확장한다.
	iter, err := k.LegacyTaskPriority.Iterate(ctx, new(collections.Range[uint64]).StartInclusive(0))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var migrated uint64
	for ; iter.Valid() && migrated < limit; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return nil, err
		}
		// AltValueCodec가 구 포맷을 읽어주고, Set은 canonical 포맷으로 다시 기록한다.
		if err := k.LegacyTaskPriority.Set(ctx, kv.Key, kv.Value); err != nil {
			return nil, errorsmod.Wrap(exampletypes.ErrLegacyMigrationFailure, err.Error())
		}
		migrated++
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeMigrateLegacy,
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Authority),
			sdk.NewAttribute(exampletypes.AttributeKeyMigrated, fmt.Sprintf("%d", migrated)),
		),
	)

	return &exampletypes.MsgMigrateLegacyTaskValuesResponse{MigratedCount: migrated}, nil
}

func (k Keeper) UpdateParams(ctx context.Context, msg *exampletypes.MsgUpdateParams) (*exampletypes.MsgUpdateParamsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if err := k.ensureAuthority(msg.Authority); err != nil {
		return nil, err
	}
	if err := k.setParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			exampletypes.EventTypeUpdateParams,
			sdk.NewAttribute(exampletypes.AttributeKeyActor, msg.Authority),
		),
	)

	return &exampletypes.MsgUpdateParamsResponse{}, nil
}
