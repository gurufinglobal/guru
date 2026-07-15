package keeper

import (
	"crypto/sha256"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

type refundStateMachineScenario struct {
	state           exchangeReceiveAtomicityState
	refundID        string
	originalTimeout uint64
}

func setupRefundStateMachineScenario(t *testing.T) refundStateMachineScenario {
	t.Helper()

	state := setupExchangeReceiveAtomicity(t, false)
	originalTimeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.
	require.NoError(t, state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		originalTimeout,
	))

	refundID := RefundID(types.PortID, "channel-7", exchangeAtomicSequence)
	record := mustRefundRecord(t, state, refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_PENDING, record.GetStatus())
	require.Equal(t, originalTimeout, record.GetOriginalTimeoutTimestamp())
	require.Equal(t, sdkmath.NewInt(103), pendingRefundAmount(t, state, state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(3), lockedRefundFeeAmount(t, state, state.inputIBCDenom))
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))

	outbound := refundSentPacketData(t, state, exchangeAtomicSequence)
	require.NoError(t, state.keeper.OnAcknowledgementTransferPacket(
		state.ctx,
		types.PortID,
		"channel-7",
		exchangeAtomicSequence,
		outbound,
		channeltypes.NewErrorAcknowledgement(errors.New("target rejected swap output")),
	))

	record = mustRefundRecord(t, state, refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, record.GetStatus())
	require.Equal(t, uint32(1), record.GetRetryCount())
	require.Equal(t, exchangeAtomicSequence+1, record.GetActivePacketSequence())
	require.Equal(t, state.reserve.String(), refundSentPacketData(t, state, record.GetActivePacketSequence()).Sender)
	require.Equal(t, sdkmath.NewInt(103), pendingRefundAmount(t, state, state.inputIBCDenom))
	require.True(t, lockedRefundFeeAmount(t, state, state.inputIBCDenom).IsZero())
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero(), "failed output must release its volume reservation")
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))

	return refundStateMachineScenario{
		state:           state,
		refundID:        refundID,
		originalTimeout: originalTimeout,
	}
}

func TestRefundStateMachineSuccessfulRefundAndDuplicateCallbacks(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	record := mustRefundRecord(t, scenario.state, scenario.refundID)
	activeSequence := record.GetActivePacketSequence()
	activeData := refundSentPacketData(t, scenario.state, activeSequence)

	wantTimeout := uint64(scenario.state.ctx.BlockTime().Add(types.DefaultRefundTimeoutWindow).UnixNano()) //nolint:gosec // fixed test time is positive.
	require.Equal(t, wantTimeout, record.GetActiveTimeoutTimestamp())
	require.NotEqual(t, scenario.originalTimeout, record.GetActiveTimeoutTimestamp())

	_, err := scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.sender.String(),
	)
	require.ErrorIs(t, err, types.ErrRefundNotClaimable)

	require.NoError(t, scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		activeSequence,
		activeData,
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))

	_, found, err := scenario.state.keeper.GetRefundRecord(scenario.state.ctx, scenario.refundID)
	require.NoError(t, err)
	require.False(t, found, "completed refund transport records must be pruned")
	require.True(t, pendingRefundAmount(t, scenario.state, scenario.state.inputIBCDenom).IsZero())

	// Duplicate ACK and timeout callbacks are terminal no-ops.
	require.NoError(t, scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		activeSequence,
		activeData,
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		activeSequence,
		activeData,
	))
	_, found, err = scenario.state.keeper.GetRefundRecord(scenario.state.ctx, scenario.refundID)
	require.NoError(t, err)
	require.False(t, found)

	_, err = scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.sender.String(),
	)
	require.ErrorIs(t, err, types.ErrRefundNotFound)
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}

