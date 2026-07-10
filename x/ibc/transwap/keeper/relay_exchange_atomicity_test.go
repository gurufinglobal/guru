package keeper

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	"github.com/stretchr/testify/require"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const exchangeAtomicSequence = uint64(77)

var (
	errExchangeAtomicRecordVolume = errors.New("test volume record failure")
	errExchangeAtomicSendPacket   = errors.New("test send packet failure")
	errExchangeAtomicQuote        = errors.New("test quote failure")
	errExchangeAtomicBankSend     = errors.New("test bank send failure")
	errExchangeAtomicAddFee       = errors.New("test add collected fee failure")
	errExchangeAtomicLockFee      = errors.New("test lock exchange fee failure")
)

func TestOnRecvExchangePacketCommitsStateAfterSuccessfulSwapReceive(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.NoError(t, err)

	require.Equal(t, sdkmath.NewInt(900), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(100), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(3), state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(state.inputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Equal(t, 1, state.ics4.sentCount(state.ctx))
	sent := state.ics4.sentPacketData(state.ctx, exchangeAtomicSequence)
	internal, err := types.UnmarshalPacketData(sent, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, "100", internal.Token.Amount)
	require.Equal(t, state.reserve.String(), internal.Sender)
	require.Equal(t, state.receiver.String(), internal.Receiver)
	require.Equal(t, types.DenomPath(state.outputTokenDenom), types.DenomPath(internal.Token.Denom))

	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	refund, err := state.keeper.GetRefundPacketData(state.ctx, refundKey)
	require.NoError(t, err)
	require.Equal(t, state.reserve.String(), refund.Sender)
	require.Equal(t, state.sender.String(), refund.Receiver)
	require.Equal(t, "7", refund.ExchangeId)
	require.Equal(t, state.inputIBCDenom, refund.GetFee().GetDenom())
	require.Equal(t, "3", refund.GetFee().GetAmount())

	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(100), state.bex.recordedVolume(state.ctx))
	require.True(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketCommitsStateWithoutFee(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.feeAmount = "0"

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.NoError(t, err)

	require.Equal(t, sdkmath.NewInt(900), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(103), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Equal(t, 1, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	refund, err := state.keeper.GetRefundPacketData(state.ctx, refundKey)
	require.NoError(t, err)
	require.Equal(t, state.inputIBCDenom, refund.GetFee().GetDenom())
	require.Equal(t, "0", refund.GetFee().GetAmount())
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(100), state.bex.recordedVolume(state.ctx))
}

func TestOnRecvExchangePacketPreservesReceiverVariationAndUsesDeterministicMemos(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	receivers := []sdk.AccAddress{
		sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20)),
		sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20)),
	}
	sourceMemos := []string{
		"source memo one",
		`{"source":"memo-two"}`,
	}
	timeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.

	for i, receiver := range receivers {
		packetData := state.packetData
		packetData.Receiver = receiver.String()
		packetData.Memo = sourceMemos[i]

		require.NoError(t, state.keeper.OnRecvExchangePacket(
			state.ctx,
			packetData,
			"xswap",
			"channel-1",
			types.PortID,
			"channel-0",
			timeout,
		))

		sequence := exchangeAtomicSequence + uint64(i) //nolint:gosec // test count is bounded.
		sent := state.ics4.sentPacketData(state.ctx, sequence)
		internal, err := types.UnmarshalPacketData(sent, types.V1, types.EncodingJSON)
		require.NoError(t, err)
		require.Equal(t, state.reserve.String(), internal.Sender)
		require.Equal(t, receiver.String(), internal.Receiver)
		require.Equal(t, "Station exchange", internal.Memo)
		require.NotEqual(t, sourceMemos[i], internal.Memo)

		refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", sequence)
		refund, err := state.keeper.GetRefundPacketData(state.ctx, refundKey)
		require.NoError(t, err)
		require.Equal(t, state.reserve.String(), refund.Sender)
		require.Equal(t, state.sender.String(), refund.Receiver)
		require.Equal(t, "refund coins through Guru station due to failure on the target chain", refund.Memo)
		require.NotEqual(t, sourceMemos[i], refund.Memo)
	}

	require.Equal(t, 2, state.ics4.sentCount(state.ctx))
	require.Equal(t, sdkmath.NewInt(800), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(200), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(6), state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(state.inputIBCDenom))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(6))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(6))), state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolume(state.ctx))
}

