package keeper

import (
	"context"

	"github.com/GPTx-global/guru-v2/x/bex/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// QueryServer implementation
var _ types.QueryServer = Keeper{}

// ModeratorAddress returns the moderator address.
func (k Keeper) ModeratorAddress(c context.Context, _ *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	moderator_address := k.GetModeratorAddress(ctx)

	return &types.QueryModeratorAddressResponse{ModeratorAddress: moderator_address}, nil
}

// Exchanges returns the list of all exchanges.
func (k Keeper) Exchanges(c context.Context, req *types.QueryExchangesRequest) (*types.QueryExchangesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	exchanges, _, err := k.GetPaginatedExchanges(ctx, &query.PageRequest{Limit: query.PaginationMaxLimit})
	if err != nil {
		return nil, err
	}

	return &types.QueryExchangesResponse{Exchanges: exchanges}, nil
}

// IsAdmin checks if the given address is admin
func (k Keeper) IsAdmin(c context.Context, req *types.QueryIsAdminRequest) (*types.QueryIsAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	ok := k.IsAdminRegistered(ctx, req.Address)

	return &types.QueryIsAdminResponse{IsAdmin: ok}, nil
}

// NextExchangeId returns the next exchange id (RegisterExchange msg should match this id).
func (k Keeper) NextExchangeId(c context.Context, _ *types.QueryNextExchangeIdRequest) (*types.QueryNextExchangeIdResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	id, err := k.GetNextExchangeId(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryNextExchangeIdResponse{Id: id}, nil
}

// Ratemeter returns the current ratemeter state
func (k Keeper) Ratemeter(c context.Context, _ *types.QueryRatemeterRequest) (*types.QueryRatemeterResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	ratemeter, err := k.GetRatemeter(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryRatemeterResponse{Ratemeter: *ratemeter}, nil
}
