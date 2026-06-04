package keeper

import (
	"context"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ oraclev1.QueryServer = QueryServer{}

type QueryServer struct {
	oraclev1.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) QueryServer {
	return QueryServer{keeper: keeper}
}

func (q QueryServer) Params(ctx context.Context, req *oraclev1.QueryParamsRequest) (*oraclev1.QueryParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryParamsResponse{Params: params}, nil
}

func (q QueryServer) ActiveTasks(ctx context.Context, req *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	tasks, err := q.keeper.ListTasks(ctx, true)
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryActiveTasksResponse{Tasks: tasks}, nil
}

func (q QueryServer) Task(ctx context.Context, req *oraclev1.QueryTaskRequest) (*oraclev1.QueryTaskResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	task, err := q.keeper.GetTask(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryTaskResponse{Task: task}, nil
}

func (q QueryServer) LatestValue(ctx context.Context, req *oraclev1.QueryLatestValueRequest) (*oraclev1.QueryLatestValueResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	value, err := q.keeper.GetLatestValue(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryLatestValueResponse{Value: value}, nil
}

func (q QueryServer) LatestValues(ctx context.Context, req *oraclev1.QueryLatestValuesRequest) (*oraclev1.QueryLatestValuesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	values, err := q.keeper.ListLatestValues(ctx)
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryLatestValuesResponse{Values: values}, nil
}

func (q QueryServer) History(ctx context.Context, req *oraclev1.QueryHistoryRequest) (*oraclev1.QueryHistoryResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	history, err := q.keeper.GetHistory(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryHistoryResponse{History: history}, nil
}
