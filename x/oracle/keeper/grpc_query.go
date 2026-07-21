package keeper

import (
	"context"
	"strconv"

	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ types.QueryServer = QueryServer{}

const DefaultQueryPageLimit uint64 = 30

type QueryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) QueryServer {
	return QueryServer{keeper: keeper}
}

func (q QueryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

func (q QueryServer) ActiveTasks(ctx context.Context, req *types.QueryActiveTasksRequest) (*types.QueryActiveTasksResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	tasks, err := q.keeper.ListTasks(ctx, true)
	if err != nil {
		return nil, err
	}
	tasks, pagination, err := paginateResults(tasks, req.GetPagination())
	if err != nil {
		return nil, err
	}

	return &types.QueryActiveTasksResponse{Tasks: tasks, Pagination: pagination}, nil
}

func (q QueryServer) Task(ctx context.Context, req *types.QueryTaskRequest) (*types.QueryTaskResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	task, err := q.keeper.GetTask(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}

	return &types.QueryTaskResponse{Task: task}, nil
}

func (q QueryServer) LatestValue(ctx context.Context, req *types.QueryLatestValueRequest) (*types.QueryLatestValueResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	value, err := q.keeper.GetLatestValue(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}

	return &types.QueryLatestValueResponse{Value: value}, nil
}

func (q QueryServer) LatestValues(ctx context.Context, req *types.QueryLatestValuesRequest) (*types.QueryLatestValuesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	values, err := q.keeper.ListLatestValues(ctx)
	if err != nil {
		return nil, err
	}
	values, pagination, err := paginateResults(values, req.GetPagination())
	if err != nil {
		return nil, err
	}

	return &types.QueryLatestValuesResponse{Values: values, Pagination: pagination}, nil
}

func (q QueryServer) History(ctx context.Context, req *types.QueryHistoryRequest) (*types.QueryHistoryResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	history, err := q.keeper.GetHistory(ctx, req.GetSymbol())
	if err != nil {
		return nil, err
	}
	values, pagination, err := paginateResults(history.GetValues(), req.GetPagination())
	if err != nil {
		return nil, err
	}
	history = &types.OracleHistory{
		Symbol: history.GetSymbol(),
		Values: values,
	}

	return &types.QueryHistoryResponse{History: history, Pagination: pagination}, nil
}

func paginateResults[T any](items []T, pageReq *querytypes.PageRequest) ([]T, *querytypes.PageResponse, error) {
	total := uint64(len(items))
	offset := uint64(0)
	limit := DefaultQueryPageLimit
	reverse := false

	if pageReq != nil {
		if key := pageReq.GetKey(); len(key) > 0 {
			parsed, err := strconv.ParseUint(string(key), 10, 64)
			if err != nil {
				return nil, nil, types.ErrInvalidRequest.Wrap("invalid pagination key")
			}
			offset = parsed
		} else {
			offset = pageReq.GetOffset()
		}
		if pageReq.GetLimit() != 0 {
			limit = pageReq.GetLimit()
		}
		reverse = pageReq.GetReverse()
	}

	if reverse {
		reversed := make([]T, len(items))
		for i := range items {
			reversed[len(items)-1-i] = items[i]
		}
		items = reversed
	}

	if offset >= total {
		return items[:0], &querytypes.PageResponse{Total: total}, nil
	}

	end := offset + limit
	if end < offset || end > total {
		end = total
	}

	var nextKey []byte
	if end < total {
		nextKey = []byte(strconv.FormatUint(end, 10))
	}

	return items[offset:end], &querytypes.PageResponse{
		NextKey: nextKey,
		Total:   total,
	}, nil
}
