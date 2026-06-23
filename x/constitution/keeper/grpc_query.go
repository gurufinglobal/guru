package keeper

import (
	"context"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
)

var _ constitutionv1.QueryServer = QueryServer{}

// QueryServer contains read-only query handlers for x/constitution.
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

func (q QueryServer) BaseAddress(ctx context.Context, req *constitutionv1.QueryBaseAddressRequest) (*constitutionv1.QueryBaseAddressResponse, error) {
	baseAddress, err := q.keeper.GetBaseAddress(ctx)
	if err != nil {
		return nil, err
	}

	return &constitutionv1.QueryBaseAddressResponse{BaseAddress: baseAddress}, nil
}

func (q QueryServer) ModeratorAddress(ctx context.Context, req *constitutionv1.QueryModeratorAddressRequest) (*constitutionv1.QueryModeratorAddressResponse, error) {
	moderatorAddress, err := q.keeper.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}

	return &constitutionv1.QueryModeratorAddressResponse{ModeratorAddress: moderatorAddress}, nil
}

func (q QueryServer) SeparationRatio(ctx context.Context, req *constitutionv1.QuerySeparationRatioRequest) (*constitutionv1.QuerySeparationRatioResponse, error) {
	separationRatio, err := q.keeper.GetSeparationRatio(ctx)
	if err != nil {
		return nil, err
	}

	return &constitutionv1.QuerySeparationRatioResponse{SeparationRatio: separationRatio}, nil
}
