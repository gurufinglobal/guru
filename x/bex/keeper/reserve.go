package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

type reserveAllowanceKey struct{}

func (k Keeper) withReserveReceiveAllowance(ctx context.Context, exchangeID uint64) context.Context {
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return sdkCtx.WithValue(reserveAllowanceKey{}, exchangeID)
	}
	// Bank keepers unwrap arbitrary context wrappers back to sdk.Context before
	// executing send restrictions. Store the allowance in that SDK context while
	// retaining outer values/deadlines so it survives the unwrap boundary.
	return sdk.UnwrapSDKContext(ctx).WithContext(ctx).WithValue(reserveAllowanceKey{}, exchangeID)
}

// WithReserveReceiveAllowance grants a context-scoped receive capability for a
// trusted module integration. It is keeper-only and must not be exposed through
// a user-facing Msg or query endpoint; wallet deposits use DepositReserve.
func (k Keeper) WithReserveReceiveAllowance(ctx context.Context, exchangeID uint64) context.Context {
	return k.withReserveReceiveAllowance(ctx, exchangeID)
}

func reserveAllowance(ctx context.Context) (uint64, bool) {
	exchangeID, ok := ctx.Value(reserveAllowanceKey{}).(uint64)
	return exchangeID, ok
}

func (k Keeper) RegisterSendRestriction() {
	if k.bankKeeper != nil {
		k.bankKeeper.AppendSendRestriction(k.SendRestrictionFn)
	}
}

func (k Keeper) AddReserveDepositor(ctx context.Context, signer string, exchangeID uint64, depositor string) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	admin, _, err := k.requireExchangeAdmin(ctx, exchange, signer)
	if err != nil {
		return err
	}
	canonical, _, err := k.canonicalAddress(depositor)
	if err != nil {
		return types.ErrInvalidRequest.Wrapf("invalid depositor address: %v", err)
	}
	key := collections.Join(exchangeID, canonical)
	has, err := k.reserveDepositors.Has(ctx, key)
	if err != nil {
		return err
	}
	if has {
		return types.ErrInvalidRequest.Wrap("reserve depositor already registered")
	}
	if err := k.reserveDepositors.Set(ctx, key); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeReserveDepositorAdded,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, admin),
		sdk.NewAttribute(types.AttributeKeyDepositor, canonical),
	)
	return nil
}

func (k Keeper) RemoveReserveDepositor(ctx context.Context, signer string, exchangeID uint64, depositor string) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	admin, _, err := k.requireExchangeAdmin(ctx, exchange, signer)
	if err != nil {
		return err
	}
	canonical, _, err := k.canonicalAddress(depositor)
	if err != nil {
		return types.ErrInvalidRequest.Wrapf("invalid depositor address: %v", err)
	}
	key := collections.Join(exchangeID, canonical)
	has, err := k.reserveDepositors.Has(ctx, key)
	if err != nil {
		return err
	}
	if !has {
		return types.ErrUnauthorizedReserveDepositor.Wrap("reserve depositor is not registered")
	}
	if err := k.reserveDepositors.Remove(ctx, key); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeReserveDepositorRemoved,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, admin),
		sdk.NewAttribute(types.AttributeKeyDepositor, canonical),
	)
	return nil
}

func (k Keeper) IsReserveDepositor(ctx context.Context, exchangeID uint64, depositor string) (bool, error) {
	canonical, _, err := k.canonicalAddress(depositor)
	if err != nil {
		return false, types.ErrInvalidRequest.Wrapf("invalid depositor address: %v", err)
	}
	return k.reserveDepositors.Has(ctx, collections.Join(exchangeID, canonical))
}

func (k Keeper) SendRestrictionFn(ctx context.Context, _ sdk.AccAddress, toAddr sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
	to, err := k.accountCodec.BytesToString(toAddr)
	if err != nil {
		return nil, err
	}
	exchangeID, err := k.reserveByAddress.Get(ctx, to)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return toAddr, nil
		}
		return nil, err
	}
	allowedExchangeID, ok := reserveAllowance(ctx)
	if !ok || allowedExchangeID != exchangeID {
		return nil, types.ErrDirectReserveTransfer.Wrapf("reserve %s belongs to exchange %d", to, exchangeID)
	}
	return toAddr, nil
}

func (k Keeper) DepositReserve(ctx context.Context, signer string, exchangeID uint64, amount sdk.Coins) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	canonical, signerAddr, err := k.canonicalAddress(signer)
	if err != nil {
		return types.ErrUnauthorizedReserveDepositor.Wrapf("invalid depositor address: %v", err)
	}
	authorized := false
	if canonical == exchange.GetAdminAddress() {
		authorized, err = k.admins.Has(ctx, canonical)
		if err != nil {
			return err
		}
	}
	if !authorized {
		authorized, err = k.reserveDepositors.Has(ctx, collections.Join(exchangeID, canonical))
		if err != nil {
			return err
		}
	}
	if !authorized {
		return types.ErrUnauthorizedReserveDepositor.Wrap("signer is not exchange admin or reserve depositor")
	}
	if !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("amount must be positive coins")
	}
	reserveAddr, err := k.accountCodec.StringToBytes(exchange.GetReserveAddress())
	if err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid reserve address: %v", err)
	}
	allowedCtx := k.withReserveReceiveAllowance(ctx, exchangeID)
	if err := k.bankKeeper.SendCoins(allowedCtx, signerAddr, sdk.AccAddress(reserveAddr), amount); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeReserveDeposited,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyDepositor, canonical),
		sdk.NewAttribute(types.AttributeKeyReserveAddress, exchange.GetReserveAddress()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	)
	return nil
}

func (k Keeper) WithdrawReserve(ctx context.Context, signer string, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE {
		return types.ErrInvalidRequest.Wrap("reserve withdraw requires inactive exchange")
	}
	if _, _, err := k.requireExchangeAdmin(ctx, exchange, signer); err != nil {
		return err
	}
	if !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("amount must be positive coins")
	}
	reserveAddr, err := k.accountCodec.StringToBytes(exchange.GetReserveAddress())
	if err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid reserve address: %v", err)
	}
	balances := k.bankKeeper.GetAllBalances(ctx, sdk.AccAddress(reserveAddr))
	if !hasCoins(balances, amount) {
		return types.ErrInsufficientReserve.Wrap("reserve balance too low")
	}
	recipientString, err := k.accountCodec.BytesToString(recipient)
	if err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoins(ctx, sdk.AccAddress(reserveAddr), recipient, amount); err != nil {
		return err
	}
	emitEvent(
		ctx,
		types.EventTypeReserveWithdrawn,
		exchangeIDAttr(exchangeID),
		sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
		sdk.NewAttribute(types.AttributeKeyReserveAddress, exchange.GetReserveAddress()),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipientString),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	)
	return nil
}