func TestRefundRetriesRecalculateTimeoutAndRejectStaleCallbacks(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	first := mustRefundRecord(t, scenario.state, scenario.refundID)
	firstSequence := first.GetActivePacketSequence()
	firstTimeout := first.GetActiveTimeoutTimestamp()
	firstData := refundSentPacketData(t, scenario.state, firstSequence)

	baseTime := scenario.state.ctx.BlockTime()
	scenario.state.ctx = scenario.state.ctx.WithBlockTime(baseTime.Add(10 * time.Minute))
	destinationTime := baseTime.Add(12 * time.Minute)
	scenario.state.keeper.WithIBCClientKeepers(
		exchangeAtomicConnectionKeeper{},
		exchangeAtomicClientKeeper{timestamp: destinationTime},
	)
	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		firstSequence,
		firstData,
	))

	second := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, second.GetStatus())
	require.Equal(t, uint32(2), second.GetRetryCount())
	require.NotEqual(t, firstSequence, second.GetActivePacketSequence())
	require.Equal(
		t,
		uint64(destinationTime.Add(types.DefaultRefundTimeoutWindow).UnixNano()), //nolint:gosec // fixed test time is positive.
		second.GetActiveTimeoutTimestamp(),
	)
	require.NotEqual(t, firstTimeout, second.GetActiveTimeoutTimestamp())
	require.Equal(t, scenario.originalTimeout, second.GetOriginalTimeoutTimestamp())

	secondSnapshot := proto.Clone(second).(*transwapv1.RefundRecord)
	sentCount := scenario.state.ics4.sentCount(scenario.state.ctx)
	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		firstSequence,
		firstData,
	))
	require.NoError(t, scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		firstSequence,
		firstData,
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	require.Equal(t, sentCount, scenario.state.ics4.sentCount(scenario.state.ctx))
	require.True(t, proto.Equal(secondSnapshot, mustRefundRecord(t, scenario.state, scenario.refundID)))

	secondData := refundSentPacketData(t, scenario.state, second.GetActivePacketSequence())
	scenario.state.ctx = scenario.state.ctx.WithBlockTime(baseTime.Add(25 * time.Minute))
	scenario.state.keeper.WithIBCClientKeepers(
		exchangeAtomicConnectionKeeper{},
		exchangeAtomicClientKeeper{timestamp: baseTime.Add(20 * time.Minute)},
	)
	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		second.GetActivePacketSequence(),
		secondData,
	))

	third := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, third.GetStatus())
	require.Equal(t, uint32(3), third.GetRetryCount())
	require.Equal(
		t,
		uint64(baseTime.Add(30*time.Minute).UnixNano()), //nolint:gosec // fixed test time is positive.
		third.GetActiveTimeoutTimestamp(),
	)
	require.NotEqual(t, second.GetActiveTimeoutTimestamp(), third.GetActiveTimeoutTimestamp())
	require.Equal(t, scenario.originalTimeout, third.GetOriginalTimeoutTimestamp())
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}

func TestRefundRetryLimitManualClaimAndLateAckCannotDoublePay(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	baseTime := scenario.state.ctx.BlockTime()

	var lastSequence uint64
	var lastData types.InternalTransferRepresentation
	for attempt := 1; attempt <= int(types.DefaultMaxRefundRetries); attempt++ {
		record := mustRefundRecord(t, scenario.state, scenario.refundID)
		require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, record.GetStatus())
		lastSequence = record.GetActivePacketSequence()
		lastData = refundSentPacketData(t, scenario.state, lastSequence)
		scenario.state.ctx = scenario.state.ctx.WithBlockTime(baseTime.Add(time.Duration(attempt) * 10 * time.Minute))
		scenario.state.keeper.WithIBCClientKeepers(
			exchangeAtomicConnectionKeeper{},
			exchangeAtomicClientKeeper{timestamp: scenario.state.ctx.BlockTime()},
		)
		require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
			scenario.state.ctx,
			types.PortID,
			"channel-0",
			lastSequence,
			lastData,
		))
	}

	manual := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, manual.GetStatus())
	require.Equal(t, types.DefaultMaxRefundRetries, manual.GetRetryCount())
	require.Zero(t, manual.GetActivePacketSequence())
	require.Equal(t, 1+int(types.DefaultMaxRefundRetries), scenario.state.ics4.sentCount(scenario.state.ctx))
	require.Equal(t, sdkmath.NewInt(103), reserveRefundAmount(scenario.state, scenario.state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(103), pendingRefundAmount(t, scenario.state, scenario.state.inputIBCDenom))
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))

	_, err := scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.receiver.String(),
	)
	require.ErrorIs(t, err, types.ErrRefundUnauthorized)
	require.True(t, scenario.state.bank.GetAllBalances(scenario.state.ctx, scenario.state.sender).IsZero())

	claimed, err := scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.sender.String(),
	)
	require.NoError(t, err)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, claimed.GetStatus())
	require.Equal(
		t,
		sdkmath.NewInt(103),
		scenario.state.bank.GetAllBalances(scenario.state.ctx, scenario.state.sender).AmountOf(scenario.state.inputIBCDenom),
	)
	require.True(t, pendingRefundAmount(t, scenario.state, scenario.state.inputIBCDenom).IsZero())

	claimedAgain, err := scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.sender.String(),
	)
	require.NoError(t, err)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, claimedAgain.GetStatus())
	require.NoError(t, scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		lastSequence,
		lastData,
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	require.Equal(
		t,
		sdkmath.NewInt(103),
		scenario.state.bank.GetAllBalances(scenario.state.ctx, scenario.state.sender).AmountOf(scenario.state.inputIBCDenom),
	)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, mustRefundRecord(t, scenario.state, scenario.refundID).GetStatus())
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}

