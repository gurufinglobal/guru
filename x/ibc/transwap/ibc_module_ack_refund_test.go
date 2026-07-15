package transwap

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2"
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
	keeperpkg "github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const moduleAckRefundSequence = uint64(77)

func TestIBCModuleErrorAcknowledgementRefundsExchangeAndCreatesRetry(t *testing.T) {
	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	im := NewIBCModule(k)

	reserve := bex.reserve
	originalSender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	targetReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	inputDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	outputDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	inputIBCDenom := types.DenomIBCDenom(inputDenom)
	outputIBCDenom := types.DenomIBCDenom(outputDenom)

	bank.SetBalance(reserve, sdk.NewCoins(
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(100)),
		sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(900)),
	))
	bank.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))))

	refundPacket := types.NewTransferPacketData(
		types.PortID,
		"channel-0",
		&transwapv1.Token{Denom: inputDenom, Amount: "103"},
		reserve.String(),
		originalSender.String(),
		"refund coins through Guru station due to failure on the target chain",
		uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3)),
		"7",
	)
	originalKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-7", moduleAckRefundSequence)
	require.NoError(t, k.SetRefundPacketData(ctx, originalKey, refundPacket))

	outboundPacket := types.NewFungibleTokenPacketData(
		types.DenomPath(outputDenom),
		"100",
		reserve.String(),
		targetReceiver.String(),
		"Station exchange",
	)
	packet := channeltypes.Packet{
		Sequence:           moduleAckRefundSequence,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-7",
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               types.FungibleTokenPacketDataBytes(outboundPacket),
	}
	ack := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	ackBz := types.ModuleCdc.MustMarshalJSON(&ack)

	require.NoError(t, im.OnAcknowledgementPacket(ctx, types.V1, packet, ackBz, sdk.AccAddress{}))

	reserveBalances := bank.GetAllBalances(ctx, reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(outputIBCDenom))
	require.True(t, reserveBalances.AmountOf(inputIBCDenom).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Empty(t, bex.ledger("released"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))), bex.ledger("refunded"))

	require.False(t, k.HasRefundPacketData(ctx, originalKey))
	retryKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", ics4.sequence)
	retry, err := k.GetRefundPacketData(ctx, retryKey)
	require.NoError(t, err)
	require.Equal(t, reserve.String(), retry.Sender)
	require.Equal(t, originalSender.String(), retry.Receiver)
	require.Nil(t, retry.Fee)

	require.Len(t, ics4.sent, 1)
	retryData, err := types.UnmarshalPacketData(ics4.sent[0].data, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, reserve.String(), retryData.Sender)
	require.Equal(t, originalSender.String(), retryData.Receiver)
	require.Equal(t, "103", retryData.Token.Amount)
	require.Equal(t, types.DenomPath(inputDenom), types.DenomPath(retryData.Token.Denom))

	foundAckEvent := false
	for _, event := range ctx.EventManager().Events() {
		for _, attr := range event.Attributes {
			if attr.Key == types.AttributeKeyAckError {
				foundAckEvent = true
				require.Contains(t, attr.Value, "error handling packet")
			}
		}
	}
	require.True(t, foundAckEvent)
}

