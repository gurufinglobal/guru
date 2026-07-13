package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type feeOutflowAllowanceKey struct{}

type feeOutflowAllowance struct {
	recipient sdk.AccAddress
	amount    sdk.Coins
}

func (k Keeper) withFeeOutflowAllowance(ctx context.Context, recipient sdk.AccAddress, amount sdk.Coins) context.Context {
	allowance := feeOutflowAllowance{
		recipient: append(sdk.AccAddress(nil), recipient...),
		amount:    append(sdk.Coins(nil), amount...),
	}
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return sdkCtx.WithValue(feeOutflowAllowanceKey{}, allowance)
	}
	return sdk.UnwrapSDKContext(ctx).WithContext(ctx).WithValue(feeOutflowAllowanceKey{}, allowance)
}

func hasFeeOutflowAllowance(ctx context.Context, recipient sdk.AccAddress, amount sdk.Coins) bool {
	allowance, ok := ctx.Value(feeOutflowAllowanceKey{}).(feeOutflowAllowance)
	return ok && allowance.recipient.Equals(recipient) && allowance.amount.Equal(amount)
}

func executeFeeTransition(ctx context.Context, fn func(sdk.Context) error) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if _, ok := ctx.(sdk.Context); !ok {
		// Preserve outer deadlines and context capabilities when a trusted module
		// wraps sdk.Context before entering the fee transition.
		sdkCtx = sdkCtx.WithContext(ctx)
	}
	cacheCtx, write := sdkCtx.CacheContext()
	if err := fn(cacheCtx); err != nil {
		return err
	}
	write()
	return nil
}
