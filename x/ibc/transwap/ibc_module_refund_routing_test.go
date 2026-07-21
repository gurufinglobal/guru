package transwap

import (
	"bytes"
	"errors"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	keeperpkg "github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const moduleRefundBoundarySequence = uint64(77)

func TestIBCModuleOriginalFailureRoutesToActiveRefund(t *testing.T) {
	tests := []struct {
		name      string
		errorAck  bool
		wantEvent string
	}{
		{name: "error acknowledgement", errorAck: true, wantEvent: types.EventTypePacket},
		{name: "timeout", wantEvent: types.EventTypeTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario := setupIBCModuleRefundBoundaryScenario(t)
			require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))

			scenario.failOriginal(t, tt.errorAck)
			scenario.requireActiveRefund(t, 0, 1)
			require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))
			require.True(t, hasEventType(scenario.ctx, tt.wantEvent))

			// The original output index is deleted before the retry is sent, so a
			// repeated callback cannot restore funds or create another packet.
			scenario.failOriginal(t, tt.errorAck)
			scenario.requireActiveRefund(t, 0, 1)
			require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))
		})
	}
}

func TestIBCModuleOriginalSuccessAckSettlesExactlyOnce(t *testing.T) {
	scenario := setupIBCModuleRefundBoundaryScenario(t)
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	successAckBz := types.ModuleCdc.MustMarshalJSON(&successAck)

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.originalPacket,
		successAckBz,
		sdk.AccAddress{},
	))

	_, found, err := scenario.k.GetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	require.False(t, found, "completed original-output records must be pruned")
	require.Empty(t, scenario.ics4.sent)
	require.Empty(t, scenario.bex.livePending())
	require.Empty(t, mustLockedRefundFees(t, scenario))
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(3))),
		scenario.bex.ledger("released"),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(103))),
		scenario.bex.ledger("liability_released"),
	)
	require.Equal(
		t,
		sdk.NewCoins(
			sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(100)),
			sdk.NewCoin(scenario.outputIBCDenom, sdkmath.NewInt(900)),
		),
		scenario.bank.GetAllBalances(scenario.ctx, scenario.reserve),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(3))),
		scenario.bank.GetAllBalances(scenario.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)),
	)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))

	// The output packet index is gone after settlement. Re-entering the same
	// application callback must not release the fee or liability a second time.
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.originalPacket,
		successAckBz,
		sdk.AccAddress{},
	))
	_, found, err = scenario.k.GetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, scenario.ics4.sent)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(3))),
		scenario.bex.ledger("released"),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(scenario.inputIBCDenom, sdkmath.NewInt(103))),
		scenario.bex.ledger("liability_released"),
	)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))
}

func TestIBCModuleActiveRefundErrorAckRestoresCustodyAndRetries(t *testing.T) {
	scenario := setupIBCModuleRefundBoundaryScenario(t)
	scenario.failOriginal(t, true)
	firstRetry := scenario.retryPacket(t, 0)
	firstTimeout := firstRetry.GetTimeoutTimestamp()

	// Keep the first packet inside its timeout while moving the current block
	// time forward. A successful second dispatch proves the first packet was
	// restored to reserve custody, since the reserve held no input beforehand.
	scenario.ctx = scenario.ctx.WithBlockTime(scenario.ctx.BlockTime().Add(2 * time.Minute))
	errorAck := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected refund"))
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		firstRetry,
		types.ModuleCdc.MustMarshalJSON(&errorAck),
		sdk.AccAddress{},
	))

	scenario.requireActiveRefund(t, 1, 2)
	secondRetry := scenario.retryPacket(t, 1)
	require.NotEqual(t, firstRetry.GetSequence(), secondRetry.GetSequence())
	require.Greater(t, secondRetry.GetTimeoutTimestamp(), firstTimeout)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))

	// Once the active index points at the second packet, re-entering the first
	// error acknowledgement is stale and cannot send a third packet.
	beforeDuplicate, err := scenario.k.MustGetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		firstRetry,
		types.ModuleCdc.MustMarshalJSON(&errorAck),
		sdk.AccAddress{},
	))
	afterDuplicate, err := scenario.k.MustGetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	require.Equal(t, beforeDuplicate, afterDuplicate)
	scenario.requireActiveRefund(t, 1, 2)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))
}