func TestPersistentRefundDispatchFailureExhaustsIntoManualClaimWithoutDustingDoS(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	first := mustRefundRecord(t, scenario.state, scenario.refundID)
	firstData := refundSentPacketData(t, scenario.state, first.GetActivePacketSequence())
	scenario.state.ics4.failSendPacket = true

	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		first.GetActivePacketSequence(),
		firstData,
	))
	retryable := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE, retryable.GetStatus())
	require.Equal(t, uint32(2), retryable.GetRetryCount())
	require.Equal(t, uint64(scenario.state.ctx.BlockHeight()+1), retryable.GetNextRetryHeight()) //nolint:gosec // test height is non-negative.
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
	_, err := scenario.state.keeper.RetryRefund(scenario.state.ctx, scenario.refundID)
	require.ErrorIs(t, err, types.ErrRefundRetryNotDue)
	require.Equal(t, retryable, mustRefundRecord(t, scenario.state, scenario.refundID))

	scenario.state.ctx = scenario.state.ctx.WithBlockHeight(int64(retryable.GetNextRetryHeight())) //nolint:gosec // test height is bounded.
	require.NoError(t, scenario.state.keeper.ProcessRefundRetryQueue(scenario.state.ctx))
	manual := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, manual.GetStatus())
	require.Equal(t, types.DefaultMaxRefundRetries, manual.GetRetryCount())
	require.Zero(t, manual.GetNextRetryHeight())
	require.Zero(t, manual.GetActivePacketSequence())
	require.Equal(t, sdkmath.NewInt(103), reserveRefundAmount(scenario.state, scenario.state.inputIBCDenom))
	require.Equal(t, 2, scenario.state.ics4.sentCount(scenario.state.ctx))
	failureReasonCount := 0
	for _, event := range scenario.state.ctx.EventManager().Events() {
		if event.Type != types.EventTypeRefundAttemptFailed {
			continue
		}
		for _, attribute := range event.Attributes {
			if attribute.Key == types.AttributeKeyFailureReason {
				failureReasonCount++
				require.Contains(t, attribute.Value, errExchangeAtomicSendPacket.Error())
			}
		}
	}
	require.Equal(t, 2, failureReasonCount)

	reserveBalances := scenario.state.bank.GetAllBalances(scenario.state.ctx, scenario.state.reserve)
	scenario.state.bank.SetBalance(
		scenario.state.ctx,
		scenario.state.reserve,
		reserveBalances.Add(sdk.NewCoins(
			sdk.NewCoin(scenario.state.inputIBCDenom, sdkmath.OneInt()),
			sdk.NewInt64Coin("adust", 7),
		)...),
	)
	claimed, err := scenario.state.keeper.ClaimRefund(
		scenario.state.ctx,
		scenario.refundID,
		scenario.state.sender.String(),
	)
	require.NoError(t, err)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, claimed.GetStatus())
	require.Equal(t, sdkmath.OneInt(), reserveRefundAmount(scenario.state, scenario.state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(7), reserveRefundAmount(scenario.state, "adust"))
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}

func TestTransientRefundDispatchFailureAutomaticallyRetries(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	first := mustRefundRecord(t, scenario.state, scenario.refundID)
	firstData := refundSentPacketData(t, scenario.state, first.GetActivePacketSequence())
	scenario.state.ics4.failSendCount = 1
	scenario.state.ctx = scenario.state.ctx.WithBlockTime(scenario.state.ctx.BlockTime().Add(15 * time.Minute))
	scenario.state.keeper.WithIBCClientKeepers(
		exchangeAtomicConnectionKeeper{},
		exchangeAtomicClientKeeper{timestamp: scenario.state.ctx.BlockTime()},
	)
	require.NoError(t, scenario.state.keeper.OnTimeoutTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		first.GetActivePacketSequence(),
		firstData,
	))

	retryable := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE, retryable.GetStatus())
	require.Equal(t, uint32(2), retryable.GetRetryCount())
	require.NotZero(t, retryable.GetNextRetryHeight())
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))

	scenario.state.ctx = scenario.state.ctx.WithBlockHeight(int64(retryable.GetNextRetryHeight())) //nolint:gosec // test height is bounded.
	require.NoError(t, scenario.state.keeper.ProcessRefundRetryQueue(scenario.state.ctx))
	retried := mustRefundRecord(t, scenario.state, scenario.refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, retried.GetStatus())
	require.Equal(t, types.DefaultMaxRefundRetries, retried.GetRetryCount())
	require.Zero(t, retried.GetNextRetryHeight())
	require.NotEqual(t, first.GetActiveTimeoutTimestamp(), retried.GetActiveTimeoutTimestamp())
	require.True(t, reserveRefundAmount(scenario.state, scenario.state.inputIBCDenom).IsZero())
	require.Equal(t, 3, scenario.state.ics4.sentCount(scenario.state.ctx))
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))
}