func TestIBCModuleTimeoutRefundsExchangeAndCreatesRetry(t *testing.T) {
	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	im := NewIBCModule(k)

	reserve := bex.reserve
	originalSender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	targetReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	inputDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	outputDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	inputIBCDenom := types.DenomIBCDenom(inputDenom)
	outputIBCDenom := types.DenomIBCDenom(outputDenom)

	bank.SetBalance(reserve, sdk.NewCoins(
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(100)),
		sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(900)),
	))
	bank.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))))

	refundPacket := types.NewTransferPacketData(
		types.PortID,
		"channel-0",
		&transwapv1.Token{Denom: inputDenom, Amount: "103"},
		reserve.String(),
		originalSender.String(),
		"refund coins through Guru station due to failure on the target chain",
		uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3)),
		"7",
	)
	originalKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-7", moduleAckRefundSequence)
	require.NoError(t, k.SetRefundPacketData(ctx, originalKey, refundPacket))

	outboundPacket := types.NewFungibleTokenPacketData(
		types.DenomPath(outputDenom),
		"100",
		reserve.String(),
		targetReceiver.String(),
		"Station exchange",
	)
	packet := channeltypes.Packet{
		Sequence:           moduleAckRefundSequence,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-7",
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               types.FungibleTokenPacketDataBytes(outboundPacket),
	}

	require.NoError(t, im.OnTimeoutPacket(ctx, types.V1, packet, sdk.AccAddress{}))

	reserveBalances := bank.GetAllBalances(ctx, reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(outputIBCDenom))
	require.True(t, reserveBalances.AmountOf(inputIBCDenom).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	require.Empty(t, bex.ledger("released"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))), bex.ledger("refunded"))

	require.False(t, k.HasRefundPacketData(ctx, originalKey))
	retryKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", ics4.sequence)
	retry, err := k.GetRefundPacketData(ctx, retryKey)
	require.NoError(t, err)
	require.Equal(t, reserve.String(), retry.Sender)
	require.Equal(t, originalSender.String(), retry.Receiver)
	require.Nil(t, retry.Fee)

	require.Len(t, ics4.sent, 1)
	retryData, err := types.UnmarshalPacketData(ics4.sent[0].data, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, reserve.String(), retryData.Sender)
	require.Equal(t, originalSender.String(), retryData.Receiver)
	require.Equal(t, "103", retryData.Token.Amount)
	require.Equal(t, types.DenomPath(inputDenom), types.DenomPath(retryData.Token.Denom))

	foundTimeoutEvent := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type != types.EventTypeTimeout {
			continue
		}
		foundTimeoutEvent = true
		for _, attr := range event.Attributes {
			if attr.Key == types.AttributeKeyReceiver {
				require.Equal(t, reserve.String(), attr.Value)
			}
		}
	}
	require.True(t, foundTimeoutEvent)
}

func TestIBCModuleRetrySuccessAckClearsFeeFreeMetadata(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	originalAck := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		types.ModuleCdc.MustMarshalJSON(&originalAck),
		sdk.AccAddress{},
	))

	retryPacket := scenario.retryPacket(t, 0)
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		retryPacket,
		types.ModuleCdc.MustMarshalJSON(&successAck),
		sdk.AccAddress{},
	))

	scenario.requirePostOriginalRefundState(t)
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, scenario.originalKey))
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[0].sequence)))
	require.Len(t, scenario.ics4.sent, 1)
}

func TestIBCModuleRetryTimeoutCreatesNextFeeFreeRetryWithoutDoubleFee(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	originalAck := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		types.ModuleCdc.MustMarshalJSON(&originalAck),
		sdk.AccAddress{},
	))

	retryPacket := scenario.retryPacket(t, 0)
	require.NoError(t, scenario.im.OnTimeoutPacket(scenario.ctx, types.V1, retryPacket, sdk.AccAddress{}))

	scenario.requirePostOriginalRefundState(t)
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, scenario.originalKey))
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[0].sequence)))
	require.Len(t, scenario.ics4.sent, 2)

	nextRetryKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[1].sequence)
	nextRetry, err := scenario.k.GetRefundPacketData(scenario.ctx, nextRetryKey)
	require.NoError(t, err)
	require.Equal(t, scenario.reserve.String(), nextRetry.Sender)
	require.Equal(t, scenario.originalSender.String(), nextRetry.Receiver)
	require.Equal(t, "7", nextRetry.ExchangeId)
	require.Nil(t, nextRetry.Fee)

	nextRetryData, err := types.UnmarshalPacketData(scenario.ics4.sent[1].data, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, scenario.reserve.String(), nextRetryData.Sender)
	require.Equal(t, scenario.originalSender.String(), nextRetryData.Receiver)
	require.Equal(t, "103", nextRetryData.Token.Amount)
	require.Equal(t, types.DenomPath(scenario.inputDenom), types.DenomPath(nextRetryData.Token.Denom))
}

func TestIBCModuleDuplicateOriginalErrorAckAfterRetryIsNoop(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	originalAck := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	ackBz := types.ModuleCdc.MustMarshalJSON(&originalAck)

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		ackBz,
		sdk.AccAddress{},
	))
	scenario.requirePostOriginalRefundState(t)

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		ackBz,
		sdk.AccAddress{},
	))

	scenario.requirePostOriginalRefundState(t)
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, scenario.originalKey))
	require.True(t, scenario.k.HasRefundPacketData(scenario.ctx, keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[0].sequence)))
	require.Len(t, scenario.ics4.sent, 1)
}

