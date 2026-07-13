package keepers

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
)

var _ evmtypes.BankKeeper = bexRestrictedEVMBankKeeper{}

// bexRestrictedEVMBankKeeper closes the EVM balance-write path that bypasses
// x/bank SendCoins restrictions. BEX module-account decreases and all direct
// deterministic-reserve balance changes are rejected.
type bexRestrictedEVMBankKeeper struct {
	evmtypes.BankKeeper
	bexKeeper bexkeeper.Keeper
}

func newBEXRestrictedEVMBankKeeper(bankKeeper evmtypes.BankKeeper, bexKeeper bexkeeper.Keeper) evmtypes.BankKeeper {
	return bexRestrictedEVMBankKeeper{BankKeeper: bankKeeper, bexKeeper: bexKeeper}
}

func (k bexRestrictedEVMBankKeeper) UncheckedSetBalance(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin) error {
	if !amount.IsValid() {
		return k.BankKeeper.UncheckedSetBalance(ctx, addr, amount)
	}
	if err := k.bexKeeper.ValidateEVMSetBalance(ctx, addr, amount); err != nil {
		return err
	}
	current := k.GetBalance(ctx, addr, amount.Denom)
	if amount.Amount.GT(current.Amount) {
		delta := amount.Amount.Sub(current.Amount)
		if _, err := k.bexKeeper.SendRestrictionFn(ctx, nil, addr, sdk.NewCoins(sdk.NewCoin(amount.Denom, delta))); err != nil {
			return err
		}
	}
	return k.BankKeeper.UncheckedSetBalance(ctx, addr, amount)
}