func TestRefundRetryQueueSelectsAtMostPerBlockBound(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	store := exchangeAtomicStore(state.ctx, state.keeper.storeService)
	for sequence := uint64(1); sequence <= uint64(types.MaxRefundRetryDispatchesPerBlock)+5; sequence++ {
		refundID := RefundID(types.PortID, "channel-7", sequence)
		store.Set(refundRetryQueueKey(1, refundID), []byte(refundID))
	}

	due, err := state.keeper.dueRefundRetries(state.ctx.WithBlockHeight(1), types.MaxRefundRetryDispatchesPerBlock)
	require.NoError(t, err)
	require.Len(t, due, int(types.MaxRefundRetryDispatchesPerBlock))
	for _, scheduled := range due {
		require.Equal(t, uint64(1), scheduled.height)
	}
}

func TestProcessRefundRetryQueueDispatchesAtMostPerBlockBound(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	const extra = 5
	count := int(types.MaxRefundRetryDispatchesPerBlock) + extra
	coin := sdk.NewInt64Coin("atgxusd", 1)
	state.bank.SetBalance(
		state.ctx,
		state.reserve,
		sdk.NewCoins(sdk.NewCoin(coin.Denom, coin.Amount.MulRaw(int64(count)))),
	)
	require.NoError(t, state.bex.AddPendingLiability(
		state.ctx,
		7,
		sdk.NewCoin(coin.Denom, coin.Amount.MulRaw(int64(count))),
	))

	for sequence := 1; sequence <= count; sequence++ {
		refundID := RefundID(types.PortID, "channel-7", uint64(sequence)) //nolint:gosec // bounded test value.
		record := &transwapv1.RefundRecord{
			Id:                             refundID,
			Status:                         transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE,
			RefundSourcePort:               types.PortID,
			RefundSourceChannel:            "channel-0",
			Token:                          &transwapv1.Token{Denom: types.NewDenom(coin.Denom), Amount: coin.Amount.String()},
			Receiver:                       state.sender.String(),
			ClaimAddress:                   state.sender.String(),
			Memo:                           "bounded retry queue",
			ExchangeId:                     "7",
			OriginalFee:                    types.SDKCoinToProto(sdk.NewInt64Coin(coin.Denom, 0)),
			OriginalTimeoutTimestamp:       uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed positive test time.
			OriginalTimeoutHeight:          &transwapv1.RefundHeight{},
			OriginalOutputPort:             types.PortID,
			OriginalOutputChannel:          "channel-7",
			OriginalOutputSequence:         uint64(sequence), //nolint:gosec // bounded test value.
			OriginalOutputPacketCommitment: make([]byte, sha256.Size),
			VolumeReservation:              testRefundVolumeReservation(7, "1"),
		}
		require.NoError(t, state.keeper.scheduleRefundRetry(state.ctx, record))
	}

	dueHeight := state.ctx.BlockHeight() + 1
	state.ctx = state.ctx.WithBlockHeight(dueHeight)
	require.NoError(t, state.keeper.ProcessRefundRetryQueue(state.ctx))

	inFlight := 0
	retryable := 0
	for _, record := range state.keeper.GetAllRefundRecords(state.ctx) {
		switch record.GetStatus() {
		case transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT:
			inFlight++
		case transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE:
			retryable++
		}
	}
	require.Equal(t, int(types.MaxRefundRetryDispatchesPerBlock), inFlight)
	require.Equal(t, extra, retryable)
	require.Equal(t, int(types.MaxRefundRetryDispatchesPerBlock), state.ics4.sentCount(state.ctx))
	remaining, err := state.keeper.dueRefundRetries(state.ctx, count)
	require.NoError(t, err)
	require.Len(t, remaining, extra)
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))
}

