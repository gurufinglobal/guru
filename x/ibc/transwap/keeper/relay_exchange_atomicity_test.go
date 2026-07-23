package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	connectiontypes "github.com/cosmos/ibc-go/v11/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const exchangeAtomicSequence = uint64(77)

var (
	errExchangeAtomicRecordVolume = errors.New("test volume record failure")
	errExchangeAtomicSendPacket   = errors.New("test send packet failure")
	errExchangeAtomicQuote        = errors.New("test quote failure")
	errExchangeAtomicBankSend     = errors.New("test bank send failure")
	errExchangeAtomicCollectFee   = errors.New("test collect fee failure")
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

	refund, found, err := state.keeper.GetRefundRecord(state.ctx, RefundID(types.PortID, "channel-7", exchangeAtomicSequence))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.RefundStatus_REFUND_STATUS_PENDING, refund.Status)
	require.Equal(t, state.sender.String(), refund.Receiver)
	require.Equal(t, "7", refund.ExchangeId)
	require.Equal(t, state.inputIBCDenom, refund.OriginalFee.Denom)
	require.Equal(t, "3", refund.OriginalFee.Amount.String())
	require.Equal(t, "103", refund.Token.Amount)

	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(103))), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), locked)

	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(100), state.bex.recordedVolume(state.ctx))
	require.True(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestExchangeEscrowUnderflowIsRejectedBeforeBankMovement(t *testing.T) {
	t.Run("received input", func(t *testing.T) {
		state := setupExchangeReceiveAtomicity(t, false)
		coin := sdk.NewInt64Coin("uatom", 100)
		escrow := types.GetEscrowAddress(types.PortID, "channel-0")
		state.bank.SetBalance(state.ctx, escrow, sdk.NewCoins(coin))
		state.keeper.SetTotalEscrowForDenom(state.ctx, sdk.NewInt64Coin(coin.Denom, 99))
		data := types.NewInternalTransferRepresentation(
			"7",
			&types.Token{
				Denom:  types.NewDenom(coin.Denom, types.NewHop("xswap", "channel-1")),
				Amount: coin.Amount.String(),
			},
			state.sender.String(),
			state.receiver.String(),
			"",
		)

		err := state.keeper.receiveTokensToReserve(
			state.ctx,
			7,
			data,
			"xswap",
			"channel-1",
			types.PortID,
			"channel-0",
		)
		require.ErrorIs(t, err, types.ErrRefundEscrowInvariant)
		require.Equal(t, coin.Amount, state.bank.GetAllBalances(state.ctx, escrow).AmountOf(coin.Denom))
		require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(coin.Denom).IsZero())
	})

	t.Run("failed output", func(t *testing.T) {
		state := setupExchangeReceiveAtomicity(t, false)
		coin := sdk.NewInt64Coin("atgxkrw", 100)
		escrow := types.GetEscrowAddress(types.PortID, "channel-7")
		state.bank.SetBalance(state.ctx, escrow, sdk.NewCoins(coin))
		state.keeper.SetTotalEscrowForDenom(state.ctx, sdk.NewInt64Coin(coin.Denom, 99))
		data := types.NewInternalTransferRepresentation(
			"0",
			&types.Token{Denom: types.NewDenom(coin.Denom), Amount: coin.Amount.String()},
			state.reserve.String(),
			state.receiver.String(),
			"",
		)

		err := state.keeper.refundPacketTokensToReserve(
			state.ctx,
			7,
			types.PortID,
			"channel-7",
			data,
		)
		require.ErrorIs(t, err, types.ErrRefundEscrowInvariant)
		require.Equal(t, coin.Amount, state.bank.GetAllBalances(state.ctx, escrow).AmountOf(coin.Denom))
		require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(coin.Denom).IsZero())
	})
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
	refund, found, err := state.keeper.GetRefundRecord(state.ctx, RefundID(types.PortID, "channel-7", exchangeAtomicSequence))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, state.inputIBCDenom, refund.OriginalFee.Denom)
	require.Equal(t, "0", refund.OriginalFee.Amount.String())
	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(103))), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Empty(t, locked)
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.Equal(t, sdkmath.NewInt(100), state.bex.recordedVolume(state.ctx))
}

