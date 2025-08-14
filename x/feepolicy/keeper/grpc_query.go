package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/GPTx-global/guru-v2/x/feepolicy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
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

func (k Keeper) Discounts(c context.Context, _ *types.QueryDiscountsRequest) (*types.QueryDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	discounts, pageRes, err := k.GetPaginatedDiscounts(ctx, &query.PageRequest{})
	if err != nil {
		return nil, err
	}
	return &types.QueryDiscountsResponse{Discounts: discounts, Pagination: pageRes}, nil
}

func (k Keeper) Discount(c context.Context, req *types.QueryDiscountRequest) (*types.QueryDiscountResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	discount, ok := k.GetAccountDiscounts(ctx, req.Address)
	if !ok {
		return nil, errorsmod.Wrap(sdkerrors.ErrNotFound, "discount not found")
	}
	return &types.QueryDiscountResponse{Discount: discount}, nil
}