func TestNativeRefundEscrowMovesOnlyBetweenReserveAndIBCCommitment(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	params := types.DefaultParams()
	params.MaxRefundRetries = 1
	require.NoError(t, state.keeper.SetParams(state.ctx, params))

	refundID := RefundID(types.PortID, "channel-7", 999)
	coin := sdk.NewInt64Coin("atgxusd", 100)
	record := &transwapv1.RefundRecord{
		Id:                             refundID,
		Status:                         transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE,
		RefundSourcePort:               types.PortID,
		RefundSourceChannel:            "channel-0",
		Token:                          &transwapv1.Token{Denom: types.NewDenom(coin.Denom), Amount: coin.Amount.String()},
		Receiver:                       state.sender.String(),
		ClaimAddress:                   state.sender.String(),
		Memo:                           "native refund",
		ExchangeId:                     "7",
		OriginalFee:                    types.SDKCoinToProto(sdk.NewInt64Coin(coin.Denom, 0)),
		OriginalTimeoutTimestamp:       uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
		OriginalTimeoutHeight:          &transwapv1.RefundHeight{},
		OriginalOutputPort:             types.PortID,
		OriginalOutputChannel:          "channel-7",
		OriginalOutputSequence:         999,
		OriginalOutputPacketCommitment: make([]byte, sha256.Size),
		VolumeReservation:              testRefundVolumeReservation(7, "1"),
	}
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(sdk.NewCoin(coin.Denom, coin.Amount.SubRaw(1))))
	require.NoError(t, state.bex.AddPendingLiability(state.ctx, 7, coin))
	require.NoError(t, state.keeper.scheduleRefundRetry(state.ctx, record))
	require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(coin))
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))
	state.ctx = state.ctx.WithBlockHeight(int64(record.GetNextRetryHeight())) //nolint:gosec // test height is bounded.

	inFlight, err := state.keeper.RetryRefund(state.ctx, refundID)
	require.NoError(t, err)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, inFlight.GetStatus())
	require.True(t, reserveRefundAmount(state, coin.Denom).IsZero())
	require.Equal(
		t,
		coin.Amount,
		state.bank.GetAllBalances(state.ctx, types.GetEscrowAddress(types.PortID, "channel-0")).AmountOf(coin.Denom),
	)
	require.Equal(t, coin.Amount, state.keeper.GetTotalEscrowForDenom(state.ctx, coin.Denom).Amount)
	require.Equal(t, coin.Amount, pendingRefundAmount(t, state, coin.Denom))
	channelEscrow := types.GetEscrowAddress(types.PortID, "channel-0")
	state.keeper.SetTotalEscrowForDenom(state.ctx, sdk.NewCoin(coin.Denom, coin.Amount.SubRaw(1)))
	require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	state.keeper.SetTotalEscrowForDenom(state.ctx, coin)
	state.bank.SetBalance(state.ctx, channelEscrow, sdk.NewCoins(sdk.NewCoin(coin.Denom, coin.Amount.SubRaw(1))))
	require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	state.bank.SetBalance(state.ctx, channelEscrow, sdk.NewCoins(coin))
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))

	packetData := refundSentPacketData(t, state, inFlight.GetActivePacketSequence())
	require.NoError(t, state.keeper.OnTimeoutTransferPacket(
		state.ctx,
		types.PortID,
		"channel-0",
		inFlight.GetActivePacketSequence(),
		packetData,
	))
	manual := mustRefundRecord(t, state, refundID)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, manual.GetStatus())
	require.Equal(t, coin.Amount, reserveRefundAmount(state, coin.Denom))
	require.True(t, state.bank.GetAllBalances(state.ctx, types.GetEscrowAddress(types.PortID, "channel-0")).IsZero())
	require.True(t, state.keeper.GetTotalEscrowForDenom(state.ctx, coin.Denom).Amount.IsZero())

	claimed, err := state.keeper.ClaimRefund(state.ctx, refundID, state.sender.String())
	require.NoError(t, err)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, claimed.GetStatus())
	require.Equal(t, coin.Amount, state.bank.GetAllBalances(state.ctx, state.sender).AmountOf(coin.Denom))
	require.True(t, pendingRefundAmount(t, state, coin.Denom).IsZero())
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))
}