func TestIBCModuleDuplicateOriginalTimeoutAfterRetryIsNoop(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)

	require.NoError(t, scenario.im.OnTimeoutPacket(scenario.ctx, types.V1, scenario.outboundPacket, sdk.AccAddress{}))
	scenario.requirePostOriginalRefundState(t)

	require.NoError(t, scenario.im.OnTimeoutPacket(scenario.ctx, types.V1, scenario.outboundPacket, sdk.AccAddress{}))

	scenario.requirePostOriginalRefundState(t)
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, scenario.originalKey))
	require.True(t, scenario.k.HasRefundPacketData(scenario.ctx, keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[0].sequence)))
	require.Len(t, scenario.ics4.sent, 1)
}

func TestIBCModuleTimeoutAfterRetrySuccessAckIsNoop(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	originalAck := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		types.ModuleCdc.MustMarshalJSON(&originalAck),
		sdk.AccAddress{},
	))

	retryPacket := scenario.retryPacket(t, 0)
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		retryPacket,
		types.ModuleCdc.MustMarshalJSON(&successAck),
		sdk.AccAddress{},
	))

	require.NoError(t, scenario.im.OnTimeoutPacket(scenario.ctx, types.V1, retryPacket, sdk.AccAddress{}))

	scenario.requirePostOriginalRefundState(t)
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, scenario.originalKey))
	require.False(t, scenario.k.HasRefundPacketData(scenario.ctx, keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-0", scenario.ics4.sent[0].sequence)))
	require.Len(t, scenario.ics4.sent, 1)
}

func TestIBCModuleInvalidAcknowledgementDoesNotMutateRefundState(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	invalidAck := channeltypes.Acknowledgement{}
	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		types.ModuleCdc.MustMarshalJSON(&invalidAck),
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireInitialRefundState(t)
}

func TestIBCModuleNonCanonicalAcknowledgementDoesNotMutateRefundState(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	ack := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))
	canonical := types.ModuleCdc.MustMarshalJSON(&ack)
	nonCanonical := append([]byte(" \n\t"), canonical...)

	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.outboundPacket,
		nonCanonical,
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireInitialRefundState(t)
}

func TestIBCModuleAcknowledgementRejectsMalformedPacketDataWithoutRefundMutation(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	packet := scenario.outboundPacket
	packet.Data = []byte("invalid packet data")
	ack := channeltypes.NewErrorAcknowledgement(errors.New("target receiver blocked"))

	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		packet,
		types.ModuleCdc.MustMarshalJSON(&ack),
		sdk.AccAddress{},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot unmarshal ICS20-V1 transfer packet data")

	scenario.requireInitialRefundState(t)
}

func TestIBCModuleTimeoutRejectsMalformedPacketDataWithoutRefundMutation(t *testing.T) {
	scenario := setupModuleAckRefundScenario(t)
	packet := scenario.outboundPacket
	packet.Data = []byte("invalid packet data")

	err := scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		packet,
		sdk.AccAddress{},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot unmarshal ICS20-V1 transfer packet data")

	scenario.requireInitialRefundState(t)
}

func TestIBCModuleExchangePacketSuccessAckDoesNotRefundEscrow(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)
	ack := channeltypes.NewResultAcknowledgement([]byte{1})

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		types.ModuleCdc.MustMarshalJSON(&ack),
		sdk.AccAddress{},
	))

	scenario.requireEscrowLocked(t)
}

func TestIBCModuleExchangePacketErrorAckRefundsEscrow(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)
	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected exchange packet"))

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		types.ModuleCdc.MustMarshalJSON(&ack),
		sdk.AccAddress{},
	))

	scenario.requireEscrowRefunded(t)
}

func TestIBCModuleExchangePacketTimeoutRefundsEscrow(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)

	require.NoError(t, scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		sdk.AccAddress{},
	))

	scenario.requireEscrowRefunded(t)
}

func TestIBCModuleExchangePacketInvalidAckDoesNotMutateEscrow(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)
	ack := channeltypes.Acknowledgement{}

	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		types.ModuleCdc.MustMarshalJSON(&ack),
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireEscrowLocked(t)
}

