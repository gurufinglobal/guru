package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

type reserveAllowanceKey struct{}

type reserveOutflowAllowanceKey struct{}

type reserveOutflowAllowance struct {
	exchangeID uint64
	recipient  sdk.AccAddress
	amount     sdk.Coins
}

func (k Keeper) withReserveReceiveAllowance(ctx context.Context, exchangeID uint64) context.Context {
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return sdkCtx.WithValue(reserveAllowanceKey{}, exchangeID)
	}
	// Bank keepers unwrap arbitrary context wrappers back to sdk.Context before
	// executing send restrictions. Store the allowance in that SDK context while
	// retaining outer values/deadlines so it survives the unwrap boundary.
	return sdk.UnwrapSDKContext(ctx).WithContext(ctx).WithValue(reserveAllowanceKey{}, exchangeID)
}

func reserveAllowance(ctx context.Context) (uint64, bool) {
	exchangeID, ok := ctx.Value(reserveAllowanceKey{}).(uint64)
	return exchangeID, ok
}

func (k Keeper) withReserveOutflowAllowance(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coins,
) context.Context {
	allowance := reserveOutflowAllowance{
		exchangeID: exchangeID,
		recipient:  append(sdk.AccAddress(nil), recipient...),
		amount:     cloneCoins(amount),
	}
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return sdkCtx.WithValue(reserveOutflowAllowanceKey{}, allowance)
	}
	return sdk.UnwrapSDKContext(ctx).WithContext(ctx).WithValue(reserveOutflowAllowanceKey{}, allowance)
}

func cloneCoins(coins sdk.Coins) sdk.Coins {
	cloned := make(sdk.Coins, len(coins))
	for i, coin := range coins {
		cloned[i] = sdk.Coin{
			Denom:  coin.Denom,
			Amount: sdkmath.NewIntFromBigInt(coin.Amount.BigInt()),
		}
	}
	return cloned
}

func hasReserveOutflowAllowance(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coins,
) bool {
	allowance, ok := ctx.Value(reserveOutflowAllowanceKey{}).(reserveOutflowAllowance)
	return ok &&
		allowance.exchangeID == exchangeID &&
		allowance.recipient.Equals(recipient) &&
		allowance.amount.Equal(amount)
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

func (k Keeper) SendRestrictionFn(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amount sdk.Coins) (sdk.AccAddress, error) {
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	if fromAddr.Equals(moduleAddr) {
		if !hasFeeOutflowAllowance(ctx, toAddr, amount) {
			return nil, types.ErrInvariantViolation.Wrap("BEX module account transfers require an exact scoped fee outflow allowance")
		}
	} else if len(fromAddr) > 0 {
		from, err := k.accountCodec.BytesToString(fromAddr)
		if err != nil {
			return nil, err
		}
		exchangeID, err := k.reserveByAddress.Get(ctx, from)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		if err == nil && !hasReserveOutflowAllowance(ctx, exchangeID, toAddr, amount) {
			return nil, types.ErrDirectReserveTransfer.Wrapf(
				"reserve %s for exchange %d transfers require an exact scoped outflow allowance",
				from,
				exchangeID,
			)
		}
	}
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
	authorized := canonical == exchange.GetAdminAddress()
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
	if err := k.bankKeeper.IsSendEnabledCoins(ctx, amount...); err != nil {
		return err
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

// ReceiveToReserve moves coins into the deterministic reserve through an exact,
// keeper-scoped receive capability. It is intended only for trusted module
// integrations such as transwap receive and callback processing.
func (k Keeper) ReceiveToReserve(ctx context.Context, exchangeID uint64, fromAddr sdk.AccAddress, amount sdk.Coins) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if len(fromAddr) == 0 || !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("reserve receive requires a sender and positive coins")
	}
	reserveAddr := k.GetReserveAddress(ctx, exchangeID)
	reserveAddress, err := k.accountCodec.BytesToString(reserveAddr)
	if err != nil {
		return err
	}
	if exchange.GetReserveAddress() != reserveAddress {
		return types.ErrInvariantViolation.Wrap("exchange reserve address does not match deterministic reserve")
	}
	if k.bankKeeper.BlockedAddr(reserveAddr) {
		return sdkerrors.ErrUnauthorized.Wrapf("%s is not allowed to receive funds", reserveAddr)
	}
	allowedCtx := k.withReserveReceiveAllowance(ctx, exchangeID)
	return k.bankKeeper.SendCoins(allowedCtx, fromAddr, reserveAddr, amount)
}

// SendSwapOutputFromReserve sends a normal swap output without allowing the
// output to consume funds reserved by aggregate refund liabilities.
func (k Keeper) SendSwapOutputFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	if err := validateReserveCoin(amount); err != nil {
		return err
	}
	exchange, reserveAddr, err := k.validateReserveOutflow(ctx, exchangeID, recipient, amount, true)
	if err != nil {
		return err
	}
	if exchange.GetStatus() != types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		return types.ErrInvalidRoute.Wrap("swap output requires an active exchange")
	}
	pending, err := k.GetPendingLiabilities(ctx, exchangeID)
	if err != nil {
		return err
	}
	balance := k.bankKeeper.GetBalance(ctx, reserveAddr, amount.Denom).Amount
	available := balance.Sub(pending.AmountOf(amount.Denom))
	if available.IsNegative() || available.LT(amount.Amount) {
		return types.ErrInsufficientReserve.Wrapf("reserve balance for %s is reserved for pending refunds", amount.Denom)
	}
	coins := sdk.NewCoins(amount)
	allowedCtx := k.withReserveOutflowAllowance(ctx, exchangeID, recipient, coins)
	return k.bankKeeper.SendCoins(allowedCtx, reserveAddr, recipient, coins)
}