func TestRefundInvariantAggregatesTrackedEscrowAcrossChannels(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.keeper.channelKeeper = exchangeAtomicChannelKeeper{
		portID: types.PortID,
		channelIDs: map[string]bool{
			"channel-0": true,
			"channel-1": true,
		},
		ics4: state.ics4,
	}
	params := types.DefaultParams()
	params.MaxRefundRetries = 1
	require.NoError(t, state.keeper.SetParams(state.ctx, params))

	coin := sdk.NewInt64Coin("atgxusd", 100)
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(sdk.NewCoin(coin.Denom, coin.Amount.MulRaw(2))))
	require.NoError(t, state.bex.AddPendingLiability(
		state.ctx,
		7,
		sdk.NewCoin(coin.Denom, coin.Amount.MulRaw(2)),
	))

	for i, refundChannel := range []string{"channel-0", "channel-1"} {
		sequence := uint64(900 + i) //nolint:gosec // fixed test index is bounded.
		record := &transwapv1.RefundRecord{
			Id:                             RefundID(types.PortID, "channel-7", sequence),
			Status:                         transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE,
			RefundSourcePort:               types.PortID,
			RefundSourceChannel:            refundChannel,
			Token:                          &transwapv1.Token{Denom: types.NewDenom(coin.Denom), Amount: coin.Amount.String()},
			Receiver:                       state.sender.String(),
			ClaimAddress:                   state.sender.String(),
			Memo:                           "multi-channel native refund",
			ExchangeId:                     "7",
			OriginalFee:                    types.SDKCoinToProto(sdk.NewInt64Coin(coin.Denom, 0)),
			OriginalTimeoutTimestamp:       uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
			OriginalTimeoutHeight:          &transwapv1.RefundHeight{},
			OriginalOutputPort:             types.PortID,
			OriginalOutputChannel:          "channel-7",
			OriginalOutputSequence:         sequence,
			OriginalOutputPacketCommitment: make([]byte, sha256.Size),
			VolumeReservation:              testRefundVolumeReservation(7, "1"),
		}
		require.NoError(t, state.keeper.scheduleRefundRetry(state.ctx, record))
	}

	state.ctx = state.ctx.WithBlockHeight(state.ctx.BlockHeight() + 1)
	for _, record := range state.keeper.GetAllRefundRecords(state.ctx) {
		_, err := state.keeper.RetryRefund(state.ctx, record.GetId())
		require.NoError(t, err)
	}
	require.Equal(t, coin.Amount, state.bank.GetAllBalances(
		state.ctx,
		types.GetEscrowAddress(types.PortID, "channel-0"),
	).AmountOf(coin.Denom))
	require.Equal(t, coin.Amount, state.bank.GetAllBalances(
		state.ctx,
		types.GetEscrowAddress(types.PortID, "channel-1"),
	).AmountOf(coin.Denom))
	require.Equal(t, coin.Amount.MulRaw(2), state.keeper.GetTotalEscrowForDenom(state.ctx, coin.Denom).Amount)
	require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))

	// Each channel individually has 100, but the global tracked total must
	// cover their 200 aggregate. The former per-channel comparison missed this.
	state.keeper.SetTotalEscrowForDenom(state.ctx, coin)
	require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
}

func TestRefundCrossAccountingInvariantRejectsOrphanLedgers(t *testing.T) {
	t.Run("pending liability", func(t *testing.T) {
		state := setupExchangeReceiveAtomicity(t, false)
		require.NoError(t, state.bex.AddPendingLiability(state.ctx, 7, sdk.NewInt64Coin("atgxusd", 1)))
		require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	})

	t.Run("locked fee", func(t *testing.T) {
		state := setupExchangeReceiveAtomicity(t, false)
		require.NoError(t, state.bex.LockExchangeFee(state.ctx, 7, sdk.NewInt64Coin("atgxusd", 1)))
		require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	})
}

func TestRefundInvariantRejectsMissingIBCCommitments(t *testing.T) {
	t.Run("pending original output", func(t *testing.T) {
		state := setupExchangeReceiveAtomicity(t, false)
		timeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.
		require.NoError(t, state.keeper.OnRecvExchangePacket(
			state.ctx,
			state.packetData,
			"xswap",
			"channel-1",
			types.PortID,
			"channel-0",
			timeout,
		))
		require.NoError(t, state.keeper.AssertRefundInvariants(state.ctx))

		exchangeAtomicStore(state.ctx, state.keeper.storeService).Delete(
			exchangeAtomicKey("ics4", "data", strconv.FormatUint(exchangeAtomicSequence, 10)),
		)
		require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
	})

	t.Run("active refund", func(t *testing.T) {
		scenario := setupRefundStateMachineScenario(t)
		active := mustRefundRecord(t, scenario.state, scenario.refundID)
		require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))

		exchangeAtomicStore(scenario.state.ctx, scenario.state.keeper.storeService).Delete(
			exchangeAtomicKey("ics4", "data", strconv.FormatUint(active.GetActivePacketSequence(), 10)),
		)
		require.ErrorIs(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx), types.ErrRefundEscrowInvariant)
	})
}

