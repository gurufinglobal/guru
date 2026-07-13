package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
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

func (k Keeper) sumCollectedFees(ctx context.Context) (sdk.Coins, error) {
	totals := map[string]sdkmath.Int{}
	err := k.collectedFees.Walk(ctx, nil, func(_ uint64, ledger *bexv1.FeeLedger) (bool, error) {
		coins, err := ledgerToCoins(ledger)
		if err != nil {
			return true, err
		}
		if err := accumulateFeeTotals(totals, coins); err != nil {
			return true, err
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return feeTotalsToCoins(totals), nil
}

func accumulateFeeTotals(totals map[string]sdkmath.Int, coins sdk.Coins) error {
	for _, coin := range coins {
		current, ok := totals[coin.Denom]
		if !ok {
			current = sdkmath.ZeroInt()
		}
		next, err := current.SafeAdd(coin.Amount)
		if err != nil {
			return types.ErrInvariantViolation.Wrapf("total collected fees for %s exceed uint256 max", coin.Denom)
		}
		totals[coin.Denom] = next
	}
	return nil
}

func feeTotalsToCoins(totals map[string]sdkmath.Int) sdk.Coins {
	coins := make([]sdk.Coin, 0, len(totals))
	for denom, amount := range totals {
		coins = append(coins, sdk.NewCoin(denom, amount))
	}
	return sdk.NewCoins(coins...)
}

func (k Keeper) assertModuleBalanceCovers(ctx context.Context, liabilities sdk.Coins) error {
	if liabilities.IsZero() {
		return nil
	}
	if k.bankKeeper == nil {
		return types.ErrInvariantViolation.Wrap("bank keeper is required for collected fees")
	}
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	for _, liability := range liabilities {
		balance := k.bankKeeper.GetBalance(ctx, moduleAddr, liability.Denom).Amount
		if balance.LT(liability.Amount) {
			return types.ErrInvariantViolation.Wrapf(
				"module account balance for %s (%s) is less than collected fee liability (%s)",
				liability.Denom,
				balance,
				liability.Amount,
			)
		}
	}
	return nil
}

// AssertFeeSolvency is an explicit O(total fee rows) audit. It is intended for
// genesis, offline audits, and tests, not a per-block lifecycle hook.
func (k Keeper) AssertFeeSolvency(ctx context.Context) error {
	liabilities, err := k.sumCollectedFees(ctx)
	if err != nil {
		return err
	}
	return k.assertModuleBalanceCovers(ctx, liabilities)
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