func TestIBCModuleExchangePacketNonCanonicalAckDoesNotMutateEscrow(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)
	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected exchange packet"))
	canonical := types.ModuleCdc.MustMarshalJSON(&ack)
	nonCanonical := append([]byte(" \n\t"), canonical...)

	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		nonCanonical,
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireEscrowLocked(t)
}

func TestIBCModuleExchangePacketDuplicateErrorAckErrorsWithoutFurtherMutation(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)
	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected exchange packet"))
	ackBz := types.ModuleCdc.MustMarshalJSON(&ack)

	require.NoError(t, scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		ackBz,
		sdk.AccAddress{},
	))
	scenario.requireEscrowRefunded(t)

	err := scenario.im.OnAcknowledgementPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		ackBz,
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireEscrowRefunded(t)
}

func TestIBCModuleExchangePacketDuplicateTimeoutErrorsWithoutFurtherMutation(t *testing.T) {
	scenario := setupModuleExchangePacketRefundScenario(t)

	require.NoError(t, scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		sdk.AccAddress{},
	))
	scenario.requireEscrowRefunded(t)

	err := scenario.im.OnTimeoutPacket(
		scenario.ctx,
		types.V1,
		scenario.packet,
		sdk.AccAddress{},
	)
	require.Error(t, err)

	scenario.requireEscrowRefunded(t)
}

type moduleAckRefundScenario struct {
	k              keeperpkg.Keeper
	ctx            sdk.Context
	bank           *moduleAckRefundBankKeeper
	bex            *moduleAckRefundBexKeeper
	ics4           *moduleAckRefundICS4Wrapper
	im             *IBCModule
	reserve        sdk.AccAddress
	originalSender sdk.AccAddress
	targetReceiver sdk.AccAddress
	inputDenom     *transwapv1.Denom
	outputDenom    *transwapv1.Denom
	inputIBCDenom  string
	outputIBCDenom string
	originalKey    string
	outboundPacket channeltypes.Packet
}

type moduleExchangePacketRefundScenario struct {
	k            keeperpkg.Keeper
	ctx          sdk.Context
	bank         *moduleAckRefundBankKeeper
	bex          *moduleAckRefundBexKeeper
	ics4         *moduleAckRefundICS4Wrapper
	im           *IBCModule
	sender       sdk.AccAddress
	escrow       sdk.AccAddress
	denom        string
	amount       sdkmath.Int
	packet       channeltypes.Packet
	totalEscrow  sdk.Coin
	escrowLocked sdk.Coins
}

func setupModuleAckRefundScenario(t *testing.T) moduleAckRefundScenario {
	t.Helper()

	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	im := NewIBCModule(k)

	reserve := bex.reserve
	originalSender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	targetReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	inputDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	outputDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	inputIBCDenom := types.DenomIBCDenom(inputDenom)
	outputIBCDenom := types.DenomIBCDenom(outputDenom)

	bank.SetBalance(reserve, sdk.NewCoins(
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(100)),
		sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(900)),
	))
	bank.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))))

	refundPacket := types.NewTransferPacketData(
		types.PortID,
		"channel-0",
		&transwapv1.Token{Denom: inputDenom, Amount: "103"},
		reserve.String(),
		originalSender.String(),
		"refund coins through Guru station due to failure on the target chain",
		uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
		sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3)),
		"7",
	)
	originalKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-7", moduleAckRefundSequence)
	require.NoError(t, k.SetRefundPacketData(ctx, originalKey, refundPacket))

	outboundPacketData := types.NewFungibleTokenPacketData(
		types.DenomPath(outputDenom),
		"100",
		reserve.String(),
		targetReceiver.String(),
		"Station exchange",
	)
	outboundPacket := channeltypes.Packet{
		Sequence:           moduleAckRefundSequence,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-7",
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               types.FungibleTokenPacketDataBytes(outboundPacketData),
	}

	return moduleAckRefundScenario{
		k:              k,
		ctx:            ctx,
		bank:           bank,
		bex:            bex,
		ics4:           ics4,
		im:             im,
		reserve:        reserve,
		originalSender: originalSender,
		targetReceiver: targetReceiver,
		inputDenom:     inputDenom,
		outputDenom:    outputDenom,
		inputIBCDenom:  inputIBCDenom,
		outputIBCDenom: outputIBCDenom,
		originalKey:    originalKey,
		outboundPacket: outboundPacket,
	}
}