func TestRefundTimeoutValidationAndImmutableBusinessDeadline(t *testing.T) {
	destinationTimestamp := uint64(1_000)
	safetyMargin := uint64(50)
	require.ErrorIs(t, validateRefundTransportTimeout(destinationTimestamp, destinationTimestamp, safetyMargin), types.ErrUnsafeRefundTimeout)
	require.ErrorIs(t, validateRefundTransportTimeout(destinationTimestamp+safetyMargin, destinationTimestamp, safetyMargin), types.ErrUnsafeRefundTimeout)
	require.NoError(t, validateRefundTransportTimeout(destinationTimestamp+safetyMargin+1, destinationTimestamp, safetyMargin))
	require.ErrorIs(t, validateRefundTransportTimeout(math.MaxUint64, math.MaxUint64-1, 2), types.ErrUnsafeRefundTimeout)

	scenario := setupRefundStateMachineScenario(t)
	record := mustRefundRecord(t, scenario.state, scenario.refundID)
	record.OriginalTimeoutTimestamp++
	require.ErrorIs(t, scenario.state.keeper.SetRefundRecord(scenario.state.ctx, record), types.ErrInvalidRefundState)
	require.Equal(t, scenario.originalTimeout, mustRefundRecord(t, scenario.state, scenario.refundID).GetOriginalTimeoutTimestamp())
	record = mustRefundRecord(t, scenario.state, scenario.refundID)
	record.OriginalTimeoutHeight = &transwapv1.RefundHeight{RevisionNumber: 2, RevisionHeight: 99}
	require.ErrorIs(t, scenario.state.keeper.SetRefundRecord(scenario.state.ctx, record), types.ErrInvalidRefundState)
}

func TestRefundCallbackTemplateMismatchRollsBack(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	before := mustRefundRecord(t, scenario.state, scenario.refundID)
	data := refundSentPacketData(t, scenario.state, before.GetActivePacketSequence())
	data.Receiver = scenario.state.receiver.String()

	err := scenario.state.keeper.OnAcknowledgementTransferPacket(
		scenario.state.ctx,
		types.PortID,
		"channel-0",
		before.GetActivePacketSequence(),
		data,
		channeltypes.NewResultAcknowledgement([]byte{1}),
	)
	require.ErrorIs(t, err, types.ErrRefundEscrowInvariant)
	require.True(t, proto.Equal(before, mustRefundRecord(t, scenario.state, scenario.refundID)))
	require.Equal(t, sdkmath.NewInt(103), pendingRefundAmount(t, scenario.state, scenario.state.inputIBCDenom))
}

func TestRefundInvariantRejectsOutputAndActivePacketCollision(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	timeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.
	require.NoError(t, state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		timeout,
	))
	pending := mustRefundRecord(t, state, RefundID(types.PortID, "channel-7", exchangeAtomicSequence))
	active := proto.Clone(pending).(*transwapv1.RefundRecord)
	active.Id = RefundID(types.PortID, "channel-8", 999)
	active.Status = transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT
	active.OriginalOutputChannel = "channel-8"
	active.OriginalOutputSequence = 999
	active.OriginalOutputPacketCommitment = make([]byte, sha256.Size)
	active.RefundSourceChannel = pending.GetOriginalOutputChannel()
	active.ActivePacketSequence = pending.GetOriginalOutputSequence()
	active.ActiveTimeoutTimestamp = timeout + 1
	active.RetryCount = 1
	require.NoError(t, state.keeper.SetRefundRecord(state.ctx, active))
	require.NoError(t, state.keeper.setActiveRefundPacketIndex(state.ctx, active))

	err := state.keeper.AssertRefundInvariants(state.ctx)
	require.ErrorIs(t, err, types.ErrRefundEscrowInvariant)
	require.ErrorContains(t, err, "share live packet")
}

