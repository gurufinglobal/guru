package keeper

import (
	"context"
	"strconv"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ oraclev1.QueryServer = QueryServer{}

const DefaultQueryPageLimit uint64 = 30

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
	tasks, pagination, err := paginateResults(tasks, req.GetPagination())
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryActiveTasksResponse{Tasks: tasks, Pagination: pagination}, nil
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
	values, pagination, err := paginateResults(values, req.GetPagination())
	if err != nil {
		return nil, err
	}

	return &oraclev1.QueryLatestValuesResponse{Values: values, Pagination: pagination}, nil
}

func (q QueryServer) History(ctx context.Context, req *oraclev1.QueryHistoryRequest) (*oraclev1.QueryHistoryResponse, error) {
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
	history = &oraclev1.OracleHistory{
		Symbol: history.GetSymbol(),
		Values: values,
	}

	return &oraclev1.QueryHistoryResponse{History: history, Pagination: pagination}, nil
}

func paginateResults[T any](items []T, pageReq *queryv1beta1.PageRequest) ([]T, *queryv1beta1.PageResponse, error) {
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
		return items[:0], &queryv1beta1.PageResponse{Total: total}, nil
	}

	end := offset + limit
	if end < offset || end > total {
		end = total
	}

	var nextKey []byte
	if end < total {
		nextKey = []byte(strconv.FormatUint(end, 10))
	}

	return items[offset:end], &queryv1beta1.PageResponse{
		NextKey: nextKey,
		Total:   total,
	}, nil
}
