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
	return context.WithValue(ctx, reserveAllowanceKey{}, exchangeID)
}

// WithReserveReceiveAllowance marks a context as allowed to receive funds into
// the deterministic reserve for exchangeID through the bank send restriction.
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
	_, signerAddr, err := k.requireExchangeAdmin(ctx, exchange, signer)
	if err != nil {
		return err
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
		sdk.NewAttribute(types.AttributeKeyAdmin, exchange.GetAdminAddress()),
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
