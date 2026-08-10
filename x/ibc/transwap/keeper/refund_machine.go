package keeper

import (
	"bytes"
	"errors"
	"math"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func claimAddressFromReceiver(receiver string) (sdk.AccAddress, error) {
	_, address, err := bech32.DecodeAndConvert(receiver)
	if err != nil {
		return nil, types.ErrRefundUnauthorized.Wrapf("refund receiver must be a bech32 account: %v", err)
	}
	if err := sdk.VerifyAddressFormat(address); err != nil {
		return nil, types.ErrRefundUnauthorized.Wrapf("invalid refund receiver bytes: %v", err)
	}
	return sdk.AccAddress(address), nil
}

func (k Keeper) calculateRefundTransportTimeout(
	ctx sdk.Context,
	sourcePort, sourceChannel string,
) (uint64, uint64, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, 0, err
	}
	if k.connectionKeeper == nil || k.clientKeeper == nil {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrap("IBC connection/client keeper is not configured")
	}
	channel, found := k.channelKeeper.GetChannel(ctx, sourcePort, sourceChannel)
	if !found {
		return 0, 0, errorsmod.Wrapf(channeltypes.ErrChannelNotFound, "%s/%s", sourcePort, sourceChannel)
	}
	if len(channel.ConnectionHops) != 1 {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrapf(
			"channel %s/%s must have exactly one connection hop",
			sourcePort,
			sourceChannel,
		)
	}
	connection, found := k.connectionKeeper.GetConnection(ctx, channel.ConnectionHops[0])
	if !found {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrapf("connection %s not found", channel.ConnectionHops[0])
	}
	consensusState, found := k.clientKeeper.GetLatestClientConsensusState(ctx, connection.ClientId)
	if !found || consensusState == nil {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrapf(
			"latest consensus state for client %s not found",
			connection.ClientId,
		)
	}
	// IBC-Go v11 exposes the trusted consensus timestamp through this method;
	// the replacement client API is not available to application keepers yet.
	destinationTimestamp := consensusState.GetTimestamp() //nolint:staticcheck
	if destinationTimestamp == 0 {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrap("destination client timestamp must be positive")
	}
	blockNanos := ctx.BlockTime().UnixNano()
	if blockNanos < 0 {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrap("block time cannot be negative")
	}
	base := max(uint64(blockNanos), destinationTimestamp)
	window := params.GetRefundTimeoutWindow()
	if base > math.MaxUint64-window {
		return 0, 0, types.ErrUnsafeRefundTimeout.Wrap("refund timeout overflows uint64")
	}
	timeout := base + window
	if err := validateRefundTransportTimeout(timeout, destinationTimestamp, params.GetMinRelaySafetyMargin()); err != nil {
		return 0, 0, err
	}
	return timeout, destinationTimestamp, nil
}

func validateRefundTransportTimeout(timeout, destinationTimestamp, safetyMargin uint64) error {
	if timeout <= destinationTimestamp {
		return types.ErrUnsafeRefundTimeout.Wrapf(
			"timeout %d must be greater than destination timestamp %d",
			timeout,
			destinationTimestamp,
		)
	}
	if destinationTimestamp > math.MaxUint64-safetyMargin {
		return types.ErrUnsafeRefundTimeout.Wrap("destination timestamp plus safety margin overflows uint64")
	}
	minimum := destinationTimestamp + safetyMargin
	if timeout <= minimum {
		return types.ErrUnsafeRefundTimeout.Wrapf(
			"timeout %d must be greater than destination timestamp plus safety margin %d",
			timeout,
			minimum,
		)
	}
	return nil
}