func TestOnRecvExchangePacketCommitsSmallAndLargeAmounts(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.quoteByAmount = map[string]exchangeAtomicSwapRoute{
		"1": {
			direction:   bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			outputDenom: state.outputIBCDenom,
			amountOut:   "1",
			feeAmount:   "0",
		},
		"1000003": {
			direction:   bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			outputDenom: state.outputIBCDenom,
			amountOut:   "700",
			feeAmount:   "3",
		},
	}
	timeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.

	amounts := []string{"1", "1000003"}
	for _, amount := range amounts {
		packetData := state.packetData
		packetData.Token.Amount = amount

		require.NoError(t, state.keeper.OnRecvExchangePacket(
			state.ctx,
			packetData,
			"xswap",
			"channel-1",
			types.PortID,
			"channel-0",
			timeout,
		))
	}

	require.Equal(t, 2, state.ics4.sentCount(state.ctx))
	expectedPackets := []struct {
		sequence  uint64
		amountOut string
		feeAmount string
	}{
		{exchangeAtomicSequence, "1", "0"},
		{exchangeAtomicSequence + 1, "700", "3"},
	}
	for _, expected := range expectedPackets {
		sent := state.ics4.sentPacketData(state.ctx, expected.sequence)
		internal, err := types.UnmarshalPacketData(sent, types.V1, types.EncodingJSON)
		require.NoError(t, err)
		require.Equal(t, expected.amountOut, internal.Token.Amount)
		require.Equal(t, types.DenomPath(state.outputTokenDenom), types.DenomPath(internal.Token.Denom))

		refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", expected.sequence)
		refund, err := state.keeper.GetRefundPacketData(state.ctx, refundKey)
		require.NoError(t, err)
		require.Equal(t, state.inputIBCDenom, refund.GetFee().GetDenom())
		require.Equal(t, expected.feeAmount, refund.GetFee().GetAmount())
	}

	reserveBalances := state.bank.GetAllBalances(state.ctx, state.reserve)
	require.Equal(t, sdkmath.NewInt(299), reserveBalances.AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(1000001), reserveBalances.AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(3), state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(state.inputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(701), state.bex.recordedVolume(state.ctx))
}

func TestOnRecvExchangePacketCommitsRepeatedSwapsInBothDirections(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(
		sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(1000)),
		sdk.NewCoin(state.outputIBCDenom, sdkmath.NewInt(1000)),
	))
	state.bex.routes = map[string]exchangeAtomicSwapRoute{
		"atgxusd": {
			direction:   bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			outputDenom: state.outputIBCDenom,
			amountOut:   "100",
			feeAmount:   "3",
		},
		"atgxkrw": {
			direction:   bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A,
			outputDenom: state.inputIBCDenom,
			amountOut:   "100",
			feeAmount:   "3",
		},
	}
	state.keeper.channelKeeper = exchangeAtomicChannelKeeper{
		portID:     types.PortID,
		channelIDs: map[string]bool{"channel-0": true, "channel-7": true},
	}

	reversePacketData := types.NewInternalTransferRepresentation(
		"7",
		transwapv1.Token{Denom: types.NewDenom("atgxkrw"), Amount: "103"},
		state.sender.String(),
		state.receiver.String(),
		"source memo",
	)
	timeout := uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()) //nolint:gosec // fixed test time is positive.

	for i := 0; i < 2; i++ {
		require.NoError(t, state.keeper.OnRecvExchangePacket(
			state.ctx,
			state.packetData,
			"xswap",
			"channel-1",
			types.PortID,
			"channel-0",
			timeout,
		))
		require.NoError(t, state.keeper.OnRecvExchangePacket(
			state.ctx,
			reversePacketData,
			"xswap",
			"channel-9",
			types.PortID,
			"channel-7",
			timeout,
		))
	}

	reserveBalances := state.bank.GetAllBalances(state.ctx, state.reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(state.outputIBCDenom))

	bexModuleBalances := state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName))
	require.Equal(t, sdkmath.NewInt(6), bexModuleBalances.AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(6), bexModuleBalances.AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	expectedFees := sdk.NewCoins(
		sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(6)),
		sdk.NewCoin(state.outputIBCDenom, sdkmath.NewInt(6)),
	)
	require.Equal(t, expectedFees, state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, expectedFees, state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(400), state.bex.recordedVolume(state.ctx))
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolumeForDirection(state.ctx, bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B))
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolumeForDirection(state.ctx, bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A))
	require.Equal(t, 4, state.ics4.sentCount(state.ctx))

	expectedPackets := []struct {
		sequence  uint64
		channel   string
		denomPath string
		feeDenom  string
	}{
		{exchangeAtomicSequence, "channel-7", types.DenomPath(state.outputTokenDenom), state.inputIBCDenom},
		{exchangeAtomicSequence + 1, "channel-0", types.DenomPath(state.inputTokenDenom), state.outputIBCDenom},
		{exchangeAtomicSequence + 2, "channel-7", types.DenomPath(state.outputTokenDenom), state.inputIBCDenom},
		{exchangeAtomicSequence + 3, "channel-0", types.DenomPath(state.inputTokenDenom), state.outputIBCDenom},
	}
	for _, expected := range expectedPackets {
		sent := state.ics4.sentPacketData(state.ctx, expected.sequence)
		internal, err := types.UnmarshalPacketData(sent, types.V1, types.EncodingJSON)
		require.NoError(t, err)
		require.Equal(t, expected.denomPath, types.DenomPath(internal.Token.Denom))
		require.Equal(t, "100", internal.Token.Amount)
		require.Equal(t, state.reserve.String(), internal.Sender)
		require.Equal(t, state.receiver.String(), internal.Receiver)

		refundKey := GetRefundPacketDataKey(types.PortID, expected.channel, expected.sequence)
		refund, err := state.keeper.GetRefundPacketData(state.ctx, refundKey)
		require.NoError(t, err)
		require.Equal(t, state.reserve.String(), refund.Sender)
		require.Equal(t, state.sender.String(), refund.Receiver)
		require.Equal(t, "7", refund.ExchangeId)
		require.Equal(t, expected.feeDenom, refund.GetFee().GetDenom())
		require.Equal(t, "3", refund.GetFee().GetAmount())
	}
}