func TestIBCModuleRefundCallbacksIgnoreStaleAndDuplicatePackets(t *testing.T) {
	scenario := setupIBCModuleRefundBoundaryScenario(t)
	scenario.failOriginal(t, true)
	firstRetry := scenario.retryPacket(t, 0)

	// A timeout restores custody and atomically sends the next bounded retry.
	require.NoError(t, scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		firstRetry,
		sdk.AccAddress{},
	))
	scenario.requireActiveRefund(t, 1, 2)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))

	beforeStale, err := scenario.k.MustGetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	staleErrorAck := channeltypes.NewErrorAcknowledgement(errors.New("late destination rejection"))
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		firstRetry,
		types.ModuleCdc.MustMarshalJSON(&staleErrorAck),
		sdk.AccAddress{},
	))
	require.NoError(t, scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		firstRetry,
		sdk.AccAddress{},
	))
	afterStale, err := scenario.k.MustGetRefundRecord(scenario.ctx, scenario.refundID)
	require.NoError(t, err)
	require.Equal(t, beforeStale, afterStale)
	scenario.requireActiveRefund(t, 1, 2)

	secondRetry := scenario.retryPacket(t, 1)
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	successAckBz := types.ModuleCdc.MustMarshalJSON(&successAck)
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		secondRetry,
		successAckBz,
		sdk.AccAddress{},
	))
	scenario.requireCompletedRefund(t)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))

	// Both duplicate ACK and late timeout are no-ops after the active index is
	// deleted. In particular, neither can release the liability twice.
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		secondRetry,
		successAckBz,
		sdk.AccAddress{},
	))
	require.NoError(t, scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		secondRetry,
		sdk.AccAddress{},
	))
	scenario.requireCompletedRefund(t)
	require.NoError(t, scenario.k.AssertRefundInvariants(scenario.ctx))
}

func mustLockedRefundFees(t *testing.T, scenario ibcModuleRefundBoundaryScenario) sdk.Coins {
	t.Helper()
	locked, err := scenario.bex.GetLockedFees(scenario.ctx, 7)
	require.NoError(t, err)
	return locked
}

type ibcModuleRefundBoundaryScenario struct {
	k              keeperpkg.Keeper
	ctx            sdk.Context
	bank           *moduleAckRefundBankKeeper
	bex            *moduleAckRefundBexKeeper
	ics4           *moduleAckRefundICS4Wrapper
	im             *IBCModule
	reserve        sdk.AccAddress
	originalSender sdk.AccAddress
	inputDenom     types.Denom
	outputDenom    types.Denom
	inputIBCDenom  string
	outputIBCDenom string
	refundID       string
	originalPacket channeltypes.Packet
}

func setupIBCModuleRefundBoundaryScenario(t *testing.T) ibcModuleRefundBoundaryScenario {
	t.Helper()

	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	reserve := bex.reserve
	originalSender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	targetReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	inputDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	outputDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	inputIBCDenom := types.DenomIBCDenom(inputDenom)
	outputIBCDenom := types.DenomIBCDenom(outputDenom)
	grossRefund := sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(103))
	originalFee := sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))

	// The principal remains in reserve while the fee remains in the BEX
	// module. The original output has already left the reserve.
	bank.SetBalance(reserve, sdk.NewCoins(
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(100)),
		sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(900)),
	))
	bank.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(originalFee))
	require.NoError(t, bex.AddPendingLiability(ctx, 7, grossRefund))
	require.NoError(t, bex.LockExchangeFee(ctx, 7, originalFee))

	originalTimeout := uint64(ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.
	originalData := types.NewFungibleTokenPacketData(
		types.DenomPath(outputDenom),
		"100",
		reserve.String(),
		targetReceiver.String(),
		"Station exchange",
	)
	originalPacket := channeltypes.NewPacket(
		types.FungibleTokenPacketDataBytes(originalData),
		moduleRefundBoundarySequence,
		types.PortID,
		"channel-7",
		"xswap",
		"channel-1",
		clienttypes.ZeroHeight(),
		originalTimeout,
	)
	ics4.recordPacketCommitment(originalPacket)

	refundID := types.RefundID(types.PortID, "channel-7", moduleRefundBoundarySequence)
	record := &types.RefundRecord{
		Id:                             refundID,
		Status:                         types.RefundStatus_REFUND_STATUS_PENDING,
		RefundSourcePort:               types.PortID,
		RefundSourceChannel:            "channel-0",
		Token:                          types.Token{Denom: inputDenom, Amount: grossRefund.Amount.String()},
		Receiver:                       originalSender.String(),
		ClaimAddress:                   originalSender.String(),
		Memo:                           "refund original input",
		ExchangeId:                     "7",
		OriginalFee:                    types.SDKCoinToProto(originalFee),
		OriginalTimeoutTimestamp:       originalTimeout,
		OriginalOutputPort:             types.PortID,
		OriginalOutputChannel:          "channel-7",
		OriginalOutputSequence:         moduleRefundBoundarySequence,
		OriginalOutputPacketCommitment: channeltypes.CommitPacket(originalPacket),
		VolumeReservation: &bextypes.VolumeReservation{
			ExchangeId:             7,
			Direction:              bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
			EpochSeconds:           bextypes.MinVolumeEpochSeconds,
			Amount:                 "100",
			VolumeWindowGeneration: 1,
		},
	}
	require.NoError(t, k.CreateRefundRecord(ctx, record))

	return ibcModuleRefundBoundaryScenario{
		k:              k,
		ctx:            ctx,
		bank:           bank,
		bex:            bex,
		ics4:           ics4,
		im:             NewIBCModule(k),
		reserve:        reserve,
		originalSender: originalSender,
		inputDenom:     inputDenom,
		outputDenom:    outputDenom,
		inputIBCDenom:  inputIBCDenom,
		outputIBCDenom: outputIBCDenom,
		refundID:       refundID,
		originalPacket: originalPacket,
	}
}