// SendRefundFromReserve commits a tracked refund obligation to IBC transport.
// The aggregate liability remains until acknowledgement success or claim.
func (k Keeper) SendRefundFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	if err := validateReserveCoin(amount); err != nil {
		return err
	}
	_, reserveAddr, err := k.validateReserveOutflow(ctx, exchangeID, recipient, amount, true)
	if err != nil {
		return err
	}
	pending, err := k.GetPendingLiabilities(ctx, exchangeID)
	if err != nil {
		return err
	}
	if pending.AmountOf(amount.Denom).LT(amount.Amount) {
		return types.ErrInvariantViolation.Wrap("refund send exceeds pending reserve liability")
	}
	if k.bankKeeper.GetBalance(ctx, reserveAddr, amount.Denom).Amount.LT(amount.Amount) {
		return types.ErrInsufficientReserve.Wrap("reserve balance is less than refund amount")
	}
	coins := sdk.NewCoins(amount)
	allowedCtx := k.withReserveOutflowAllowance(ctx, exchangeID, recipient, coins)
	return k.bankKeeper.SendCoins(allowedCtx, reserveAddr, recipient, coins)
}

// ClaimRefundFromReserve atomically pays a local manual claim and releases the
// corresponding aggregate refund liability. TransSwap authenticates the exact
// refund identity, receiver, and state before calling this trusted boundary.
func (k Keeper) ClaimRefundFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	if err := validateReserveCoin(amount); err != nil {
		return err
	}
	err := executeStateTransition(ctx, func(cacheCtx sdk.Context) error {
		_, reserveAddr, err := k.validateReserveOutflow(cacheCtx, exchangeID, recipient, amount, false)
		if err != nil {
			return err
		}
		if k.bankKeeper.BlockedAddr(recipient) {
			return sdkerrors.ErrUnauthorized.Wrapf("%s is not allowed to receive funds", recipient)
		}
		pending, err := k.GetPendingLiabilities(cacheCtx, exchangeID)
		if err != nil {
			return err
		}
		coins := sdk.NewCoins(amount)
		if !hasCoins(pending, coins) {
			return types.ErrInvariantViolation.Wrap("refund claim exceeds pending reserve liability")
		}
		if k.bankKeeper.GetBalance(cacheCtx, reserveAddr, amount.Denom).Amount.LT(amount.Amount) {
			return types.ErrInsufficientReserve.Wrap("reserve balance is less than refund claim")
		}
		updatedPending := pending.Sub(amount)
		if err := k.pendingLiabilities.Set(cacheCtx, exchangeID, coinsToLedger(updatedPending)); err != nil {
			return err
		}
		allowedCtx := k.withReserveOutflowAllowance(cacheCtx, exchangeID, recipient, coins)
		return k.bankKeeper.SendCoins(allowedCtx, reserveAddr, recipient, coins)
	})
	if err != nil {
		return err
	}
	return nil
}