func TestOnRecvExchangePacketEnforcesOptionalMemoProtection(t *testing.T) {
	tests := []struct {
		name               string
		memo               string
		expectedErr        error
		nilQuote           bool
		expectedQuoteCalls int
	}{
		{name: "minimum only", memo: `guru.transwap.protection:v1:{"min_amount_out":"100"}`, expectedQuoteCalls: 1},
		{name: "revision only", memo: `guru.transwap.protection:v1:{"expected_exchange_revision":"11"}`, expectedQuoteCalls: 1},
		{name: "both", memo: `guru.transwap.protection:v1:{"min_amount_out":"100","expected_exchange_revision":"11"}`, expectedQuoteCalls: 1},
		{name: "minimum not met", memo: `guru.transwap.protection:v1:{"min_amount_out":"101"}`, expectedErr: types.ErrMinimumAmountOut, expectedQuoteCalls: 1},
		{name: "stale revision", memo: `guru.transwap.protection:v1:{"expected_exchange_revision":"10"}`, expectedErr: bextypes.ErrRevisionConflict, expectedQuoteCalls: 1},
		{name: "malformed field", memo: `guru.transwap.protection:v1:{"min_amount_out":100}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "BOM before marker", memo: "\uFEFF" + `guru.transwap.protection:v1:{"min_amount_out":"100"}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "NUL before marker", memo: "\x00" + `guru.transwap.protection:v1:{"min_amount_out":"100"}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "arbitrary prefix before marker", memo: `xguru.transwap.protection:v1:{"min_amount_out":"100"}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "duplicate protection field", memo: `guru.transwap.protection:v1:{"min_amount_out":"100","min_amount_out":"1"}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "trailing object", memo: `guru.transwap.protection:v1:{"min_amount_out":"100"}{}`, expectedErr: types.ErrInvalidSwapProtection},
		{name: "nil quote", expectedErr: bextypes.ErrInvariantViolation, nilQuote: true, expectedQuoteCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := setupExchangeReceiveAtomicity(t, false)
			state.packetData.Memo = tt.memo
			state.bex.nilQuote = tt.nilQuote

			err := state.keeper.OnRecvExchangePacket(
				state.ctx,
				state.packetData,
				"xswap",
				"channel-1",
				types.PortID,
				"channel-0",
				uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
			)
			if tt.expectedErr == nil {
				require.NoError(t, err)
				require.Equal(t, tt.expectedQuoteCalls, state.bex.quoteCalls)
				require.Equal(t, 1, state.ics4.sentCount(state.ctx))
				return
			}

			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedQuoteCalls, state.bex.quoteCalls)
			require.Zero(t, state.ics4.sentCount(state.ctx))
			requireNoExchangeRefundState(t, state, "channel-7")
			require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
			require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
			require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
			require.Empty(t, state.bex.ledger(state.ctx, "collected"))
			require.Empty(t, state.bex.ledger(state.ctx, "locked"))
			require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
			require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
		})
	}
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

		refund, found, err := state.keeper.GetRefundRecord(state.ctx, RefundID(types.PortID, "channel-7", sequence))
		require.NoError(t, err)
		require.True(t, found)
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
	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(206))), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(6))), locked)
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolume(state.ctx))
}

func TestOnRecvExchangePacketCommitsSmallAndLargeAmounts(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.quoteByAmount = map[string]exchangeAtomicSwapRoute{
		"1": {
			direction:   bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
			outputDenom: state.outputIBCDenom,
			amountOut:   "1",
			feeAmount:   "0",
		},
		"1000003": {
			direction:   bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
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

		refund, found, err := state.keeper.GetRefundRecord(state.ctx, RefundID(types.PortID, "channel-7", expected.sequence))
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.inputIBCDenom, refund.OriginalFee.Denom)
		require.Equal(t, expected.feeAmount, refund.OriginalFee.Amount.String())
	}

	reserveBalances := state.bank.GetAllBalances(state.ctx, state.reserve)
	require.Equal(t, sdkmath.NewInt(299), reserveBalances.AmountOf(state.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(1000001), reserveBalances.AmountOf(state.inputIBCDenom))
	require.Equal(t, sdkmath.NewInt(3), state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(state.inputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "locked"))
	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(1_000_004))), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), locked)
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
			direction:   bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
			outputDenom: state.outputIBCDenom,
			amountOut:   "100",
			feeAmount:   "3",
		},
		"atgxkrw": {
			direction:   bextypes.SwapDirection_SWAP_DIRECTION_B_TO_A,
			outputDenom: state.inputIBCDenom,
			amountOut:   "100",
			feeAmount:   "3",
		},
	}
	state.keeper.channelKeeper = exchangeAtomicChannelKeeper{
		portID:     types.PortID,
		channelIDs: map[string]bool{"channel-0": true, "channel-7": true},
		ics4:       state.ics4,
	}

	reversePacketData := types.NewInternalTransferRepresentation(
		"7",
		&types.Token{Denom: types.NewDenom("atgxkrw"), Amount: "103"},
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
	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(206)),
		sdk.NewCoin(state.outputIBCDenom, sdkmath.NewInt(206)),
	), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, expectedFees, locked)
	require.Equal(t, sdkmath.NewInt(400), state.bex.recordedVolume(state.ctx))
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolumeForDirection(state.ctx, bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B))
	require.Equal(t, sdkmath.NewInt(200), state.bex.recordedVolumeForDirection(state.ctx, bextypes.SwapDirection_SWAP_DIRECTION_B_TO_A))
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

		refund, found, err := state.keeper.GetRefundRecord(state.ctx, RefundID(types.PortID, expected.channel, expected.sequence))
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.sender.String(), refund.Receiver)
		require.Equal(t, "7", refund.ExchangeId)
		require.Equal(t, expected.feeDenom, refund.OriginalFee.Denom)
		require.Equal(t, "3", refund.OriginalFee.Amount.String())
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-8")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
		ics4:       state.ics4,
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
	require.Empty(t, state.bex.ledger(state.ctx, "released"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(3))), state.bex.ledger(state.ctx, "refunded"))
	pending, err := state.bex.GetPendingLiabilities(state.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(state.inputIBCDenom, sdkmath.NewInt(103))), pending)
	locked, err := state.bex.GetLockedFees(state.ctx, 7)
	require.NoError(t, err)
	require.Empty(t, locked)

	refund, found, err := state.keeper.GetRefundRecord(
		state.ctx,
		RefundID(types.PortID, "channel-7", exchangeAtomicSequence),
	)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.RefundStatus_REFUND_STATUS_IN_FLIGHT, refund.Status)
	require.Equal(t, uint32(1), refund.RetryCount)
	require.Equal(t, exchangeAtomicSequence+1, refund.ActivePacketSequence)
	require.NotEqual(t, refund.OriginalTimeoutTimestamp, refund.ActiveTimeoutTimestamp)

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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	require.ErrorIs(t, err, bextypes.ErrInsufficientReserve)

	require.Equal(t, sdkmath.NewInt(50), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
	require.Empty(t, state.bex.ledger(state.ctx, "collected"))
	require.Empty(t, state.bex.ledger(state.ctx, "locked"))
	require.True(t, state.bex.recordedVolume(state.ctx).IsZero())
	require.False(t, state.keeper.HasDenom(state.ctx, types.DenomHash(state.inputTokenDenom)))
}

func TestOnRecvExchangePacketRollsBackStateWhenCollectFeeFailsAfterWrite(t *testing.T) {
	state := setupExchangeReceiveAtomicity(t, false)
	state.bex.failCollectFee = true

	err := state.keeper.OnRecvExchangePacket(
		state.ctx,
		state.packetData,
		"xswap",
		"channel-1",
		types.PortID,
		"channel-0",
		uint64(state.ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	)
	require.ErrorIs(t, err, errExchangeAtomicCollectFee)

	require.Equal(t, sdkmath.NewInt(1000), state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.outputIBCDenom))
	require.True(t, state.bank.GetAllBalances(state.ctx, state.reserve).AmountOf(state.inputIBCDenom).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, state.bank.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Zero(t, state.ics4.sentCount(state.ctx))
	requireNoExchangeRefundState(t, state, "channel-7")
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
	requireNoExchangeRefundState(t, state, "channel-7")
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
	inputTokenDenom  types.Denom
	inputIBCDenom    string
	outputTokenDenom types.Denom
	outputIBCDenom   string
	packetData       types.InternalTransferRepresentation
}

func requireNoExchangeRefundState(t *testing.T, state exchangeReceiveAtomicityState, outputChannel string) {
	t.Helper()

	_, found, err := state.keeper.GetRefundRecord(
		state.ctx,
		RefundID(types.PortID, outputChannel, exchangeAtomicSequence),
	)
	require.NoError(t, err)
	require.False(t, found)

	pending, err := state.bex.GetPendingLiabilities(state.ctx, state.bex.expectedExchangeID)
	require.NoError(t, err)
	require.Empty(t, pending)
	locked, err := state.bex.GetLockedFees(state.ctx, state.bex.expectedExchangeID)
	require.NoError(t, err)
	require.Empty(t, locked)
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
		bank:               bank,
		outputDenom:        outputIBCDenom,
		feeAmount:          "3",
		amountOut:          "100",
		failRecordVolume:   failRecordVolume,
		expectedExchangeID: 7,
		exchangeRevision:   11,
	}
	ics4 := &exchangeAtomicICS4Wrapper{storeService: k.storeService, sequence: exchangeAtomicSequence}

	k.BankKeeper = bank
	k.BexKeeper = bex
	k.ics4Wrapper = ics4
	k.channelKeeper = exchangeAtomicChannelKeeper{
		portID:     types.PortID,
		channelIDs: map[string]bool{"channel-0": true, "channel-7": true},
		ics4:       ics4,
	}
	k.WithIBCClientKeepers(
		exchangeAtomicConnectionKeeper{},
		exchangeAtomicClientKeeper{timestamp: ctx.BlockTime()},
	)

	packetData := types.NewInternalTransferRepresentation(
		"7",
		&types.Token{Denom: types.NewDenom("atgxusd"), Amount: "103"},
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
	bank                *exchangeAtomicBankKeeper
	outputDenom         string
	feeAmount           string
	amountOut           string
	routes              map[string]exchangeAtomicSwapRoute
	quoteByAmount       map[string]exchangeAtomicSwapRoute
	failRecordVolume    bool
	failCollectFee      bool
	failLockExchangeFee bool
	quoteErr            error
	nilQuote            bool
	quoteCalls          int
	expectedExchangeID  uint64
	exchangeRevision    uint64
}

type exchangeAtomicSwapRoute struct {
	direction   bextypes.SwapDirection
	outputDenom string
	amountOut   string
	feeAmount   string
}

func (m *exchangeAtomicBexKeeper) ValidateSwapInput(_ context.Context, exchangeID uint64, inputDenom, _ string) (bextypes.SwapDirection, error) {
	route, ok := m.routeForInputDenom(inputDenom)
	if exchangeID != m.expectedExchangeID || !ok {
		return bextypes.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, bextypes.ErrInvalidRoute.Wrap("unexpected exchange route")
	}
	return route.direction, nil
}

func (m *exchangeAtomicBexKeeper) QuoteSwap(_ context.Context, req *bextypes.QuoteSwapRequest) (*bextypes.QuoteSwapResponse, error) {
	m.quoteCalls++
	if m.quoteErr != nil {
		return nil, m.quoteErr
	}
	if m.nilQuote {
		return nil, nil
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
	return &bextypes.QuoteSwapResponse{
		ExchangeId:       req.GetExchangeId(),
		Direction:        route.direction,
		InputDenom:       req.GetInputDenom(),
		OutputDenom:      route.outputDenom,
		AmountIn:         req.GetAmountIn(),
		FeeAmount:        route.feeAmount,
		NetAmountIn:      "100",
		AmountOut:        route.amountOut,
		ExchangeRevision: m.exchangeRevision,
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
		direction:   bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
		outputDenom: m.outputDenom,
		amountOut:   m.amountOut,
		feeAmount:   m.feeAmount,
	}, true
}

func (m *exchangeAtomicBexKeeper) ReceiveToReserve(ctx context.Context, _ uint64, from sdk.AccAddress, amount sdk.Coins) error {
	if m.bank.BlockedAddr(m.reserve) {
		return fmt.Errorf("%s is not allowed to receive funds", m.reserve)
	}
	return m.bank.SendCoins(ctx, from, m.reserve, amount)
}

func (m *exchangeAtomicBexKeeper) SendSwapOutputFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	pending, err := m.GetPendingLiabilities(ctx, exchangeID)
	if err != nil {
		return err
	}
	available := m.bank.GetAllBalances(ctx, m.reserve).AmountOf(amount.Denom).Sub(pending.AmountOf(amount.Denom))
	if available.IsNegative() || available.LT(amount.Amount) {
		return bextypes.ErrInsufficientReserve.Wrapf("refund liabilities reserve %s", amount.Denom)
	}
	return m.bank.SendCoins(ctx, m.reserve, recipient, sdk.NewCoins(amount))
}

func (m *exchangeAtomicBexKeeper) SendRefundFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	pending, err := m.GetPendingLiabilities(ctx, exchangeID)
	if err != nil {
		return err
	}
	if pending.AmountOf(amount.Denom).LT(amount.Amount) {
		return bextypes.ErrInvariantViolation.Wrap("refund send exceeds pending liability")
	}
	return m.bank.SendCoins(ctx, m.reserve, recipient, sdk.NewCoins(amount))
}

func (m *exchangeAtomicBexKeeper) ClaimRefundFromReserve(
	ctx context.Context,
	exchangeID uint64,
	recipient sdk.AccAddress,
	amount sdk.Coin,
) error {
	cacheCtx, writeCache := sdk.UnwrapSDKContext(ctx).CacheContext()
	if _, err := m.GetPendingLiabilities(cacheCtx, exchangeID); err != nil {
		return err
	}
	if m.bank.BlockedAddr(recipient) {
		return errors.New("refund recipient is blocked")
	}
	if err := m.subtractLiveLedgerCoin(cacheCtx, "pending_current", amount); err != nil {
		return err
	}
	if err := m.bank.SendCoins(cacheCtx, m.reserve, recipient, sdk.NewCoins(amount)); err != nil {
		return err
	}
	if err := m.addLedgerCoin(cacheCtx, "liability_claimed", amount); err != nil {
		return err
	}
	writeCache()
	return nil
}

func (m *exchangeAtomicBexKeeper) ReserveVolumeWindow(
	ctx context.Context,
	exchangeID uint64,
	direction bextypes.SwapDirection,
	amountOut sdkmath.Int,
) (*bextypes.VolumeReservation, error) {
	store := exchangeAtomicStore(ctx, m.storeService)
	store.Set(exchangeAtomicKey("bex", "volume"), []byte(m.recordedVolume(sdk.UnwrapSDKContext(ctx)).Add(amountOut).String()))
	store.Set(
		exchangeAtomicKey("bex", "volume", strconv.Itoa(int(direction))),
		[]byte(m.recordedVolumeForDirection(sdk.UnwrapSDKContext(ctx), direction).Add(amountOut).String()),
	)
	if m.failRecordVolume {
		return nil, errExchangeAtomicRecordVolume
	}
	return &bextypes.VolumeReservation{
		ExchangeId:             exchangeID,
		Direction:              direction,
		EpochSeconds:           bextypes.MinVolumeEpochSeconds,
		Amount:                 amountOut.String(),
		VolumeWindowGeneration: 1,
	}, nil
}

func (m *exchangeAtomicBexKeeper) ReleaseVolumeWindow(ctx context.Context, reservation *bextypes.VolumeReservation) error {
	amount, ok := sdkmath.NewIntFromString(reservation.GetAmount())
	if !ok {
		return bextypes.ErrInvariantViolation.Wrap("invalid volume reservation amount")
	}
	current := m.recordedVolume(sdk.UnwrapSDKContext(ctx))
	currentDirection := m.recordedVolumeForDirection(sdk.UnwrapSDKContext(ctx), reservation.GetDirection())
	if current.LT(amount) || currentDirection.LT(amount) {
		return bextypes.ErrInvariantViolation.Wrap("volume release exceeds recorded volume")
	}
	store := exchangeAtomicStore(ctx, m.storeService)
	store.Set(exchangeAtomicKey("bex", "volume"), []byte(current.Sub(amount).String()))
	store.Set(
		exchangeAtomicKey("bex", "volume", strconv.Itoa(int(reservation.GetDirection()))),
		[]byte(currentDirection.Sub(amount).String()),
	)
	return nil
}

func (m *exchangeAtomicBexKeeper) CollectFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.addLedgerCoin(ctx, "collected", fee); err != nil {
		return err
	}
	if err := m.bank.SendCoinsFromAccountToModule(ctx, m.reserve, bextypes.ModuleName, sdk.NewCoins(fee)); err != nil {
		return err
	}
	if m.failCollectFee {
		return errExchangeAtomicCollectFee
	}
	return nil
}

func (m *exchangeAtomicBexKeeper) LockExchangeFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.addLiveLedgerCoin(ctx, "locked_current", fee); err != nil {
		return err
	}
	if err := m.addLedgerCoin(ctx, "locked", fee); err != nil {
		return err
	}
	if m.failLockExchangeFee {
		return errExchangeAtomicLockFee
	}
	return nil
}

func (m *exchangeAtomicBexKeeper) ReleaseExchangeFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.subtractLiveLedgerCoin(ctx, "locked_current", fee); err != nil {
		return err
	}
	return m.addLedgerCoin(ctx, "released", fee)
}

func (m *exchangeAtomicBexKeeper) RefundLockedFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.subtractLiveLedgerCoin(ctx, "locked_current", fee); err != nil {
		return err
	}
	if err := m.addLedgerCoin(ctx, "refunded", fee); err != nil {
		return err
	}
	return m.bank.SendCoinsFromModuleToAccount(ctx, bextypes.ModuleName, m.reserve, sdk.NewCoins(fee))
}

func (m *exchangeAtomicBexKeeper) AddPendingLiability(ctx context.Context, _ uint64, liability sdk.Coin) error {
	if err := m.addLiveLedgerCoin(ctx, "pending_current", liability); err != nil {
		return err
	}
	return m.addLedgerCoin(ctx, "pending", liability)
}

func (m *exchangeAtomicBexKeeper) ReleasePendingLiability(ctx context.Context, _ uint64, liability sdk.Coin) error {
	if err := m.subtractLiveLedgerCoin(ctx, "pending_current", liability); err != nil {
		return err
	}
	return m.addLedgerCoin(ctx, "liability_released", liability)
}

func (m *exchangeAtomicBexKeeper) GetPendingLiabilities(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	if exchangeID != m.expectedExchangeID {
		return nil, bextypes.ErrInvalidRoute.Wrapf("unexpected exchange %d", exchangeID)
	}
	return m.ledger(sdk.UnwrapSDKContext(ctx), "pending_current"), nil
}

func (m *exchangeAtomicBexKeeper) GetLockedFees(ctx context.Context, exchangeID uint64) (sdk.Coins, error) {
	if exchangeID != m.expectedExchangeID {
		return nil, bextypes.ErrInvalidRoute.Wrapf("unexpected exchange %d", exchangeID)
	}
	return m.ledger(sdk.UnwrapSDKContext(ctx), "locked_current"), nil
}

func (m *exchangeAtomicBexKeeper) GetRefundAccountingExchangeIDs(ctx context.Context) ([]uint64, error) {
	pending, err := m.GetPendingLiabilities(ctx, m.expectedExchangeID)
	if err != nil {
		return nil, err
	}
	locked, err := m.GetLockedFees(ctx, m.expectedExchangeID)
	if err != nil {
		return nil, err
	}
	if pending.Empty() && locked.Empty() {
		return nil, nil
	}
	return []uint64{m.expectedExchangeID}, nil
}

func (m *exchangeAtomicBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return m.reserve
}

func (m *exchangeAtomicBexKeeper) addLedgerCoin(ctx context.Context, kind string, coin sdk.Coin) error {
	current := m.ledger(sdk.UnwrapSDKContext(ctx), kind)
	exchangeAtomicStore(ctx, m.storeService).Set(exchangeAtomicKey("bex", "ledger", kind), []byte(current.Add(coin).String()))
	return nil
}

func (m *exchangeAtomicBexKeeper) addLiveLedgerCoin(ctx context.Context, kind string, coin sdk.Coin) error {
	return m.addLedgerCoin(ctx, kind, coin)
}

func (m *exchangeAtomicBexKeeper) subtractLiveLedgerCoin(ctx context.Context, kind string, coin sdk.Coin) error {
	current := m.ledger(sdk.UnwrapSDKContext(ctx), kind)
	if current.AmountOf(coin.Denom).LT(coin.Amount) {
		return bextypes.ErrInvariantViolation.Wrapf("%s does not cover %s", kind, coin)
	}
	remaining := current.Sub(coin)
	store := exchangeAtomicStore(ctx, m.storeService)
	key := exchangeAtomicKey("bex", "ledger", kind)
	if remaining.Empty() {
		store.Delete(key)
		return nil
	}
	store.Set(key, []byte(remaining.String()))
	return nil
}

func (m *exchangeAtomicBexKeeper) ledger(ctx sdk.Context, kind string) sdk.Coins {
	return exchangeAtomicCoins(ctx, m.storeService, exchangeAtomicKey("bex", "ledger", kind))
}

func (m *exchangeAtomicBexKeeper) recordedVolume(ctx sdk.Context) sdkmath.Int {
	return m.recordedVolumeByKey(ctx, exchangeAtomicKey("bex", "volume"))
}

func (m *exchangeAtomicBexKeeper) recordedVolumeForDirection(ctx sdk.Context, direction bextypes.SwapDirection) sdkmath.Int {
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
	failSendCount  int
}

func (m *exchangeAtomicICS4Wrapper) SendPacket(ctx sdk.Context, sourcePort, sourceChannel string, _ clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	store := exchangeAtomicStore(ctx, m.storeService)
	seq := m.sequence + uint64(m.sentCount(ctx)) //nolint:gosec // test count is bounded.
	store.Set(exchangeAtomicKey("ics4", "count"), []byte(strconv.Itoa(m.sentCount(ctx)+1)))
	store.Set(exchangeAtomicKey("ics4", "data", strconv.FormatUint(seq, 10)), append([]byte(nil), data...))
	store.Set(exchangeAtomicKey("ics4", "source", strconv.FormatUint(seq, 10)), []byte(sourcePort+"/"+sourceChannel+"/"+strconv.FormatUint(timeoutTimestamp, 10)))
	if m.failSendPacket || m.failSendCount > 0 {
		if m.failSendCount > 0 {
			m.failSendCount--
		}
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
	ics4       *exchangeAtomicICS4Wrapper
}

func (m exchangeAtomicChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	if portID != m.portID || !m.channelIDs[channelID] {
		return channeltypes.Channel{}, false
	}
	return channeltypes.Channel{
		State:          channeltypes.OPEN,
		ConnectionHops: []string{"connection-0"},
		Counterparty: channeltypes.Counterparty{
			PortId:    "xswap",
			ChannelId: channelID + "-counterparty",
		},
	}, true
}

type exchangeAtomicConnectionKeeper struct{}

func (exchangeAtomicConnectionKeeper) GetConnection(_ sdk.Context, connectionID string) (connectiontypes.ConnectionEnd, bool) {
	if connectionID != "connection-0" {
		return connectiontypes.ConnectionEnd{}, false
	}
	return connectiontypes.ConnectionEnd{ClientId: "client-0"}, true
}

type exchangeAtomicClientKeeper struct {
	timestamp time.Time
}

func (m exchangeAtomicClientKeeper) GetLatestClientConsensusState(_ sdk.Context, clientID string) (ibcexported.ConsensusState, bool) {
	if clientID != "client-0" {
		return nil, false
	}
	return &ibctm.ConsensusState{Timestamp: m.timestamp}, true
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

func (m exchangeAtomicChannelKeeper) GetPacketCommitment(ctx sdk.Context, portID, channelID string, sequence uint64) []byte {
	if m.ics4 == nil {
		return nil
	}

	store := exchangeAtomicStore(ctx, m.ics4.storeService)
	data := store.Get(exchangeAtomicKey("ics4", "data", strconv.FormatUint(sequence, 10)))
	source := store.Get(exchangeAtomicKey("ics4", "source", strconv.FormatUint(sequence, 10)))
	parts := strings.Split(string(source), "/")
	if len(data) == 0 || len(parts) != 3 || parts[0] != portID || parts[1] != channelID {
		return nil
	}
	timeoutTimestamp, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil
	}

	return channeltypes.CommitPacket(channeltypes.NewPacket(
		data,
		sequence,
		portID,
		channelID,
		"",
		"",
		clienttypes.ZeroHeight(),
		timeoutTimestamp,
	))
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