func setupModuleExchangePacketRefundScenario(t *testing.T) moduleExchangePacketRefundScenario {
	t.Helper()

	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	denom := "atgxusd"
	amount := sdkmath.NewInt(42)
	escrow := types.GetEscrowAddress(types.PortID, "channel-0")
	escrowLocked := sdk.NewCoins(sdk.NewCoin(denom, amount))
	bank.SetBalance(escrow, escrowLocked)
	totalEscrow := sdk.NewCoin(denom, amount)
	k.SetTotalEscrowForDenom(ctx, totalEscrow)

	packetData := transwapv1.FungibleTokenPacketData{
		ExchangeId: "7",
		Denom:      denom,
		Amount:     amount.String(),
		Sender:     sender.String(),
		Receiver:   receiver.String(),
		Memo:       "source exchange packet",
	}
	packet := channeltypes.Packet{
		Sequence:           moduleAckRefundSequence,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-0",
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               types.FungibleTokenPacketDataBytes(&packetData),
	}

	return moduleExchangePacketRefundScenario{
		k:            k,
		ctx:          ctx,
		bank:         bank,
		bex:          bex,
		ics4:         ics4,
		im:           im,
		sender:       sender,
		escrow:       escrow,
		denom:        denom,
		amount:       amount,
		packet:       packet,
		totalEscrow:  totalEscrow,
		escrowLocked: escrowLocked,
	}
}

func (s moduleAckRefundScenario) retryPacket(t *testing.T, index int) channeltypes.Packet {
	t.Helper()
	require.Greater(t, len(s.ics4.sent), index)
	sent := s.ics4.sent[index]
	return channeltypes.Packet{
		Sequence:           sent.sequence,
		SourcePort:         sent.sourcePort,
		SourceChannel:      sent.sourceChannel,
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               sent.data,
	}
}

func (s moduleAckRefundScenario) requirePostOriginalRefundState(t *testing.T) {
	t.Helper()

	reserveBalances := s.bank.GetAllBalances(s.ctx, s.reserve)
	require.Equal(t, sdkmath.NewInt(1000), reserveBalances.AmountOf(s.outputIBCDenom))
	require.True(t, reserveBalances.AmountOf(s.inputIBCDenom).IsZero())
	require.True(t, s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.Empty(t, s.bex.ledger("released"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(s.inputIBCDenom, sdkmath.NewInt(3))), s.bex.ledger("refunded"))
}

func (s moduleAckRefundScenario) requireInitialRefundState(t *testing.T) {
	t.Helper()

	reserveBalances := s.bank.GetAllBalances(s.ctx, s.reserve)
	require.Equal(t, sdkmath.NewInt(900), reserveBalances.AmountOf(s.outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(100), reserveBalances.AmountOf(s.inputIBCDenom))
	require.True(t, s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.Equal(t, sdkmath.NewInt(3), s.bank.GetAllBalances(s.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(s.inputIBCDenom))
	require.Empty(t, s.bex.ledger("released"))
	require.Empty(t, s.bex.ledger("refunded"))
	require.True(t, s.k.HasRefundPacketData(s.ctx, s.originalKey))
	require.Empty(t, s.ics4.sent)
}

func (s moduleExchangePacketRefundScenario) requireEscrowLocked(t *testing.T) {
	t.Helper()

	require.Equal(t, s.escrowLocked, s.bank.GetAllBalances(s.ctx, s.escrow))
	require.True(t, s.bank.GetAllBalances(s.ctx, s.sender).IsZero())
	require.Equal(t, s.totalEscrow, s.k.GetTotalEscrowForDenom(s.ctx, s.denom))
	require.Empty(t, s.bex.ledger("released"))
	require.Empty(t, s.bex.ledger("refunded"))
	require.Empty(t, s.ics4.sent)
}

func (s moduleExchangePacketRefundScenario) requireEscrowRefunded(t *testing.T) {
	t.Helper()

	require.True(t, s.bank.GetAllBalances(s.ctx, s.escrow).IsZero())
	require.Equal(t, s.amount, s.bank.GetAllBalances(s.ctx, s.sender).AmountOf(s.denom))
	require.True(t, s.k.GetTotalEscrowForDenom(s.ctx, s.denom).Amount.IsZero())
	require.Empty(t, s.bex.ledger("released"))
	require.Empty(t, s.bex.ledger("refunded"))
	require.Empty(t, s.ics4.sent)
}

func setupIBCModuleAckRefund(t *testing.T) (keeperpkg.Keeper, sdk.Context, *moduleAckRefundBankKeeper, *moduleAckRefundBexKeeper, *moduleAckRefundICS4Wrapper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "transwap-ack-refund"}, false, log.NewNopLogger()).
		WithBlockTime(time.Unix(1_700_000_000, 0)).
		WithEventManager(sdk.NewEventManager())

	bank := &moduleAckRefundBankKeeper{balances: make(map[string]sdk.Coins)}
	bex := &moduleAckRefundBexKeeper{
		reserve: sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20)),
		bank:    bank,
		ledgers: make(map[string]sdk.Coins),
	}
	ics4 := &moduleAckRefundICS4Wrapper{sequence: 88}

	k := keeperpkg.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		nil,
		ics4,
		moduleAckRefundChannelKeeper{channels: map[string]bool{"channel-0": true, "channel-7": true}},
		moduleAckRefundMsgRouter{},
		moduleAckRefundAccountKeeper{moduleAddr: authtypes.NewModuleAddress(types.ModuleName)},
		bank,
		bex,
		"authority",
	)

	return k, ctx, bank, bex, ics4
}

