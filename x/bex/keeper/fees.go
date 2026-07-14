package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

func validateFeeCoin(fee sdk.Coin) error {
	if err := fee.Validate(); err != nil || !fee.IsPositive() {
		return types.ErrInvalidRequest.Wrap("fee must be a positive coin")
	}
	return nil
}

func validateExchangeFeeDenom(exchange *bexv1.Exchange, denom string) error {
	if exchange == nil {
		return types.ErrInvariantViolation.Wrap("exchange is nil")
	}
	switch denom {
	case exchange.GetDenomA(), exchange.GetDenomB(), exchange.GetIbcDenomA(), exchange.GetIbcDenomB():
		return nil
	default:
		return types.ErrInvalidRoute.Wrapf("fee denom %q is not configured for exchange %d", denom, exchange.GetId())
	}
}

func validateExchangeFeeCoins(exchange *bexv1.Exchange, coins sdk.Coins) error {
	for _, coin := range coins {
		if err := validateExchangeFeeDenom(exchange, coin.Denom); err != nil {
			return err
		}
	}
	return nil
}

func checkedAddFeeCoin(balance sdk.Coins, fee sdk.Coin) (sdk.Coins, error) {
	nextAmount, err := balance.AmountOf(fee.Denom).SafeAdd(fee.Amount)
	if err != nil {
		return nil, types.ErrInvariantViolation.Wrapf("fee ledger amount for %s exceeds uint256 max", fee.Denom)
	}

	next := make(sdk.Coins, 0, len(balance)+1)
	replaced := false
	for _, coin := range balance {
		if coin.Denom == fee.Denom {
			next = append(next, sdk.Coin{Denom: coin.Denom, Amount: nextAmount})
			replaced = true
			continue
		}
		next = append(next, coin)
	}
	if !replaced {
		next = append(next, fee)
		next = next.Sort()
	}
	return next, nil
}

func (k Keeper) GetCollectedFees(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	ledger, err := k.collectedFees.Get(ctx, exchangeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return sdk.Coins{}, nil
		}
		return nil, err
	}
	return ledgerToCoins(ledger)
}

func (k Keeper) GetLockedFees(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	ledger, err := k.lockedFees.Get(ctx, exchangeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return sdk.Coins{}, nil
		}
		return nil, err
	}
	return ledgerToCoins(ledger)
}

func (k Keeper) GetAvailableFees(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return nil, err
	}
	locked, err := k.GetLockedFees(ctx, exchangeID)
	if err != nil {
		return nil, err
	}
	if !hasCoins(collected, locked) {
		return nil, types.ErrInvariantViolation.Wrap("locked fees exceed collected fees")
	}
	return collected.Sub(locked...), nil
}

// CollectFee atomically moves a fee from the exchange reserve into BEX custody
// and credits the exchange collected-fee ledger.
func (k Keeper) CollectFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := validateFeeCoin(fee); err != nil {
		return err
	}
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
			return types.ErrInvalidRoute.Wrap("fee collection requires an active exchange")
		}
		if err := validateExchangeFeeDenom(exchange, fee.Denom); err != nil {
			return err
		}
		if k.bankKeeper == nil {
			return types.ErrInvariantViolation.Wrap("bank keeper is required for fee collection")
		}
		reserveAddr := k.GetReserveAddress(cacheCtx, exchangeID)
		reserveAddress, err := k.accountCodec.BytesToString(reserveAddr)
		if err != nil {
			return types.ErrInvariantViolation.Wrapf("cannot encode deterministic reserve address: %v", err)
		}
		if exchange.GetReserveAddress() != reserveAddress {
			return types.ErrInvariantViolation.Wrap("exchange reserve address does not match deterministic reserve")
		}
		if k.bankKeeper.GetBalance(cacheCtx, reserveAddr, fee.Denom).Amount.LT(fee.Amount) {
			return types.ErrInsufficientReserve.Wrap("reserve balance is less than fee")
		}
		collected, err := k.GetCollectedFees(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		collected, err = checkedAddFeeCoin(collected, fee)
		if err != nil {
			return err
		}
		if err := k.collectedFees.Set(cacheCtx, exchangeID, coinsToLedger(collected)); err != nil {
			return err
		}
		coins := sdk.NewCoins(fee)
		collectCtx := k.withReserveOutflowAllowance(
			cacheCtx,
			exchangeID,
			authtypes.NewModuleAddress(types.ModuleName),
			coins,
		)
		if err := k.bankKeeper.SendCoinsFromAccountToModule(
			collectCtx,
			reserveAddr,
			types.ModuleName,
			coins,
		); err != nil {
			return err
		}
		emitEvent(
			cacheCtx,
			types.EventTypeFeesCollected,
			exchangeIDAttr(exchangeID),
			sdk.NewAttribute(types.AttributeKeyReserveAddress, reserveAddress),
			sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
		)
		return nil
	})
}