func TestOnRecvExchangePacketRollsBackStateWhenVolumeRecordFails(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, true)

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicRecordVolume)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenTelemetryChannelLookupFails(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.keeper.channelKeeper = refundAccountingChannelKeeper{portID: types.PortID, channelID: "channel-missing"}

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, channeltypes.ErrChannelNotFound)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenQuoteFailsAfterReceive(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.quoteErr = errExchangeAtomicQuote

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicQuote)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenQuoteAmountOutIsInvalid(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.amountOut = "0"

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenQuoteOutputDenomIsMissing(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	missingDenom := types.NewDenom("atmissing", types.NewHop(types.PortID, "channel-99"))
	state.bex.outputDenom = types.DenomIBCDenom(missingDenom)

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, types.ErrDenomNotFound)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(missingDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenQuoteOutputPortIsWrong(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	wrongPortDenom := types.NewDenom("atgxkrw", types.NewHop("transfer", "channel-7"))
	state.keeper.SetDenom(state.ctx, wrongPortDenom)
	state.bex.outputDenom = types.DenomIBCDenom(wrongPortDenom)

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, bextypes.ErrInvalidRoute)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
	require.True(t, state.keeper.HasDenom(state.ctx, types.DenomHash(wrongPortDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenQuoteOutputChannelIsWrong(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	wrongChannelDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-8"))
	wrongChannelIBCDenom := types.DenomIBCDenom(wrongChannelDenom)
	state.keeper.SetDenom(state.ctx, wrongChannelDenom)
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(
		sdk.NewCoin(state.outputIBCDenom, sdkmath.NewInt(1000)),
		sdk.NewCoin(wrongChannelIBCDenom, sdkmath.NewInt(1000)),
	))
	state.bex.outputDenom = wrongChannelIBCDenom

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, channeltypes.ErrChannelNotFound)

	reserveBalances := state.bank.GetAllBalances(state.ctx, state.reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(wrongChannelIBCDenom))
	require.True(t, reserveBalances.AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-8", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
	require.True(t, state.keeper.HasDenom(state.ctx, types.DenomHash(wrongChannelDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenReserveReceiverIsBlocked(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bank.blockedAddr = state.reserve.String()

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorContains(t, err, "is not allowed to receive funds")

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestExchangeTargetBlockedReceiverErrorAckRefundsAndCreatesRetry(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.keeper.channelKeeper = exchangeAtomicChannelKeeper{
		portID:     types.PortID,
		channelIDs: map[string]bool{"channel-0": true, "channel-7": true},
	}
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

	outboundData, err := types.UnmarshalPacketData(state.ics4.sentPacketData(state.ctx, exchangeAtomicSequence), types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, state.receiver.String(), outboundData.Receiver)

	target := setupExchangeReceiveAtomicity(t, false)
	target.bank.blockedAddr = outboundData.Receiver
	targetEscrow := types.GetEscrowAddress("xswap", "channel-1")
	target.bank.SetBalance(target.ctx, targetEscrow, sdk.NewCoins(sdk.NewCoin("atgxkrw", sdkmath.NewInt(100))))
	target.keeper.SetTotalEscrowForDenom(target.ctx, sdk.NewCoin("atgxkrw", sdkmath.NewInt(100)))

	targetErr := target.keeper.OnRecvTransferPacket(
		target.ctx,
		outboundData,
		types.PortID,
		"channel-7",
		"xswap",
		"channel-1",
	)
	require.ErrorContains(t, targetErr, "is not allowed to receive funds")
	require.True(t, target.bank.GetAllBalances(target.ctx, sdk.MustAccAddressFromBech32(outboundData.Receiver)).IsZero())
	require.Equal(t, sdkmath.NewInt(100), target.bank.GetAllBalances(target.ctx, targetEscrow).AmountOf("atgxkrw"))
	require.Equal(t, sdkmath.NewInt(100), target.keeper.GetTotalEscrowForDenom(target.ctx, "atgxkrw").Amount)

	require.NoError(t, state.keeper.OnAcknowledgementTransferPacket(
		state.ctx,
		types.PortID,
		"channel-7",
		exchangeAtomicSequence,
		outboundData,
		channeltypes.NewErrorAcknowledgement(targetErr),
	))

	reserveBalances := state.bank.GetAllBalances(state.ctx, state.reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(state.outputIBCDenom))
	require.True(t, reserveBalances.AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "released"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "deducted"))

	require.False(t, state.keeper.HasRefundPacketData(state.ctx, GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)))
	retryKey := GetRefundPacketDataKey(types.PortID, "channel-0", exchangeAtomicSequence+1)
	retry, err := state.keeper.GetRefundPacketData(state.ctx, retryKey)
	require.NoError(t, err)
	require.Equal(t, state.reserve.String(), retry.Sender)
	require.Equal(t, state.sender.String(), retry.Receiver)
	require.Nil(t, retry.Fee)

	require.Equal(t, 2, state.ics4.sentCount(state.ctx))
	retryData, err := types.UnmarshalPacketData(state.ics4.sentPacketData(state.ctx, exchangeAtomicSequence+1), types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, state.reserve.String(), retryData.Sender)
	require.Equal(t, state.sender.String(), retryData.Receiver)
	require.Equal(t, "103", retryData.Token.Amount)
	require.Equal(t, types.DenomPath(state.inputTokenDenom), types.DenomPath(retryData.Token.Denom))
}

func TestOnRecvExchangePacketRollsBackStateWhenReceiveSendFailsAfterMint(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bank.failSendTo = state.reserve.String()

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicBankSend)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenOutboundSendFails(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.ics4.failSendPacket = true

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicSendPacket)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenReserveOutputIsInsufficient(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bank.SetBalance(state.ctx, state.reserve, sdk.NewCoins(sdk.NewCoin(state.outputIBCDenom, sdkmath.NewInt(50))))

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorContains(t, err, "insufficient funds")

	require.Equal(t, sdkmath.NewInt(50), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenFeeTransferFailsAfterOutboundPacket(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bank.failSendTo = authtypes.NewModuleAddress(bextypes.ModuleName).String()

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicBankSend)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenAddCollectedFeeFailsAfterWrite(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.failAddCollectedFee = true

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicAddFee)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenLockExchangeFeeFailsAfterWrite(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.failLockExchangeFee = true

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicLockFee)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	refundKey := GetRefundPacketDataKey(types.PortID, "channel-7", exchangeAtomicSequence)
	require.False(t, state.keeper.HasRefundPacketData(state.ctx, refundKey))
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

type exchangeReceiveAtomicityState struct {
	keeper           Keeper
	ctx              sdk.Context
	bank             *exchangeAtomicBankKeeper
	bex              *exchangeAtomicBexKeeper
	ics4             *exchangeAtomicICS4Wrapper
	reserve          sdk.AccAddress
	sender           sdk.AccAddress
	receiver         sdk.AccAddress
	inputTokenDenom  *transwapv1.Denom
	inputIBCDenom    string
	outputTokenDenom *transwapv1.Denom
	outputIBCDenom   string
	packetData       types.InternalTransferRepresentation
}

func setupExchangeReceiveAtomicity(t *testing.T, failRecordVolume bool) exchangeReceiveAtomicityState {
	t.Helper()

	k, ctx, _, _ := setupKeeperStateTester(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0)).WithEventManager(sdk.NewEventManager())

	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	sender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))

	inputTokenDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	inputIBCDenom := types.DenomIBCDenom(inputTokenDenom)
	outputTokenDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	outputIBCDenom := types.DenomIBCDenom(outputTokenDenom)
	k.SetDenom(ctx, outputTokenDenom)

	bank := newExchangeAtomicBankKeeper(k.storeService)
	bank.SetBalance(ctx, reserve, sdk.NewCoins(sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(1000))))

	bex := &exchangeAtomicBexKeeper{
		storeService:       k.storeService,
		reserve:            reserve,
		outputDenom:        outputIBCDenom,
		feeAmount:          "3",
		amountOut:          "100",
		failRecordVolume:   failRecordVolume,
		expectedExchangeID: 7,
	}
	ics4 := &exchangeAtomicICS4Wrapper{storeService: k.storeService, sequence: exchangeAtomicSequence}

	k.BankKeeper = bank
	k.BexKeeper = bex
	k.ics4Wrapper = ics4
	k.channelKeeper = refundAccountingChannelKeeper{portID: types.PortID, channelID: "channel-7"}

	packetData := types.NewInternalTransferRepresentation(
		"7",
		transwapv1.Token{Denom: types.NewDenom("atgxusd"), Amount: "103"},
		sender.String(),
		receiver.String(),
		"",
	)

	return exchangeReceiveAtomicityState{
		keeper:           k,
		ctx:              ctx,
		bank:             bank,
		bex:              bex,
		ics4:             ics4,
		reserve:          reserve,
		sender:           sender,
		receiver:         receiver,
		inputTokenDenom:  inputTokenDenom,
		inputIBCDenom:    inputIBCDenom,
		outputTokenDenom: outputTokenDenom,
		outputIBCDenom:   outputIBCDenom,
		packetData:       packetData,
	}
}

type exchangeAtomicBexKeeper struct {
	storeService        corestore.KVStoreService
	reserve             sdk.AccAddress
	outputDenom         string
	feeAmount           string
	amountOut           string
	routes              map[string]exchangeAtomicSwapRoute
	quoteByAmount       map[string]exchangeAtomicSwapRoute
	failRecordVolume    bool
	failAddCollectedFee bool
	failLockExchangeFee bool
	quoteErr            error
	expectedExchangeID  uint64
}

type exchangeAtomicSwapRoute struct {
	direction   bexv1.SwapDirection
	outputDenom string
	amountOut   string
	feeAmount   string
}

func (m *exchangeAtomicBexKeeper) ResolveSwapDirection(_ context.Context, exchangeID uint64, inputDenom string) (bexv1.SwapDirection, error) {
	route, ok := m.routeForInputDenom(inputDenom)
	if exchangeID != m.expectedExchangeID || !ok {
		return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, bextypes.ErrInvalidRoute.Wrap("unexpected exchange route")
	}
	return route.direction, nil
}

func (m *exchangeAtomicBexKeeper) QuoteSwap(_ context.Context, req *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	if m.quoteErr != nil {
		return nil, m.quoteErr
	}
	route, ok := m.routeForInputDenom(req.GetInputDenom())
	if req.GetExchangeId() != m.expectedExchangeID || !ok {
		return nil, bextypes.ErrInvalidRoute.Wrap("unexpected exchange route")
	}
	if quoteRoute, ok := m.quoteByAmount[req.GetAmountIn()]; ok {
		quoteRoute.direction = route.direction
		if quoteRoute.outputDenom == "" {
			quoteRoute.outputDenom = route.outputDenom
		}
		route = quoteRoute
	}
	return &bexv1.QuoteSwapResponse{
		ExchangeId:  req.GetExchangeId(),
		Direction:   route.direction,
		InputDenom:  req.GetInputDenom(),
		OutputDenom: route.outputDenom,
		AmountIn:    req.GetAmountIn(),
		FeeAmount:   route.feeAmount,
		NetAmountIn: "100",
		AmountOut:   route.amountOut,
	}, nil
}

func (m *exchangeAtomicBexKeeper) routeForInputDenom(inputDenom string) (exchangeAtomicSwapRoute, bool) {
	if len(m.routes) > 0 {
		route, ok := m.routes[inputDenom]
		return route, ok
	}
	if inputDenom != "atgxusd" {
		return exchangeAtomicSwapRoute{}, false
	}
	return exchangeAtomicSwapRoute{
		direction:   bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		outputDenom: m.outputDenom,
		amountOut:   m.amountOut,
		feeAmount:   m.feeAmount,
	}, true
}

func (*exchangeAtomicBexKeeper) WithReserveReceiveAllowance(ctx context.Context, _ uint64) context.Context {
	return ctx
}

func (m *exchangeAtomicBexKeeper) RecordVolumeWindow(ctx context.Context, _ uint64, direction bexv1.SwapDirection, amountOut sdkmath.Int) error {
	store := exchangeAtomicStore(ctx, m.storeService)
	store.Set(exchangeAtomicKey("bex", "volume"), []byte(m.recordedVolume(sdk.UnwrapSDKContext(ctx)).Add(amountOut).String()))
	store.Set(
		exchangeAtomicKey("bex", "volume", strconv.Itoa(int(direction))),
		[]byte(m.recordedVolumeForDirection(sdk.UnwrapSDKContext(ctx), direction).Add(amountOut).String()),
	)
	if m.failRecordVolume {
		return errExchangeAtomicRecordVolume
	}
	return nil
}

func (m *exchangeAtomicBexKeeper) AddCollectedFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.addLedgerCoin(ctx, "collected", fee); err != nil {
		return err
	}
	if m.failAddCollectedFee {
		return errExchangeAtomicAddFee
	}
	return nil
}

func (m *exchangeAtomicBexKeeper) LockExchangeFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.addLedgerCoin(ctx, "locked", fee); err != nil {
		return err
	}
	if m.failLockExchangeFee {
		return errExchangeAtomicLockFee
	}
	return nil
}

func (m *exchangeAtomicBexKeeper) ReleaseExchangeFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	return m.addLedgerCoin(ctx, "released", fee)
}

func (m *exchangeAtomicBexKeeper) DeductCollectedFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	return m.addLedgerCoin(ctx, "deducted", fee)
}

func (m *exchangeAtomicBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return m.reserve
}

func (m *exchangeAtomicBexKeeper) addLedgerCoin(ctx context.Context, kind string, coin sdk.Coin) error {
	current := m.ledger(sdk.UnwrapSDKContext(ctx), kind)
	exchangeAtomicStore(ctx, m.storeService).Set(exchangeAtomicKey("bex", "ledger", kind), []byte(current.Add(coin).String()))
	return nil
}

func (m *exchangeAtomicBexKeeper) ledger(ctx sdk.Context, kind string) sdk.Coins {
	return exchangeAtomicCoins(ctx, m.storeService, exchangeAtomicKey("bex", "ledger", kind))
}

func (m *exchangeAtomicBexKeeper) recordedVolume(ctx sdk.Context) sdkmath.Int {
	return m.recordedVolumeByKey(ctx, exchangeAtomicKey("bex", "volume"))
}

func (m *exchangeAtomicBexKeeper) recordedVolumeForDirection(ctx sdk.Context, direction bexv1.SwapDirection) sdkmath.Int {
	return m.recordedVolumeByKey(ctx, exchangeAtomicKey("bex", "volume", strconv.Itoa(int(direction))))
}

func (m *exchangeAtomicBexKeeper) recordedVolumeByKey(ctx sdk.Context, key []byte) sdkmath.Int {
	bz := exchangeAtomicStore(ctx, m.storeService).Get(key)
	if len(bz) == 0 {
		return sdkmath.ZeroInt()
	}
	amount, ok := sdkmath.NewIntFromString(string(bz))
	if !ok {
		panic("invalid test volume amount")
	}
	return amount
}

type exchangeAtomicBankKeeper struct {
	storeService corestore.KVStoreService
	failSendTo   string
	blockedAddr  string
}

func newExchangeAtomicBankKeeper(storeService corestore.KVStoreService) *exchangeAtomicBankKeeper {
	return &exchangeAtomicBankKeeper{storeService: storeService}
}

func (m *exchangeAtomicBankKeeper) SetBalance(ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	m.setBalance(ctx, addr, coins)
}

func (m *exchangeAtomicBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return exchangeAtomicCoins(sdk.UnwrapSDKContext(ctx), m.storeService, exchangeAtomicKey("bank", addr.String()))
}

func (m *exchangeAtomicBankKeeper) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.failSendTo != "" && toAddr.String() == m.failSendTo {
		return errExchangeAtomicBankSend
	}
	from := m.GetAllBalances(ctx, fromAddr)
	if !exchangeAtomicHasCoins(from, amt) {
		return errors.New("insufficient funds")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	m.setBalance(sdkCtx, fromAddr, from.Sub(amt...))
	m.setBalance(sdkCtx, toAddr, m.GetAllBalances(ctx, toAddr).Add(amt...))
	return nil
}

func (m *exchangeAtomicBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	m.setBalance(sdkCtx, moduleAddr, m.GetAllBalances(ctx, moduleAddr).Add(amt...))
	return nil
}

func (m *exchangeAtomicBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	current := m.GetAllBalances(ctx, moduleAddr)
	if !exchangeAtomicHasCoins(current, amt) {
		return errors.New("insufficient module funds")
	}
	m.setBalance(sdk.UnwrapSDKContext(ctx), moduleAddr, current.Sub(amt...))
	return nil
}

func (m *exchangeAtomicBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return m.SendCoins(ctx, authtypes.NewModuleAddress(senderModule), recipientAddr, amt)
}

func (m *exchangeAtomicBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return m.SendCoins(ctx, senderAddr, authtypes.NewModuleAddress(recipientModule), amt)
}

func (m *exchangeAtomicBankKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return m.blockedAddr != "" && addr.String() == m.blockedAddr
}

func (*exchangeAtomicBankKeeper) IsSendEnabledCoins(context.Context, ...sdk.Coin) error {
	return nil
}

func (*exchangeAtomicBankKeeper) HasDenomMetaData(context.Context, string) bool {
	return false
}

func (*exchangeAtomicBankKeeper) SetDenomMetaData(context.Context, banktypes.Metadata) {}

func (m *exchangeAtomicBankKeeper) SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.GetAllBalances(ctx, addr).AmountOf(denom))
}

