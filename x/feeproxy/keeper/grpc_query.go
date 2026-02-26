package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

var _ types.QueryServer = Keeper{}

// Params implements the Query/Params gRPC method.
func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

func (k Keeper) ModeratorAddress(c context.Context, _ *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryModeratorAddressResponse{ModeratorAddress: moderator}, nil
}

func (k Keeper) AdminAddress(c context.Context, _ *types.QueryAdminAddressRequest) (*types.QueryAdminAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryAdminAddressResponse{AdminAddress: params.AdminAddress}, nil
}

func (k Keeper) IsAdmin(c context.Context, req *types.QueryIsAdminRequest) (*types.QueryIsAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryIsAdminResponse{IsAdmin: req.Address == params.AdminAddress}, nil
}

func (k Keeper) FeePercentage(c context.Context, _ *types.QueryFeePercentageRequest) (*types.QueryFeePercentageResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryFeePercentageResponse{FeePercentage: params.FeePercentage}, nil
}

func (k Keeper) ReserveAddress(c context.Context, _ *types.QueryReserveAddressRequest) (*types.QueryReserveAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryReserveAddressResponse{ReserveAddress: params.ReserveAddress}, nil
}