func (k Keeper) LockExchangeFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := validateFeeCoin(fee); err != nil {
		return err
	}
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		return types.ErrInvalidRoute.Wrap("fee lock requires an active exchange")
	}
	if err := validateExchangeFeeDenom(exchange, fee.Denom); err != nil {
		return err
	}
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	locked, err := k.GetLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	newLocked, err := checkedAddFeeCoin(locked, fee)
	if err != nil {
		return err
	}
	if !hasCoins(collected, newLocked) {
		return types.ErrInsufficientAvailableFees.Wrap("lock exceeds available collected fees")
	}
	if err := k.lockedFees.Set(ctx, exchangeID, coinsToLedger(newLocked)); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeFeesLocked,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
	)
	return nil
}

func (k Keeper) ReleaseExchangeFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := validateFeeCoin(fee); err != nil {
		return err
	}
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if err := validateExchangeFeeDenom(exchange, fee.Denom); err != nil {
		return err
	}
	locked, err := k.GetLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !hasCoins(locked, sdk.NewCoins(fee)) {
		return types.ErrInsufficientLockedFees.Wrap("release exceeds locked fees")
	}
	if err := k.lockedFees.Set(ctx, exchangeID, coinsToLedger(locked.Sub(fee))); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeFeesReleased,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
	)
	return nil
}

// RefundLockedFee atomically consumes a locked fee obligation and returns the
// actual coin from BEX custody to the same deterministic exchange reserve.
// Locks are aggregate per exchange/denom; the caller remains responsible for
// packet-keyed exactly-once callback processing.
func (k Keeper) RefundLockedFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := validateFeeCoin(fee); err != nil {
		return err
	}
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if err := validateExchangeFeeDenom(exchange, fee.Denom); err != nil {
			return err
		}
		if k.bankKeeper == nil {
			return types.ErrInvariantViolation.Wrap("bank keeper is required for fee refund")
		}
		collected, err := k.GetCollectedFees(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		locked, err := k.GetLockedFees(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		coins := sdk.NewCoins(fee)
		if !hasCoins(locked, coins) {
			return types.ErrInsufficientLockedFees.Wrap("refund exceeds locked fees")
		}
		if !hasCoins(collected, coins) {
			return types.ErrInvariantViolation.Wrap("refund exceeds collected fees")
		}
		reserveAddr := k.GetReserveAddress(cacheCtx, exchangeID)
		reserveAddress, err := k.accountCodec.BytesToString(reserveAddr)
		if err != nil {
			return types.ErrInvariantViolation.Wrapf("cannot encode deterministic reserve address: %v", err)
		}
		if exchange.GetReserveAddress() != reserveAddress {
			return types.ErrInvariantViolation.Wrap("exchange reserve address does not match deterministic reserve")
		}
		if err := k.collectedFees.Set(cacheCtx, exchangeID, coinsToLedger(collected.Sub(fee))); err != nil {
			return err
		}
		if err := k.lockedFees.Set(cacheCtx, exchangeID, coinsToLedger(locked.Sub(fee))); err != nil {
			return err
		}
		refundCtx := k.withFeeOutflowAllowance(cacheCtx, reserveAddr, coins)
		refundCtx = k.withReserveReceiveAllowance(refundCtx, exchangeID)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(refundCtx, types.ModuleName, reserveAddr, coins); err != nil {
			return err
		}
		emitEvent(
			cacheCtx,
			types.EventTypeFeesRefunded,
			exchangeIDAttr(exchangeID),
			sdk.NewAttribute(types.AttributeKeyReserveAddress, reserveAddress),
			sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
		)
		return nil
	})
}

func (k Keeper) WithdrawFees(ctx context.Context, signer string, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	if !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("amount must be positive coins")
	}
	return executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		exchange, err := k.GetActiveExchange(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if _, _, err := k.requireExchangeAdmin(cacheCtx, exchange, signer); err != nil {
			return err
		}
		if err := validateExchangeFeeCoins(exchange, amount); err != nil {
			return err
		}
		if k.bankKeeper == nil {
			return types.ErrInvariantViolation.Wrap("bank keeper is required for fee withdrawal")
		}
		available, err := k.GetAvailableFees(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		if !hasCoins(available, amount) {
			return types.ErrInsufficientAvailableFees.Wrap("withdraw exceeds available fees")
		}
		collected, err := k.GetCollectedFees(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		recipientString, err := k.accountCodec.BytesToString(recipient)
		if err != nil {
			return err
		}
		if err := k.collectedFees.Set(cacheCtx, exchangeID, coinsToLedger(collected.Sub(amount...))); err != nil {
			return err
		}
		withdrawCtx := k.withFeeOutflowAllowance(cacheCtx, recipient, amount)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(withdrawCtx, types.ModuleName, recipient, amount); err != nil {
			return err
		}
		emitEvent(
			cacheCtx,
			types.EventTypeFeesWithdrawn,
			exchangeIDAttr(exchangeID),
			sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
			sdk.NewAttribute(types.AttributeKeyRecipient, recipientString),
			sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		)
		return nil
	})
}

func hasCoins(balance sdk.Coins, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}