func (m *exchangeAtomicBankKeeper) setBalance(ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	store := exchangeAtomicStore(ctx, m.storeService)
	key := exchangeAtomicKey("bank", addr.String())
	if coins.Empty() {
		store.Delete(key)
		return
	}
	store.Set(key, []byte(coins.Sort().String()))
}

type exchangeAtomicICS4Wrapper struct {
	storeService   corestore.KVStoreService
	sequence       uint64
	failSendPacket bool
}

func (m *exchangeAtomicICS4Wrapper) SendPacket(ctx sdk.Context, sourcePort, sourceChannel string, _ clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	store := exchangeAtomicStore(ctx, m.storeService)
	seq := m.sequence + uint64(m.sentCount(ctx)) //nolint:gosec // test count is bounded.
	store.Set(exchangeAtomicKey("ics4", "count"), []byte(strconv.Itoa(m.sentCount(ctx)+1)))
	store.Set(exchangeAtomicKey("ics4", "data", strconv.FormatUint(seq, 10)), append([]byte(nil), data...))
	store.Set(exchangeAtomicKey("ics4", "source", strconv.FormatUint(seq, 10)), []byte(sourcePort+"/"+sourceChannel+"/"+strconv.FormatUint(timeoutTimestamp, 10)))
	if m.failSendPacket {
		return 0, errExchangeAtomicSendPacket
	}
	return seq, nil
}