func (k Keeper) commitRefundTokensToTransport(ctx sdk.Context, record *types.RefundRecord) error {
	token := types.CloneToken(&record.Token)
	coin, err := types.TokenToCoin(token)
	if err != nil {
		return err
	}
	if err := k.BankKeeper.IsSendEnabledCoins(ctx, coin); err != nil {
		return errorsmod.Wrap(types.ErrSendDisabled, err.Error())
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return err
	}

	if types.DenomHasPrefix(token.Denom, record.GetRefundSourcePort(), record.GetRefundSourceChannel()) {
		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		if err := k.BexKeeper.SendRefundFromReserve(ctx, exchangeID, moduleAddr, coin); err != nil {
			return err
		}
		if err := k.BankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return errorsmod.Wrap(err, "failed to burn refund voucher")
		}
		return nil
	}

	channelEscrow := types.GetEscrowAddress(record.GetRefundSourcePort(), record.GetRefundSourceChannel())
	if err := k.BexKeeper.SendRefundFromReserve(ctx, exchangeID, channelEscrow, coin); err != nil {
		return err
	}
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.Denom)
	k.SetTotalEscrowForDenom(ctx, currentTotalEscrow.Add(coin))
	return nil
}

func (k Keeper) validateActiveRefundPacketData(
	ctx sdk.Context,
	record *types.RefundRecord,
	data types.InternalTransferRepresentation,
) (sdk.Coin, error) {
	kind, err := data.ClassifyPacket()
	if err != nil || kind != types.PacketKindTransfer {
		return sdk.Coin{}, types.ErrRefundEscrowInvariant.Wrap("active refund packet must be a transfer packet")
	}
	coin, err := types.TokenToCoin(data.Token)
	if err != nil {
		return sdk.Coin{}, err
	}
	expected, err := types.TokenToCoin(&record.Token)
	if err != nil {
		return sdk.Coin{}, err
	}
	if !coin.Equal(expected) {
		return sdk.Coin{}, types.ErrRefundEscrowInvariant.Wrapf(
			"active packet token %s does not match refund %s",
			coin,
			expected,
		)
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return sdk.Coin{}, err
	}
	expectedSender := k.BexKeeper.GetReserveAddress(ctx, exchangeID).String()
	if data.GetPacketSender(record.GetRefundSourcePort()) != expectedSender ||
		data.Receiver != record.GetReceiver() || data.Memo != record.GetMemo() {
		return sdk.Coin{}, types.ErrRefundEscrowInvariant.Wrap("active refund packet template changed")
	}
	return coin, nil
}

func (k Keeper) restoreRefundTokensToReserve(
	ctx sdk.Context,
	record *types.RefundRecord,
	data types.InternalTransferRepresentation,
) error {
	coin, err := k.validateActiveRefundPacketData(ctx, record, data)
	if err != nil {
		return err
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(coin)
	if types.DenomHasPrefix(data.Token.Denom, record.GetRefundSourcePort(), record.GetRefundSourceChannel()) {
		if err := k.BankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
			return err
		}
		moduleAddr := k.AuthKeeper.GetModuleAddress(types.ModuleName)
		return k.BexKeeper.ReceiveToReserve(ctx, exchangeID, moduleAddr, coins)
	}

	channelEscrow := types.GetEscrowAddress(record.GetRefundSourcePort(), record.GetRefundSourceChannel())
	currentTotalEscrow := k.GetTotalEscrowForDenom(ctx, coin.Denom)
	if currentTotalEscrow.Amount.LT(coin.Amount) {
		return types.ErrRefundEscrowInvariant.Wrapf("tracked channel escrow for %s is less than refund amount", coin.Denom)
	}
	if err := k.BexKeeper.ReceiveToReserve(ctx, exchangeID, channelEscrow, coins); err != nil {
		return err
	}
	k.SetTotalEscrowForDenom(ctx, currentTotalEscrow.Sub(coin))
	return nil
}