type moduleAckRefundAccountKeeper struct {
	moduleAddr sdk.AccAddress
}

func (m moduleAckRefundAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return m.moduleAddr
}

func (moduleAckRefundAccountKeeper) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}

type moduleAckRefundBankKeeper struct {
	balances map[string]sdk.Coins
}

func (m *moduleAckRefundBankKeeper) SetBalance(addr sdk.AccAddress, coins sdk.Coins) {
	m.balances[addr.String()] = coins.Sort()
}

func (m *moduleAckRefundBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}

func (m *moduleAckRefundBankKeeper) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	from := m.GetAllBalances(ctx, fromAddr)
	if !moduleAckRefundHasCoins(from, amt) {
		return errors.New("insufficient funds")
	}
	m.balances[fromAddr.String()] = from.Sub(amt...)
	m.balances[toAddr.String()] = m.GetAllBalances(ctx, toAddr).Add(amt...)
	return nil
}

func (m *moduleAckRefundBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	m.balances[moduleAddr.String()] = m.GetAllBalances(ctx, moduleAddr).Add(amt...)
	return nil
}

func (m *moduleAckRefundBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	current := m.GetAllBalances(ctx, moduleAddr)
	if !moduleAckRefundHasCoins(current, amt) {
		return errors.New("insufficient module funds")
	}
	m.balances[moduleAddr.String()] = current.Sub(amt...)
	return nil
}

func (m *moduleAckRefundBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return m.SendCoins(ctx, authtypes.NewModuleAddress(senderModule), recipientAddr, amt)
}

func (m *moduleAckRefundBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return m.SendCoins(ctx, senderAddr, authtypes.NewModuleAddress(recipientModule), amt)
}

func (*moduleAckRefundBankKeeper) BlockedAddr(sdk.AccAddress) bool { return false }

func (*moduleAckRefundBankKeeper) IsSendEnabledCoins(context.Context, ...sdk.Coin) error {
	return nil
}

func (*moduleAckRefundBankKeeper) HasDenomMetaData(context.Context, string) bool { return false }

func (*moduleAckRefundBankKeeper) SetDenomMetaData(context.Context, banktypes.Metadata) {}

func (m *moduleAckRefundBankKeeper) SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.GetAllBalances(ctx, addr).AmountOf(denom))
}

type moduleAckRefundBexKeeper struct {
	reserve sdk.AccAddress
	bank    *moduleAckRefundBankKeeper
	ledgers map[string]sdk.Coins
}

func (*moduleAckRefundBexKeeper) ValidateSwapInput(context.Context, uint64, string, string) (bexv1.SwapDirection, error) {
	return bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, nil
}