func (*exchangeAtomicICS4Wrapper) WriteAcknowledgement(sdk.Context, ibcexported.PacketI, ibcexported.Acknowledgement) error {
	return nil
}

func (*exchangeAtomicICS4Wrapper) GetAppVersion(sdk.Context, string, string) (string, bool) {
	return types.V1, true
}

func (m *exchangeAtomicICS4Wrapper) sentCount(ctx sdk.Context) int {
	bz := exchangeAtomicStore(ctx, m.storeService).Get(exchangeAtomicKey("ics4", "count"))
	if len(bz) == 0 {
		return 0
	}
	count, err := strconv.Atoi(string(bz))
	if err != nil {
		panic(err)
	}
	return count
}

func (m *exchangeAtomicICS4Wrapper) sentPacketData(ctx sdk.Context, sequence uint64) []byte {
	bz := exchangeAtomicStore(ctx, m.storeService).Get(exchangeAtomicKey("ics4", "data", strconv.FormatUint(sequence, 10)))
	return append([]byte(nil), bz...)
}

type exchangeAtomicChannelKeeper struct {
	portID     string
	channelIDs map[string]bool
}

func (m exchangeAtomicChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	if portID != m.portID || !m.channelIDs[channelID] {
		return channeltypes.Channel{}, false
	}
	return channeltypes.Channel{
		State: channeltypes.OPEN,
		Counterparty: channeltypes.Counterparty{
			PortId:    "xswap",
			ChannelId: channelID + "-counterparty",
		},
	}, true
}