func (k Keeper) attemptRefundPacket(ctx sdk.Context, refundID string) (*types.RefundRecord, error) {
	record, err := k.MustGetRefundRecord(ctx, refundID)
	if err != nil {
		return nil, err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_RETRYABLE {
		return nil, types.ErrInvalidRefundState.Wrapf(
			"refund %s is %s, expected REFUND_RETRYABLE",
			refundID,
			record.GetStatus(),
		)
	}
	if record.GetNextRetryHeight() != 0 {
		blockHeight := ctx.BlockHeight()
		if blockHeight < 0 || uint64(blockHeight) < record.GetNextRetryHeight() { //nolint:gosec // negative height is rejected first.
			return nil, types.ErrRefundRetryNotDue.Wrapf(
				"refund %s is scheduled for height %d, current height %d",
				refundID,
				record.GetNextRetryHeight(),
				blockHeight,
			)
		}
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if record.GetRetryCount() >= params.GetMaxRefundRetries() {
		return nil, types.ErrRefundRetryExhausted.Wrapf("refund %s reached %d attempts", refundID, record.GetRetryCount())
	}

	cacheCtx, writeCache := ctx.CacheContext()
	cached, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return nil, err
	}
	timeout, _, err := k.calculateRefundTransportTimeout(
		cacheCtx,
		cached.GetRefundSourcePort(),
		cached.GetRefundSourceChannel(),
	)
	if err != nil {
		return nil, types.ErrRefundDispatchFailed.Wrap(err.Error())
	}
	if err := k.commitRefundTokensToTransport(cacheCtx, cached); err != nil {
		if errors.Is(err, types.ErrSendDisabled) {
			return nil, types.ErrRefundDispatchFailed.Wrap(err.Error())
		}
		return nil, err
	}

	exchangeID, err := parseExchangeID(cached.GetExchangeId())
	if err != nil {
		return nil, err
	}
	reserveAddress := k.BexKeeper.GetReserveAddress(cacheCtx, exchangeID).String()
	packetData := types.NewFungibleTokenPacketData(
		types.DenomPath(cached.GetToken().Denom),
		cached.GetToken().Amount,
		reserveAddress,
		cached.GetReceiver(),
		cached.GetMemo(),
	)
	if k.ics4Wrapper == nil {
		return nil, types.ErrRefundDispatchFailed.Wrap("IBC packet wrapper is not configured")
	}
	sequence, err := k.ics4Wrapper.SendPacket(
		cacheCtx,
		cached.GetRefundSourcePort(),
		cached.GetRefundSourceChannel(),
		clienttypes.ZeroHeight(),
		timeout,
		types.FungibleTokenPacketDataBytes(packetData),
	)
	if err != nil {
		return nil, types.ErrRefundDispatchFailed.Wrap(err.Error())
	}
	if sequence == 0 {
		return nil, types.ErrRefundDispatchFailed.Wrap("IBC returned a zero refund packet sequence")
	}

	k.clearRefundRetrySchedule(cacheCtx, cached)
	if err := transitionRefundStatus(cached, types.RefundStatus_REFUND_STATUS_IN_FLIGHT); err != nil {
		return nil, err
	}
	cached.ActivePacketSequence = sequence
	cached.ActiveTimeoutTimestamp = timeout
	cached.RetryCount++
	if err := k.SetRefundRecord(cacheCtx, cached); err != nil {
		return nil, err
	}
	if err := k.setActiveRefundPacketIndex(cacheCtx, cached); err != nil {
		return nil, err
	}
	events.EmitTransferEvent(cacheCtx, reserveAddress, cached.GetReceiver(), &cached.Token, cached.GetMemo())
	emitRefundEvent(cacheCtx, types.EventTypeRefundAttempt, cached)

	writeCache()
	return types.CloneRefundRecord(cached), nil
}

// dispatchRefundPackets performs exactly one attempt. A local transport
// failure is persisted in the bounded retry queue for a later block;
// accounting/invariant failures remain transaction errors.
func (k Keeper) dispatchRefundPackets(ctx sdk.Context, refundID string) (*types.RefundRecord, error) {
	record, err := k.attemptRefundPacket(ctx, refundID)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, types.ErrRefundDispatchFailed) {
		return nil, err
	}

	record, recordErr := k.recordRefundDispatchFailure(ctx, refundID, err)
	if recordErr != nil {
		return nil, recordErr
	}
	k.Logger(ctx).Error(
		"refund packet dispatch failed",
		"refund_id", refundID,
		"attempt", record.GetRetryCount(),
		"next_retry_height", record.GetNextRetryHeight(),
		"error", err,
	)
	return record, nil
}