func (s ibcModuleRefundBoundaryScenario) failOriginal(t *testing.T, errorAck bool) {
	t.Helper()
	if !errorAck {
		require.NoError(t, s.im.OnTimeoutPacket(s.ctx, types.V1, s.originalPacket, sdk.AccAddress{}))
		return
	}
	ack := channeltypes.NewErrorAcknowledgement(errors.New("target rejected swap output"))
	require.NoError(t, s.im.OnAcknowledgementPacket(
		s.ctx,
		types.V1,
		s.originalPacket,
		types.ModuleCdc.MustMarshalJSON(&ack),
		sdk.AccAddress{},
	))
}

func (s ibcModuleRefundBoundaryScenario) retryPacket(t *testing.T, index int) channeltypes.Packet {
	t.Helper()
	require.Greater(t, len(s.ics4.sent), index)
	sent := s.ics4.sent[index]
	return channeltypes.NewPacket(
		sent.data,
		sent.sequence,
		sent.sourcePort,
		sent.sourceChannel,
		"xswap",
		"channel-1",
		clienttypes.ZeroHeight(),
		sent.timeoutTimestamp,
	)
}

func (s ibcModuleRefundBoundaryScenario) requireActiveRefund(t *testing.T, sentIndex int, retryCount uint32) {
	t.Helper()
	require.Len(t, s.ics4.sent, sentIndex+1)
	sent := s.ics4.sent[sentIndex]
	record, err := s.k.MustGetRefundRecord(s.ctx, s.refundID)
	require.NoError(t, err)
	require.Equal(t, types.RefundStatus_REFUND_STATUS_IN_FLIGHT, record.GetStatus())
	require.Equal(t, retryCount, record.GetRetryCount())
	require.Equal(t, sent.sequence, record.GetActivePacketSequence())
	require.Equal(t, sent.timeoutTimestamp, record.GetActiveTimeoutTimestamp())
	require.Greater(t, sent.timeoutTimestamp, uint64(s.ctx.BlockTime().UnixNano())) //nolint:gosec // fixed test time is positive.

	data, err := types.UnmarshalPacketData(sent.data, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, types.PacketKindTransfer, mustClassifyRefundPacket(t, data))
	require.Equal(t, s.reserve.String(), data.Sender)
	require.Equal(t, s.originalSender.String(), data.Receiver)
	require.Equal(t, "103", data.Token.Amount)
	require.Equal(t, types.DenomPath(s.inputDenom), types.DenomPath(data.Token.Denom))

	reserveBalances := s.bank.GetAllBalances(s.ctx, s.reserve)
	require.True(t, reserveBalances.AmountOf(s.inputIBCDenom).IsZero())
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(s.outputIBCDenom))
	require.True(t, s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	pending, err := s.bex.GetPendingLiabilities(s.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(s.inputIBCDenom, sdkmath.NewInt(103))), pending)
	locked, err := s.bex.GetLockedFees(s.ctx, 7)
	require.NoError(t, err)
	require.Empty(t, locked)
}

func (s ibcModuleRefundBoundaryScenario) requireCompletedRefund(t *testing.T) {
	t.Helper()
	_, found, err := s.k.GetRefundRecord(s.ctx, s.refundID)
	require.NoError(t, err)
	require.False(t, found, "completed refund transport records must be pruned")
	pending, err := s.bex.GetPendingLiabilities(s.ctx, 7)
	require.NoError(t, err)
	require.Empty(t, pending)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(s.inputIBCDenom, sdkmath.NewInt(103))),
		s.bex.ledger("liability_released"),
	)
}

func mustClassifyRefundPacket(t *testing.T, data types.InternalTransferRepresentation) types.PacketKind {
	t.Helper()
	kind, err := data.ClassifyPacket()
	require.NoError(t, err)
	return kind
}

func hasEventType(ctx sdk.Context, eventType string) bool {
	for _, event := range ctx.EventManager().Events() {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
