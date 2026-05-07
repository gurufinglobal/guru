package keeper

import (
	"context"

	"cosmossdk.io/collections"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

// InitGenesis는 제네시스 상태를 컬렉션 스키마에 적재한다.
// 데모 목적상 "실무형 패턴"을 보여주기 위해 다음을 함께 수행한다:
// - 시퀀스/카운터 복원
// - 보조 인덱스(KeySet/LookupMap) 재구축
// - AltValueCodec 대상 미러 상태 초기화
func (k Keeper) InitGenesis(ctx context.Context, state *exampletypes.GenesisState) error {
	if state == nil {
		state = exampletypes.DefaultGenesisState()
	}
	if err := exampletypes.ValidateGenesis(*state); err != nil {
		return err
	}
	if err := k.setParams(ctx, state.Params); err != nil {
		return err
	}

	// 카운터/집계는 제네시스 객체를 기준으로 재구성한다.
	statusCounts := map[exampletypes.TaskStatus]uint64{}
	assigneeOpenCounts := map[string]uint64{}

	var (
		maxProjectID uint64
		maxTaskID    uint64
		maxEventID   uint64
	)

	for _, p := range state.Projects {
		if err := k.ProjectStore.Set(ctx, p.Id, p); err != nil {
			return err
		}
		if p.Id > maxProjectID {
			maxProjectID = p.Id
		}
	}

	for _, m := range state.Members {
		key := collections.Join(m.ProjectId, m.Address)
		if err := k.Members.Set(ctx, key, m); err != nil {
			return err
		}
		if m.Role == exampletypes.MemberRole_MEMBER_ROLE_ADMIN {
			if err := k.ProjectAdmins.Set(ctx, key); err != nil {
				return err
			}
		}
	}

	for _, t := range state.Tasks {
		task := t
		task.Labels = normalizeLabels(task.Labels)
		task.DueAt = canonicalDueAt(task.DueAt)

		if err := k.Tasks.Set(ctx, task.Id, task); err != nil {
			return err
		}
		if err := k.LegacyTaskPriority.Set(ctx, task.Id, task.Priority); err != nil {
			return err
		}
		if err := k.setTaskLabelKeys(ctx, task, true); err != nil {
			return err
		}
		if err := k.RecentTaskIDs.Push(ctx, task.Id); err != nil {
			return err
		}

		statusCounts[task.Status]++
		if exampletypes.IsOpenTaskStatus(task.Status) && task.Assignee != "" {
			assigneeOpenCounts[task.Assignee]++
		}

		if task.Id > maxTaskID {
			maxTaskID = task.Id
		}
	}

	for _, e := range state.TaskEvents {
		event := e
		if err := k.TaskEventStore.Set(ctx, event.Id, event); err != nil {
			return err
		}
		if err := k.TaskEventsByTask.Set(ctx, collections.Join(event.TaskId, event.Id)); err != nil {
			return err
		}
		if err := k.TaskEventsByProjectTask.Set(ctx, collections.Join3(event.ProjectId, event.TaskId, event.Id)); err != nil {
			return err
		}
		if event.Id > maxEventID {
			maxEventID = event.Id
		}
	}

	if err := k.ProjectCount.Set(ctx, uint64(len(state.Projects))); err != nil {
		return err
	}
	if err := k.TaskCount.Set(ctx, uint64(len(state.Tasks))); err != nil {
		return err
	}
	if err := k.TaskEventCount.Set(ctx, uint64(len(state.TaskEvents))); err != nil {
		return err
	}

	for s, c := range statusCounts {
		if err := k.TaskStatusCounts.Set(ctx, s, c); err != nil {
			return err
		}
	}
	for assignee, c := range assigneeOpenCounts {
		if err := k.AssigneeOpenTaskCounts.Set(ctx, assignee, c); err != nil {
			return err
		}
	}

	nextProjectID := state.NextProjectId
	if nextProjectID == 0 || nextProjectID <= maxProjectID {
		nextProjectID = maxProjectID + 1
	}
	if nextProjectID == 0 {
		nextProjectID = 1
	}

	nextTaskID := state.NextTaskId
	if nextTaskID == 0 || nextTaskID <= maxTaskID {
		nextTaskID = maxTaskID + 1
	}
	if nextTaskID == 0 {
		nextTaskID = 1
	}

	nextEventID := state.NextTaskEventId
	if nextEventID == 0 || nextEventID <= maxEventID {
		nextEventID = maxEventID + 1
	}
	if nextEventID == 0 {
		nextEventID = 1
	}

	if err := k.ProjectSequence.Set(ctx, nextProjectID); err != nil {
		return err
	}
	if err := k.TaskSequence.Set(ctx, nextTaskID); err != nil {
		return err
	}
	if err := k.TaskEventSequence.Set(ctx, nextEventID); err != nil {
		return err
	}

	return nil
}

// ExportGenesis는 현재 상태를 제네시스 구조체로 내보낸다.
func (k Keeper) ExportGenesis(ctx context.Context) (*exampletypes.GenesisState, error) {
	params, err := k.getParams(ctx)
	if err != nil {
		return nil, err
	}

	nextProjectID, err := k.ProjectSequence.Peek(ctx)
	if err != nil {
		return nil, err
	}
	nextTaskID, err := k.TaskSequence.Peek(ctx)
	if err != nil {
		return nil, err
	}
	nextEventID, err := k.TaskEventSequence.Peek(ctx)
	if err != nil {
		return nil, err
	}

	projectIter, err := k.ProjectStore.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	projects, err := projectIter.Values()
	if err != nil {
		return nil, err
	}

	memberIter, err := k.Members.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	members, err := memberIter.Values()
	if err != nil {
		return nil, err
	}

	taskIter, err := k.Tasks.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	tasks, err := taskIter.Values()
	if err != nil {
		return nil, err
	}

	taskEventIter, err := k.TaskEventStore.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	taskEvents, err := taskEventIter.Values()
	if err != nil {
		return nil, err
	}

	return &exampletypes.GenesisState{
		Params:          params,
		NextProjectId:   nextProjectID,
		NextTaskId:      nextTaskID,
		NextTaskEventId: nextEventID,
		Projects:        projects,
		Members:         members,
		Tasks:           tasks,
		TaskEvents:      taskEvents,
	}, nil
}