func (k Keeper) recordRefundDispatchFailure(
	ctx sdk.Context,
	refundID string,
	cause error,
) (*types.RefundRecord, error) {
	record, err := k.MustGetRefundRecord(ctx, refundID)
	if err != nil {
		return nil, err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_RETRYABLE {
		return nil, types.ErrInvalidRefundState.Wrapf(
			"refund %s is %s after dispatch failure",
			refundID,
			record.GetStatus(),
		)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if record.GetRetryCount() >= params.GetMaxRefundRetries() {
		return nil, types.ErrRefundRetryExhausted.Wrapf(
			"refund %s reached %d attempts",
			refundID,
			record.GetRetryCount(),
		)
	}
	record.RetryCount++
	if record.GetRetryCount() >= params.GetMaxRefundRetries() {
		if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE); err != nil {
			return nil, err
		}
		k.clearRefundRetrySchedule(ctx, record)
		if err := k.SetRefundRecord(ctx, record); err != nil {
			return nil, err
		}
	} else if err := k.scheduleRefundRetry(ctx, record); err != nil {
		return nil, err
	}
	emitRefundEvent(
		ctx,
		types.EventTypeRefundAttemptFailed,
		record,
		sdk.NewAttribute(types.AttributeKeyFailureReason, cause.Error()),
	)
	return types.CloneRefundRecord(record), nil
}

func (k Keeper) settleSuccessfulOutput(ctx sdk.Context, refundID string) error {
	cacheCtx, writeCache := ctx.CacheContext()
	record, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_PENDING {
		return nil
	}
	fee, err := types.ProtoCoinToSDK(record.GetOriginalFee())
	if err != nil {
		return err
	}
	liability, err := types.TokenToCoin(&record.Token)
	if err != nil {
		return err
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return err
	}
	if fee.IsPositive() {
		if err := k.BexKeeper.ReleaseExchangeFee(cacheCtx, exchangeID, fee); err != nil {
			return err
		}
	}
	if err := k.BexKeeper.ReleasePendingLiability(cacheCtx, exchangeID, liability); err != nil {
		return err
	}
	k.deleteOutputRefundPacketIndex(cacheCtx, record)
	if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_COMPLETED); err != nil {
		return err
	}
	emitRefundEvent(cacheCtx, types.EventTypeRefundStateChanged, record)
	if err := k.deleteCompletedRefundRecord(cacheCtx, record); err != nil {
		return err
	}
	writeCache()
	return nil
}

func (k Keeper) failOriginalOutput(
	ctx sdk.Context,
	refundID, portID, channelID string,
	data types.InternalTransferRepresentation,
) error {
	cacheCtx, writeCache := ctx.CacheContext()
	record, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_PENDING {
		return nil
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return types.ErrRefundEscrowInvariant.Wrap(err.Error())
	}
	if err := k.refundPacketTokensToReserve(cacheCtx, exchangeID, portID, channelID, data); err != nil {
		return err
	}
	fee, err := types.ProtoCoinToSDK(record.GetOriginalFee())
	if err != nil {
		return err
	}
	if fee.IsPositive() {
		if err := k.BexKeeper.RefundLockedFee(cacheCtx, exchangeID, fee); err != nil {
			return err
		}
	}
	if err := k.BexKeeper.ReleaseVolumeWindow(cacheCtx, record.GetVolumeReservation()); err != nil {
		return errorsmod.Wrap(err, "failed to release rejected swap volume reservation")
	}
	k.deleteOutputRefundPacketIndex(cacheCtx, record)
	if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_RETRYABLE); err != nil {
		return err
	}
	if err := k.SetRefundRecord(cacheCtx, record); err != nil {
		return err
	}
	emitRefundEvent(cacheCtx, types.EventTypeRefundStateChanged, record)
	if _, err := k.dispatchRefundPackets(cacheCtx, refundID); err != nil {
		return err
	}

	writeCache()
	return nil
}

