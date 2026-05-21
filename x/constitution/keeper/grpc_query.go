package keeper

import (
	"context"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
)

var _ constitutionv1.QueryServer = QueryServer{}

// QueryServer contains read-only query handlers for x/constitution.
// Query RPC request/response messages can be wired once query.proto is added.
type QueryServer struct {
	constitutionv1.UnimplementedQueryServer
	keeper *Keeper
}

func NewQueryServer(keeper *Keeper) QueryServer {
	return QueryServer{keeper: keeper}
}

func (q QueryServer) Params(ctx context.Context, req *constitutionv1.QueryParamsRequest) (*constitutionv1.QueryParamsResponse, error) {
	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &constitutionv1.QueryParamsResponse{Params: params}, nil
}