func validateReserveCoin(amount sdk.Coin) error {
	if err := amount.Validate(); err != nil || !amount.IsPositive() {
		return types.ErrInvalidRequest.Wrap("reserve outflow amount must be a positive coin")
	}
	return nil
}

func (k Keeper) validateReserveOutflow(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
	enforceSendEnabled bool,
) (*types.Exchange, sdk.AccAddress, error) {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return nil, nil, err
	}
	if len(recipient) == 0 {
		return nil, nil, types.ErrInvalidRequest.Wrap("reserve outflow requires a recipient")
	}
	if err := validateExchangeReserveDenom(exchange, amount.Denom); err != nil {
		return nil, nil, err
	}
	if k.bankKeeper == nil {
		return nil, nil, types.ErrInvariantViolation.Wrap("bank keeper is required for reserve outflow")
	}
	if enforceSendEnabled {
		if err := k.bankKeeper.IsSendEnabledCoins(ctx, amount); err != nil {
			return nil, nil, err
		}
	}
	reserveAddr := k.GetReserveAddress(ctx, exchangeID)
	reserveAddress, err := k.accountCodec.BytesToString(reserveAddr)
	if err != nil {
		return nil, nil, err
	}
	if exchange.GetReserveAddress() != reserveAddress {
		return nil, nil, types.ErrInvariantViolation.Wrap("exchange reserve address does not match deterministic reserve")
	}
	return exchange, reserveAddr, nil
}

func (k Keeper) WithdrawReserve(ctx context.Context, signer string, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	exchange, err := k.GetActiveExchange(ctx, exchangeID)
	if err != nil {
		return err
	}
	if exchange.GetStatus() != types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE {
		return types.ErrInvalidRequest.Wrap("reserve withdraw requires inactive exchange")
	}
	if _, _, err := k.requireExchangeAdmin(ctx, exchange, signer); err != nil {
		return err
	}
	if !amount.IsValid() || !amount.IsAllPositive() {
		return types.ErrInvalidRequest.Wrap("amount must be positive coins")
	}
	if err := k.bankKeeper.IsSendEnabledCoins(ctx, amount...); err != nil {
		return err
	}
	if k.bankKeeper.BlockedAddr(recipient) {
		return sdkerrors.ErrUnauthorized.Wrapf("%s is not allowed to receive funds", recipient)
	}
	reserveAddr, err := k.accountCodec.StringToBytes(exchange.GetReserveAddress())
	if err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid reserve address: %v", err)
	}
	balances := k.bankKeeper.GetAllBalances(ctx, sdk.AccAddress(reserveAddr))
	if !hasCoins(balances, amount) {
		return types.ErrInsufficientReserve.Wrap("reserve balance too low")
	}
	pending, err := k.GetPendingLiabilities(ctx, exchangeID)
	if err != nil {
		return err
	}
	for _, coin := range amount {
		available := balances.AmountOf(coin.Denom).Sub(pending.AmountOf(coin.Denom))
		if available.IsNegative() || available.LT(coin.Amount) {
			return types.ErrInsufficientReserve.Wrapf("reserve balance for %s is reserved for pending refunds", coin.Denom)
		}
	}
	recipientString, err := k.accountCodec.BytesToString(recipient)
	if err != nil {
		return err
	}
	withdrawCtx := k.withReserveOutflowAllowance(ctx, exchangeID, recipient, amount)
	if err := k.bankKeeper.SendCoins(withdrawCtx, sdk.AccAddress(reserveAddr), recipient, amount); err != nil {
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