func (k Keeper) handleTrackedRefundAcknowledgement(
	ctx sdk.Context,
	portID, channelID string,
	sequence uint64,
	data types.InternalTransferRepresentation,
	ack channeltypes.Acknowledgement,
) (bool, error) {
	record, found, err := k.refundForOutputPacket(ctx, portID, channelID, sequence)
	if err != nil {
		return true, err
	}
	if found {
		switch ack.Response.(type) {
		case *channeltypes.Acknowledgement_Result:
			return true, k.settleSuccessfulOutput(ctx, record.GetId())
		case *channeltypes.Acknowledgement_Error:
			return true, k.failOriginalOutput(ctx, record.GetId(), portID, channelID, data)
		default:
			return true, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid acknowledgement response %T", ack.Response)
		}
	}

	record, found, err = k.refundForActivePacket(ctx, portID, channelID, sequence)
	if err != nil {
		return true, err
	}
	if !found {
		return false, nil
	}
	if !isCurrentActivePacket(record, portID, channelID, sequence) {
		return true, nil
	}
	switch ack.Response.(type) {
	case *channeltypes.Acknowledgement_Result:
		return true, k.completeActiveRefund(ctx, record.GetId(), data)
	case *channeltypes.Acknowledgement_Error:
		return true, k.failActiveRefund(ctx, record.GetId(), data)
	default:
		return true, errorsmod.Wrapf(ibcerrors.ErrInvalidType, "invalid acknowledgement response %T", ack.Response)
	}
}

func (k Keeper) handleTrackedRefundTimeout(
	ctx sdk.Context,
	portID, channelID string,
	sequence uint64,
	data types.InternalTransferRepresentation,
) (bool, error) {
	record, found, err := k.refundForOutputPacket(ctx, portID, channelID, sequence)
	if err != nil {
		return true, err
	}
	if found {
		return true, k.failOriginalOutput(ctx, record.GetId(), portID, channelID, data)
	}

	record, found, err = k.refundForActivePacket(ctx, portID, channelID, sequence)
	if err != nil {
		return true, err
	}
	if !found || !isCurrentActivePacket(record, portID, channelID, sequence) {
		// Missing/stale callbacks are intentionally idempotent no-ops.
		return found, nil
	}
	return true, k.failActiveRefund(ctx, record.GetId(), data)
}

func (k Keeper) completeActiveRefund(
	ctx sdk.Context,
	refundID string,
	data types.InternalTransferRepresentation,
) error {
	cacheCtx, writeCache := ctx.CacheContext()
	record, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_IN_FLIGHT {
		return nil
	}
	if _, err := k.validateActiveRefundPacketData(cacheCtx, record, data); err != nil {
		return err
	}
	liability, err := types.TokenToCoin(&record.Token)
	if err != nil {
		return err
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return err
	}
	if err := k.BexKeeper.ReleasePendingLiability(cacheCtx, exchangeID, liability); err != nil {
		return err
	}
	k.deleteActiveRefundPacketIndex(cacheCtx, record)
	record.ActivePacketSequence = 0
	record.ActiveTimeoutTimestamp = 0
	if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_COMPLETED); err != nil {
		return err
	}
	emitRefundEvent(cacheCtx, types.EventTypeRefundStateChanged, record)
	if err := k.deleteCompletedRefundRecord(cacheCtx, record); err != nil {
		return err
	}
	writeCache()
	return nil
}

func (k Keeper) failActiveRefund(
	ctx sdk.Context,
	refundID string,
	data types.InternalTransferRepresentation,
) error {
	cacheCtx, writeCache := ctx.CacheContext()
	record, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_IN_FLIGHT {
		return nil
	}
	if err := k.restoreRefundTokensToReserve(cacheCtx, record, data); err != nil {
		return err
	}
	k.deleteActiveRefundPacketIndex(cacheCtx, record)
	record.ActivePacketSequence = 0
	record.ActiveTimeoutTimestamp = 0
	params, err := k.GetParams(cacheCtx)
	if err != nil {
		return err
	}
	if record.GetRetryCount() >= params.GetMaxRefundRetries() {
		if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE); err != nil {
			return err
		}
		if err := k.SetRefundRecord(cacheCtx, record); err != nil {
			return err
		}
		emitRefundEvent(cacheCtx, types.EventTypeRefundStateChanged, record)
	} else {
		if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_RETRYABLE); err != nil {
			return err
		}
		if err := k.SetRefundRecord(cacheCtx, record); err != nil {
			return err
		}
		emitRefundEvent(cacheCtx, types.EventTypeRefundStateChanged, record)
		if _, err := k.dispatchRefundPackets(cacheCtx, record.GetId()); err != nil {
			return err
		}
	}

	writeCache()
	return nil
}