func (*moduleAckRefundBexKeeper) QuoteSwap(context.Context, *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	return nil, errors.New("unexpected quote")
}

func (m *moduleAckRefundBexKeeper) ReceiveToReserve(ctx context.Context, _ uint64, from sdk.AccAddress, amount sdk.Coins) error {
	return m.bank.SendCoins(ctx, from, m.reserve, amount)
}

func (m *moduleAckRefundBexKeeper) SendFromReserve(ctx context.Context, _ uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	return m.bank.SendCoins(ctx, m.reserve, recipient, amount)
}

func (*moduleAckRefundBexKeeper) RecordVolumeWindow(context.Context, uint64, bexv1.SwapDirection, sdkmath.Int) error {
	return nil
}

func (*moduleAckRefundBexKeeper) CollectFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (*moduleAckRefundBexKeeper) LockExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (m *moduleAckRefundBexKeeper) ReleaseExchangeFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("released", fee)
	return nil
}

func (m *moduleAckRefundBexKeeper) RefundLockedFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.bank.SendCoinsFromModuleToAccount(ctx, bextypes.ModuleName, m.reserve, sdk.NewCoins(fee)); err != nil {
		return err
	}
	m.addLedger("refunded", fee)
	return nil
}

func (m *moduleAckRefundBexKeeper) AddPendingLiability(_ context.Context, _ uint64, liability sdk.Coin) error {
	m.addLedger("pending", liability)
	return nil
}

func (m *moduleAckRefundBexKeeper) ReleasePendingLiability(_ context.Context, _ uint64, liability sdk.Coin) error {
	m.addLedger("liability_released", liability)
	return nil
}

func (m *moduleAckRefundBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return m.reserve
}

func (m *moduleAckRefundBexKeeper) addLedger(kind string, coin sdk.Coin) {
	m.ledgers[kind] = m.ledgers[kind].Add(coin)
}

func (m *moduleAckRefundBexKeeper) ledger(kind string) sdk.Coins {
	return m.ledgers[kind]
}

type moduleAckRefundChannelKeeper struct {
	channels map[string]bool
}

func (m moduleAckRefundChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	if portID != types.PortID || !m.channels[channelID] {
		return channeltypes.Channel{}, false
	}
	return channeltypes.Channel{State: channeltypes.OPEN}, true
}

func (moduleAckRefundChannelKeeper) GetNextSequenceSend(sdk.Context, string, string) (uint64, bool) {
	return 0, false
}

func (moduleAckRefundChannelKeeper) GetAllChannelsWithPortPrefix(sdk.Context, string) []channeltypes.IdentifiedChannel {
	return nil
}

func (m moduleAckRefundChannelKeeper) HasChannel(ctx sdk.Context, portID, channelID string) bool {
	_, found := m.GetChannel(ctx, portID, channelID)
	return found
}

type moduleAckRefundICS4Wrapper struct {
	sequence uint64
	sent     []moduleAckRefundSentPacket
}

func (m *moduleAckRefundICS4Wrapper) SendPacket(_ sdk.Context, sourcePort, sourceChannel string, _ clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	seq := m.sequence + uint64(len(m.sent)) //nolint:gosec // test sends are bounded.
	m.sent = append(m.sent, moduleAckRefundSentPacket{
		sequence:         seq,
		sourcePort:       sourcePort,
		sourceChannel:    sourceChannel,
		timeoutTimestamp: timeoutTimestamp,
		data:             append([]byte(nil), data...),
	})
	return seq, nil
}

func (*moduleAckRefundICS4Wrapper) WriteAcknowledgement(sdk.Context, ibcexported.PacketI, ibcexported.Acknowledgement) error {
	return nil
}

func (*moduleAckRefundICS4Wrapper) GetAppVersion(sdk.Context, string, string) (string, bool) {
	return types.V1, true
}

type moduleAckRefundSentPacket struct {
	sequence         uint64
	sourcePort       string
	sourceChannel    string
	timeoutTimestamp uint64
	data             []byte
}

type moduleAckRefundMsgRouter struct{}

func (moduleAckRefundMsgRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return nil
}

func moduleAckRefundHasCoins(balance sdk.Coins, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}

var _ porttypes.ICS4Wrapper = (*moduleAckRefundICS4Wrapper)(nil)
