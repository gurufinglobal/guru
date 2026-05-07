package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

var _ exampletypes.QueryServer = Keeper{}

// NewQueryServerImpl은 gRPC Query service 구현체를 반환한다.
func NewQueryServerImpl(k Keeper) exampletypes.QueryServer {
	return k
}

func (k Keeper) Params(ctx context.Context, _ *exampletypes.QueryParamsRequest) (*exampletypes.QueryParamsResponse, error) {
	params, err := k.getParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &exampletypes.QueryParamsResponse{Params: params}, nil
}

func (k Keeper) Project(ctx context.Context, req *exampletypes.QueryProjectRequest) (*exampletypes.QueryProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.ProjectId == 0 {
		return nil, status.Error(codes.InvalidArgument, "project_id must be > 0")
	}

	project, err := k.ProjectStore.Get(ctx, req.ProjectId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryProjectResponse{Project: project}, nil
}

func (k Keeper) Projects(ctx context.Context, req *exampletypes.QueryProjectsRequest) (*exampletypes.QueryProjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Owner != "" {
		if err := exampletypes.ValidateAddress(req.Owner); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		projects, pageRes, err := sdkquery.CollectionFilteredPaginate(
			ctx,
			k.ProjectStore.Indexes.Owner,
			req.Pagination,
			func(key collections.Pair[string, uint64], _ collections.NoValue) (bool, error) {
				project, err := k.ProjectStore.Get(ctx, key.K2())
				if err != nil {
					return false, err
				}
				return req.IncludeArchived || !project.Archived, nil
			},
			func(key collections.Pair[string, uint64], _ collections.NoValue) (exampletypes.Project, error) {
				return k.ProjectStore.Get(ctx, key.K2())
			},
			sdkquery.WithCollectionPaginationPairPrefix[string, uint64](req.Owner),
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &exampletypes.QueryProjectsResponse{Projects: projects, Pagination: pageRes}, nil
	}

	projects, pageRes, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		k.ProjectStore,
		req.Pagination,
		func(_ uint64, project exampletypes.Project) (bool, error) {
			return req.IncludeArchived || !project.Archived, nil
		},
		func(_ uint64, project exampletypes.Project) (exampletypes.Project, error) {
			return project, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryProjectsResponse{Projects: projects, Pagination: pageRes}, nil
}

func (k Keeper) ProjectMembers(ctx context.Context, req *exampletypes.QueryProjectMembersRequest) (*exampletypes.QueryProjectMembersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.ProjectId == 0 {
		return nil, status.Error(codes.InvalidArgument, "project_id must be > 0")
	}
	if _, err := k.getProject(ctx, req.ProjectId); err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	members, pageRes, err := sdkquery.CollectionPaginate(
		ctx,
		k.Members,
		req.Pagination,
		func(_ collections.Pair[uint64, string], member exampletypes.ProjectMember) (exampletypes.ProjectMember, error) {
			return member, nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[uint64, string](req.ProjectId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryProjectMembersResponse{Members: members, Pagination: pageRes}, nil
}

func (k Keeper) Task(ctx context.Context, req *exampletypes.QueryTaskRequest) (*exampletypes.QueryTaskResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.TaskId == 0 {
		return nil, status.Error(codes.InvalidArgument, "task_id must be > 0")
	}

	task, err := k.Tasks.Get(ctx, req.TaskId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTaskResponse{Task: task}, nil
}

func (k Keeper) TaskByExternalRef(ctx context.Context, req *exampletypes.QueryTaskByExternalRefRequest) (*exampletypes.QueryTaskByExternalRefResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	ref := normalizeExternalRef(req.ExternalRef)
	if ref == "" {
		return nil, status.Error(codes.InvalidArgument, "external_ref is empty")
	}

	taskID, err := k.Tasks.Indexes.ExternalRef.MatchExact(ctx, ref)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	task, err := k.Tasks.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTaskByExternalRefResponse{Task: task}, nil
}

func (k Keeper) TasksByProject(ctx context.Context, req *exampletypes.QueryTasksByProjectRequest) (*exampletypes.QueryTasksByProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.ProjectId == 0 {
		return nil, status.Error(codes.InvalidArgument, "project_id must be > 0")
	}
	if _, err := k.getProject(ctx, req.ProjectId); err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	assignee := strings.TrimSpace(req.Assignee)
	if assignee != "" {
		if err := exampletypes.ValidateAddress(assignee); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	if req.StatusFilter != exampletypes.TaskStatus_TASK_STATUS_UNSPECIFIED && !req.StatusFilter.IsValid() {
		return nil, status.Error(codes.InvalidArgument, "invalid status_filter")
	}

	tasks, pageRes, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		k.Tasks.Indexes.Project,
		req.Pagination,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (bool, error) {
			task, err := k.Tasks.Get(ctx, key.K2())
			if err != nil {
				return false, err
			}
			if req.StatusFilter != exampletypes.TaskStatus_TASK_STATUS_UNSPECIFIED && task.Status != req.StatusFilter {
				return false, nil
			}
			if assignee != "" && task.Assignee != assignee {
				return false, nil
			}
			return true, nil
		},
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (exampletypes.Task, error) {
			return k.Tasks.Get(ctx, key.K2())
		},
		sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.ProjectId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTasksByProjectResponse{Tasks: tasks, Pagination: pageRes}, nil
}

func (k Keeper) TasksByAssignee(ctx context.Context, req *exampletypes.QueryTasksByAssigneeRequest) (*exampletypes.QueryTasksByAssigneeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if err := exampletypes.ValidateAddress(req.Assignee); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tasks, pageRes, err := sdkquery.CollectionPaginate(
		ctx,
		k.Tasks.Indexes.Assignee,
		req.Pagination,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (exampletypes.Task, error) {
			return k.Tasks.Get(ctx, key.K2())
		},
		sdkquery.WithCollectionPaginationPairPrefix[string, uint64](req.Assignee),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTasksByAssigneeResponse{Tasks: tasks, Pagination: pageRes}, nil
}

func (k Keeper) TasksByStatus(ctx context.Context, req *exampletypes.QueryTasksByStatusRequest) (*exampletypes.QueryTasksByStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if !req.Status.IsValid() {
		return nil, status.Error(codes.InvalidArgument, "invalid status")
	}

	tasks, pageRes, err := sdkquery.CollectionPaginate(
		ctx,
		k.Tasks.Indexes.Status,
		req.Pagination,
		func(key collections.Pair[exampletypes.TaskStatus, uint64], _ collections.NoValue) (exampletypes.Task, error) {
			return k.Tasks.Get(ctx, key.K2())
		},
		sdkquery.WithCollectionPaginationPairPrefix[exampletypes.TaskStatus, uint64](req.Status),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTasksByStatusResponse{Tasks: tasks, Pagination: pageRes}, nil
}

func (k Keeper) ExpiringTasks(ctx context.Context, req *exampletypes.QueryExpiringTasksRequest) (*exampletypes.QueryExpiringTasksResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	before := req.Before.UTC()
	if before.IsZero() {
		before = sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
	}

	tasks, pageRes, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		k.Tasks,
		req.Pagination,
		func(_ uint64, task exampletypes.Task) (bool, error) {
			due := canonicalDueAt(task.DueAt)
			return !due.After(before), nil
		},
		func(_ uint64, task exampletypes.Task) (exampletypes.Task, error) {
			return task, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryExpiringTasksResponse{Tasks: tasks, Pagination: pageRes}, nil
}

func (k Keeper) TaskEvents(ctx context.Context, req *exampletypes.QueryTaskEventsRequest) (*exampletypes.QueryTaskEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.TaskId == 0 {
		return nil, status.Error(codes.InvalidArgument, "task_id must be > 0")
	}

	taskEvents, pageRes, err := sdkquery.CollectionPaginate(
		ctx,
		k.TaskEventsByTask,
		req.Pagination,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (exampletypes.TaskEvent, error) {
			event, err := k.TaskEventStore.Get(ctx, key.K2())
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return exampletypes.TaskEvent{}, status.Error(codes.NotFound, "task event not found")
				}
				return exampletypes.TaskEvent{}, err
			}
			return event, nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.TaskId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryTaskEventsResponse{
		TaskEvents: taskEvents,
		Pagination: pageRes,
	}, nil
}

func (k Keeper) Statistics(ctx context.Context, _ *exampletypes.QueryStatisticsRequest) (*exampletypes.QueryStatisticsResponse, error) {
	projectCount, err := getItemUint64(ctx, k.ProjectCount)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	taskCount, err := getItemUint64(ctx, k.TaskCount)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	taskEventCount, err := getItemUint64(ctx, k.TaskEventCount)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	statusCount := func(s exampletypes.TaskStatus) (uint64, error) {
		v, err := k.TaskStatusCounts.Get(ctx, s)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return 0, nil
			}
			return 0, err
		}
		return v, nil
	}

	todoCount, err := statusCount(exampletypes.TaskStatus_TASK_STATUS_TODO)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	inProgressCount, err := statusCount(exampletypes.TaskStatus_TASK_STATUS_IN_PROGRESS)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	blockedCount, err := statusCount(exampletypes.TaskStatus_TASK_STATUS_BLOCKED)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	doneCount, err := statusCount(exampletypes.TaskStatus_TASK_STATUS_DONE)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &exampletypes.QueryStatisticsResponse{
		ProjectCount:    projectCount,
		TaskCount:       taskCount,
		TaskEventCount:  taskEventCount,
		TodoCount:       todoCount,
		InProgressCount: inProgressCount,
		BlockedCount:    blockedCount,
		DoneCount:       doneCount,
	}, nil
}