func (k Keeper) ClaimRefund(ctx sdk.Context, refundID, signer string) (*types.RefundRecord, error) {
	cacheCtx, writeCache := ctx.CacheContext()
	record, err := k.MustGetRefundRecord(cacheCtx, refundID)
	if err != nil {
		return nil, err
	}
	signerAddr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return nil, types.ErrRefundUnauthorized.Wrapf("invalid signer: %v", err)
	}
	claimAddr, err := sdk.AccAddressFromBech32(record.GetClaimAddress())
	if err != nil {
		return nil, types.ErrRefundEscrowInvariant.Wrap("stored claim address is invalid")
	}
	if !bytes.Equal(signerAddr, claimAddr) {
		return nil, types.ErrRefundUnauthorized.Wrap("only the refund receiver may claim")
	}
	if record.GetStatus() == types.RefundStatus_REFUND_STATUS_CLAIMED {
		return types.CloneRefundRecord(record), nil
	}
	if record.GetStatus() == types.RefundStatus_REFUND_STATUS_COMPLETED {
		return nil, types.ErrRefundNotClaimable.Wrap("completed refund cannot be claimed")
	}
	if record.GetStatus() == types.RefundStatus_REFUND_STATUS_IN_FLIGHT {
		return nil, types.ErrRefundNotClaimable.Wrap("in-flight refund cannot be claimed")
	}
	params, err := k.GetParams(cacheCtx)
	if err != nil {
		return nil, err
	}
	if record.GetStatus() == types.RefundStatus_REFUND_STATUS_RETRYABLE &&
		record.GetRetryCount() >= params.GetMaxRefundRetries() {
		if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE); err != nil {
			return nil, err
		}
		k.clearRefundRetrySchedule(cacheCtx, record)
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE {
		return nil, types.ErrRefundNotClaimable.Wrapf("refund status is %s", record.GetStatus())
	}
	if k.BankKeeper.BlockedAddr(signerAddr) {
		return nil, types.ErrRefundUnauthorized.Wrap("claim address is blocked")
	}
	coin, err := types.TokenToCoin(&record.Token)
	if err != nil {
		return nil, err
	}
	exchangeID, err := parseExchangeID(record.GetExchangeId())
	if err != nil {
		return nil, err
	}
	if err := k.BexKeeper.ClaimRefundFromReserve(cacheCtx, exchangeID, signerAddr, coin); err != nil {
		return nil, err
	}
	if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_CLAIMED); err != nil {
		return nil, err
	}
	if err := k.SetRefundRecord(cacheCtx, record); err != nil {
		return nil, err
	}
	emitRefundEvent(cacheCtx, types.EventTypeRefundClaimed, record)
	writeCache()
	return types.CloneRefundRecord(record), nil
}

func (k Keeper) RetryRefund(ctx sdk.Context, refundID string) (*types.RefundRecord, error) {
	record, err := k.MustGetRefundRecord(ctx, refundID)
	if err != nil {
		return nil, err
	}
	if record.GetStatus() != types.RefundStatus_REFUND_STATUS_RETRYABLE {
		return nil, types.ErrInvalidRefundState.Wrapf(
			"refund %s is %s, expected REFUND_RETRYABLE",
			refundID,
			record.GetStatus(),
		)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if record.GetRetryCount() >= params.GetMaxRefundRetries() {
		if err := transitionRefundStatus(record, types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE); err != nil {
			return nil, err
		}
		k.clearRefundRetrySchedule(ctx, record)
		if err := k.SetRefundRecord(ctx, record); err != nil {
			return nil, err
		}
		emitRefundEvent(ctx, types.EventTypeRefundStateChanged, record)
		return types.CloneRefundRecord(record), nil
	}
	return k.dispatchRefundPackets(ctx, refundID)
}

func isCurrentActivePacket(record *types.RefundRecord, portID, channelID string, sequence uint64) bool {
	return record.GetStatus() == types.RefundStatus_REFUND_STATUS_IN_FLIGHT &&
		record.GetRefundSourcePort() == portID &&
		record.GetRefundSourceChannel() == channelID &&
		record.GetActivePacketSequence() == sequence
}