func TestRefundInvariantRejectsOrphanAndDuplicateActivePacketIndexes(t *testing.T) {
	scenario := setupRefundStateMachineScenario(t)
	require.NoError(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx))

	cacheCtx, _ := scenario.state.ctx.CacheContext()
	active := mustRefundRecord(t, scenario.state, scenario.refundID)
	duplicate := proto.Clone(active).(*transwapv1.RefundRecord)
	duplicate.Id = RefundID(types.PortID, "channel-7", 1_000)
	duplicate.OriginalOutputSequence = 1_000
	require.NoError(t, scenario.state.keeper.SetRefundRecord(cacheCtx, duplicate))
	require.ErrorIs(t, scenario.state.keeper.setActiveRefundPacketIndex(cacheCtx, duplicate), types.ErrRefundEscrowInvariant)

	scenario.state.keeper.refundActiveStore(scenario.state.ctx).Set(
		[]byte(refundPacketIndexKey(types.PortID, "channel-0", 9_999)),
		[]byte(scenario.refundID),
	)
	require.ErrorIs(t, scenario.state.keeper.AssertRefundInvariants(scenario.state.ctx), types.ErrRefundEscrowInvariant)
}

func TestRefundInvariantRejectsOrphanRetryQueueIndex(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	refundID := RefundID(types.PortID, "channel-7", 9_999)
	store := exchangeAtomicStore(state.ctx, state.keeper.storeService)
	store.Set(refundRetryQueueKey(1, refundID), []byte(refundID))

	require.ErrorIs(t, state.keeper.AssertRefundInvariants(state.ctx), types.ErrRefundEscrowInvariant)
}

func TestRefundStateTransitionsAreDeterministic(t *testing.T) {
	tests := []struct {
		name    string
		current transwapv1.RefundStatus
		next    transwapv1.RefundStatus
		wantErr bool
	}{
		{"pending to retryable", transwapv1.RefundStatus_REFUND_STATUS_PENDING, transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE, false},
		{"pending to completed", transwapv1.RefundStatus_REFUND_STATUS_PENDING, transwapv1.RefundStatus_REFUND_STATUS_COMPLETED, false},
		{"pending cannot skip to in flight", transwapv1.RefundStatus_REFUND_STATUS_PENDING, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, true},
		{"retryable to in flight", transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, false},
		{"in flight to retryable", transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE, false},
		{"in flight to completed", transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, transwapv1.RefundStatus_REFUND_STATUS_COMPLETED, false},
		{"in flight to manual", transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT, transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, false},
		{"manual to claimed", transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, false},
		{"completed is terminal", transwapv1.RefundStatus_REFUND_STATUS_COMPLETED, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, true},
		{"claimed is terminal", transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, transwapv1.RefundStatus_REFUND_STATUS_COMPLETED, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &transwapv1.RefundRecord{Status: test.current}
			err := transitionRefundStatus(record, test.next)
			if test.wantErr {
				require.ErrorIs(t, err, types.ErrInvalidRefundState)
				require.Equal(t, test.current, record.GetStatus())
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.next, record.GetStatus())
		})
	}
}

func mustRefundRecord(t *testing.T, state exchangeReceiveAtomicityState, refundID string) *transwapv1.RefundRecord {
	t.Helper()
	record, found, err := state.keeper.GetRefundRecord(state.ctx, refundID)
	require.NoError(t, err)
	require.True(t, found)
	return record
}

func testRefundVolumeReservation(exchangeID uint64, amount string) *bexv1.VolumeReservation {
	return &bexv1.VolumeReservation{
		ExchangeId:             exchangeID,
		Direction:              bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		EpochSeconds:           bextypes.MinVolumeEpochSeconds,
		Amount:                 amount,
		VolumeWindowGeneration: 1,
	}
}

func refundSentPacketData(t *testing.T, state exchangeReceiveAtomicityState, sequence uint64) types.InternalTransferRepresentation {
	t.Helper()
	data, err := types.UnmarshalPacketData(
		state.ics4.sentPacketData(state.ctx, sequence),
		types.V1,
		types.EncodingJSON,
	)
	require.NoError(t, err)
	return data
}

func pendingRefundAmount(t *testing.T, state exchangeReceiveAtomicityState, denom string) sdkmath.Int {
	t.Helper()
	coins, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	return coins.AmountOf(denom)
}

func lockedRefundFeeAmount(t *testing.T, state exchangeReceiveAtomicityState, denom string) sdkmath.Int {
	t.Helper()
	coins, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	return coins.AmountOf(denom)
}

func reserveRefundAmount(state exchangeReceiveAtomicityState, denom string) sdkmath.Int {
	return state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(denom)
}
