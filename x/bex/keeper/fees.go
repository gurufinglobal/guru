package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

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

func (k Keeper) AddCollectedFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := fee.Validate(); err != nil || !fee.IsPositive() {
		return types.ErrInvalidRequest.Wrap("fee must be a positive coin")
	}
	if _, err := k.GetActiveExchange(ctx, exchangeID); err != nil {
		return err
	}
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	collected = collected.Add(fee)
	if err := k.collectedFees.Set(ctx, exchangeID, coinsToLedger(collected)); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeFeesCollected,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
	)
	return nil
}

func (k Keeper) LockExchangeFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := fee.Validate(); err != nil || !fee.IsPositive() {
		return types.ErrInvalidRequest.Wrap("fee must be a positive coin")
	}
	if _, err := k.GetActiveExchange(ctx, exchangeID); err != nil {
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
	newLocked := locked.Add(fee)
	if !hasCoins(collected, newLocked) {
		return types.ErrInvariantViolation.Wrap("locked fees exceed collected fees")
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
	if err := fee.Validate(); err != nil || !fee.IsPositive() {
		return types.ErrInvalidRequest.Wrap("fee must be a positive coin")
	}
	locked, err := k.GetLockedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !hasCoins(locked, sdk.NewCoins(fee)) {
		return types.ErrInvariantViolation.Wrap("release exceeds locked fees")
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

func (k Keeper) DeductCollectedFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error {
	if err := fee.Validate(); err != nil || !fee.IsPositive() {
		return types.ErrInvalidRequest.Wrap("fee must be a positive coin")
	}
	available, err := k.GetAvailableFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !hasCoins(available, sdk.NewCoins(fee)) {
		return types.ErrInsufficientAvailableFees.Wrap("deduct exceeds available fees")
	}
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if err := k.collectedFees.Set(ctx, exchangeID, coinsToLedger(collected.Sub(fee))); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeFeesDeducted,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAmount, fee.String()),
	)
	return nil
}

func (k Keeper) WithdrawFees(ctx context.Context, signer string, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if _, _, err := k.requireExchangeAdmin(ctx, exchange, signer); err != nil {
		return err
	}
	if !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("amount must be positive coins")
	}
	available, err := k.GetAvailableFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	if !hasCoins(available, amount) {
		return types.ErrInsufficientAvailableFees.Wrap("withdraw exceeds available fees")
	}
	collected, err := k.GetCollectedFees(ctx, exchangeID)
	if err != nil {
		return err
	}
	recipientString, err := k.accountCodec.BytesToString(recipient)
	if err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, amount); err != nil {
		return err
	}
	if err := k.collectedFees.Set(ctx, exchangeID, coinsToLedger(collected.Sub(amount...))); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeFeesWithdrawn,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipientString),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	)
	return nil
}

func hasCoins(balance sdk.Coins, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}
