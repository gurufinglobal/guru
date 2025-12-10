package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

// QueryServer implementation
var _ types.QueryServer = Keeper{}

// ModeratorAddress returns the moderator address.
func (k Keeper) ModeratorAddress(c context.Context, _ *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryModeratorAddressResponse{ModeratorAddress: moderator.Address}, nil
}

// Discounts returns the full list of registered discounts for all accounts.
func (k Keeper) Discounts(c context.Context, _ *types.QueryDiscountsRequest) (*types.QueryDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	discounts, pageRes, err := k.GetPaginatedDiscounts(ctx, &query.PageRequest{})
	if err != nil {
		return nil, err
	}
	return &types.QueryDiscountsResponse{Discounts: discounts, Pagination: pageRes}, nil
}

// Discount returns the discounts for a specific account.
func (k Keeper) Discount(c context.Context, req *types.QueryDiscountRequest) (*types.QueryDiscountResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	discount, ok := k.GetAccountDiscounts(ctx, req.Address)
	if !ok {
		return &types.QueryDiscountResponse{}, nil
	}
	return &types.QueryDiscountResponse{Discount: discount}, nil
}