func (exchangeAtomicChannelKeeper) GetNextSequenceSend(sdk.Context, string, string) (uint64, bool) {
	return 0, false
}

func (exchangeAtomicChannelKeeper) GetAllChannelsWithPortPrefix(sdk.Context, string) []channeltypes.IdentifiedChannel {
	return nil
}

func (m exchangeAtomicChannelKeeper) HasChannel(ctx sdk.Context, portID, channelID string) bool {
	_, found := m.GetChannel(ctx, portID, channelID)
	return found
}

func exchangeAtomicStore(ctx context.Context, storeService corestore.KVStoreService) storetypes.KVStore {
	return runtime.KVStoreAdapter(storeService.OpenKVStore(ctx))
}

func exchangeAtomicCoins(ctx sdk.Context, storeService corestore.KVStoreService, key []byte) sdk.Coins {
	bz := exchangeAtomicStore(ctx, storeService).Get(key)
	if len(bz) == 0 {
		return sdk.Coins{}
	}
	coins, err := sdk.ParseCoinsNormalized(string(bz))
	if err != nil {
		panic(err)
	}
	return coins
}

func exchangeAtomicHasCoins(balance sdk.Coins, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}

func exchangeAtomicKey(parts ...string) []byte {
	return []byte("\xffexatom/" + strings.Join(parts, "/"))
}

var _ porttypes.ICS4Wrapper = (*exchangeAtomicICS4Wrapper)(nil)
