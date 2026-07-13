package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
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

// ValidateEVMSetBalance closes low-level EVM balance writes that bypass x/bank
// send restrictions. BEX custody balances may only be changed through explicit
// BEX keeper operations, never through the EVM balance adapter.
func (k Keeper) ValidateEVMSetBalance(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin) error {
	if k.bankKeeper == nil {
		return types.ErrInvariantViolation.Wrap("bank keeper is required for EVM balance validation")
	}
	current := k.bankKeeper.GetBalance(ctx, addr, amount.Denom).Amount
	if addr.Equals(authtypes.NewModuleAddress(types.ModuleName)) {
		if !amount.Amount.Equal(current) {
			return types.ErrInvariantViolation.Wrapf(
				"EVM balance write cannot change BEX module balance for %s",
				amount.Denom,
			)
		}
		return nil
	}
	address, err := k.accountCodec.BytesToString(addr)
	if err != nil {
		return err
	}
	_, err = k.reserveByAddress.Get(ctx, address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if !amount.Amount.Equal(current) {
		return types.ErrDirectReserveTransfer.Wrapf("EVM balance write cannot change BEX reserve %s", address)
	}
	return nil
}
