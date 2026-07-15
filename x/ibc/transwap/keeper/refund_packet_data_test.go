package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	tmdb "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"

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

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	transtypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

type refundBexKeeperMock struct{}

func (refundBexKeeperMock) ValidateSwapInput(context.Context, uint64, string, string) (bexv1.SwapDirection, error) {
	return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, nil
}

func (refundBexKeeperMock) QuoteSwap(context.Context, *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	return nil, nil
}

func (refundBexKeeperMock) ReceiveToReserve(context.Context, uint64, sdk.AccAddress, sdk.Coins) error {
	return nil
}

func (refundBexKeeperMock) SendFromReserve(context.Context, uint64, sdk.AccAddress, sdk.Coins) error {
	return nil
}

func (refundBexKeeperMock) RecordVolumeWindow(context.Context, uint64, bexv1.SwapDirection, sdkmath.Int) error {
	return nil
}

func (refundBexKeeperMock) CollectFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) LockExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) ReleaseExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) RefundLockedFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) AddPendingLiability(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) ReleasePendingLiability(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (refundBexKeeperMock) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return sdk.AccAddress("reserve")
}

func setupRefundKeeper(t *testing.T) (Keeper, sdk.Context) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(transtypes.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter()).
		WithBlockTime(time.Unix(1_700_000_000, 0))

	k := Keeper{
		storeService: runtime.NewKVStoreService(storeKey),
		cdc:          cdc,
		BexKeeper:    refundBexKeeperMock{},
	}

	return k, ctx
}

func TestRefundPacketDataKey(t *testing.T) {
	key := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 42)
	require.Equal(t, "refund/transwap/channel-7/42", key)
}

func TestRefundPacketData_IsolatedBySequenceForSameReceiver(t *testing.T) {
	k, ctx := setupRefundKeeper(t)

	receiver := "same-receiver"
	token := transwapv1.Token{Denom: transtypes.NewDenom("uatom"), Amount: "100"}

	packet1 := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-0",
		&token,
		"reserve-address-1",
		receiver,
		"memo-1",
		1,
		sdk.NewInt64Coin("uatom", 1),
		"1",
	)
	packet2 := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-0",
		&token,
		"reserve-address-2",
		receiver,
		"memo-2",
		2,
		sdk.NewInt64Coin("uatom", 2),
		"2",
	)

	key1 := GetRefundPacketDataKey(transtypes.PortID, "channel-0", 1)
	key2 := GetRefundPacketDataKey(transtypes.PortID, "channel-0", 2)

	require.NoError(t, k.SetRefundPacketData(ctx, key1, packet1))
	require.NoError(t, k.SetRefundPacketData(ctx, key2, packet2))

	got1, err := k.GetRefundPacketData(ctx, key1)
	require.NoError(t, err)
	require.Equal(t, packet1.ExchangeId, got1.ExchangeId)
	require.Equal(t, packet1.Sender, got1.Sender)
	require.Equal(t, packet1.Receiver, got1.Receiver)
	require.Equal(t, packet1.GetFee().GetDenom(), got1.GetFee().GetDenom())
	require.Equal(t, packet1.GetFee().GetAmount(), got1.GetFee().GetAmount())

	got2, err := k.GetRefundPacketData(ctx, key2)
	require.NoError(t, err)
	require.Equal(t, packet2.ExchangeId, got2.ExchangeId)
	require.Equal(t, packet2.Sender, got2.Sender)
	require.Equal(t, packet2.Receiver, got2.Receiver)
	require.Equal(t, packet2.GetFee().GetDenom(), got2.GetFee().GetDenom())
	require.Equal(t, packet2.GetFee().GetAmount(), got2.GetFee().GetAmount())

	// deleting one sequence entry must not affect the other sequence entry
	k.DeleteRefundPacketData(ctx, key1)

	_, err = k.GetRefundPacketData(ctx, key1)
	require.Error(t, err)

	stillPresent, err := k.GetRefundPacketData(ctx, key2)
	require.NoError(t, err)
	require.Equal(t, packet2.ExchangeId, stillPresent.ExchangeId)
}

func TestAcknowledgementSuccessReleasesExchangeRefundFee(t *testing.T) {
	k, ctx := setupRefundKeeper(t)
	bexKeeper := &refundAccountingBexKeeper{}
	k.BexKeeper = bexKeeper

	token := transwapv1.Token{Denom: transtypes.NewDenom("agxn"), Amount: "1000"}
	packet := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-7",
		&token,
		sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20)).String(),
		sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
		"refund",
		100,
		sdk.NewInt64Coin("agxn", 3),
		"7",
	)
	key := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 12)
	require.NoError(t, k.SetRefundPacketData(ctx, key, packet))

	ack := channeltypes.NewResultAcknowledgement([]byte{1})
	err := k.OnAcknowledgementTransferPacket(
		ctx,
		transtypes.PortID,
		"channel-7",
		12,
		transtypes.NewInternalTransferRepresentation("0", &token, packet.Sender, packet.Receiver, ""),
		ack,
	)
	require.NoError(t, err)
	require.Equal(t, []sdk.Coin{sdk.NewInt64Coin("agxn", 3)}, bexKeeper.released)
	require.Empty(t, bexKeeper.refunded)
	require.False(t, k.HasRefundPacketData(ctx, key))
}

func TestAcknowledgementSuccessIsIdempotentAfterMetadataDeleted(t *testing.T) {
	k, ctx := setupRefundKeeper(t)
	bexKeeper := &refundAccountingBexKeeper{}
	k.BexKeeper = bexKeeper

	token := transwapv1.Token{Denom: transtypes.NewDenom("agxn"), Amount: "1000"}
	packet := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-7",
		&token,
		sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20)).String(),
		sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
		"refund",
		100,
		sdk.NewInt64Coin("agxn", 3),
		"7",
	)
	key := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 12)
	require.NoError(t, k.SetRefundPacketData(ctx, key, packet))

	ack := channeltypes.NewResultAcknowledgement([]byte{1})
	for range 2 {
		err := k.OnAcknowledgementTransferPacket(
			ctx,
			transtypes.PortID,
			"channel-7",
			12,
			transtypes.NewInternalTransferRepresentation("0", &token, packet.Sender, packet.Receiver, ""),
			ack,
		)
		require.NoError(t, err)
	}

	require.Equal(t, []sdk.Coin{sdk.NewInt64Coin("agxn", 3)}, bexKeeper.released)
	require.Empty(t, bexKeeper.refunded)
	require.False(t, k.HasRefundPacketData(ctx, key))
}

func TestAcknowledgementErrorRefundsOutboundPacketAndCreatesRetryPacket(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	err := state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		ack,
	)
	require.NoError(t, err)

	requireExchangeRefundCallbackState(t, state)
}

func TestTimeoutRefundsOutboundPacketAndCreatesRetryPacket(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	err := state.k.OnTimeoutTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
	)
	require.NoError(t, err)

	requireExchangeRefundCallbackState(t, state)
}

func TestInvalidAcknowledgementDoesNotMutateRefundState(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	err := state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		channeltypes.Acknowledgement{},
	)
	require.Error(t, err)

	require.Empty(t, state.bexKeeper.released)
	require.Empty(t, state.bexKeeper.refunded)
	require.True(t, state.bankKeeper.GetAllBalances(state.ctx, state.reserve).IsZero())
	require.Equal(t, sdk.NewCoins(state.fee), state.bankKeeper.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), state.bankKeeper.GetAllBalances(state.ctx, transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), state.k.GetTotalEscrowForDenom(state.ctx, "agxn"))
	require.True(t, state.k.HasRefundPacketData(state.ctx, state.originalKey))
	require.Empty(t, state.ics4.sent)
}

func TestDuplicateOriginalErrorAckAfterRetryIsNoop(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	ack := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		ack,
	))
	requireExchangeRefundCallbackState(t, state)

	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		ack,
	))

	requireExchangeRefundCallbackState(t, state)
}

func TestDuplicateOriginalTimeoutAfterRetryIsNoop(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	require.NoError(t, state.k.OnTimeoutTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
	))
	requireExchangeRefundCallbackState(t, state)

	require.NoError(t, state.k.OnTimeoutTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
	))

	requireExchangeRefundCallbackState(t, state)
}

func TestAcknowledgementSuccessForRetryPacketClearsFeeFreeMetadata(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	errAck := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		errAck,
	))
	requireExchangeRefundCallbackState(t, state)

	retryData := retryInternalTransferDataFromSentPacket(t, state.ics4.sent[0])
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		88,
		retryData,
		successAck,
	))

	require.Empty(t, state.bexKeeper.released)
	require.Equal(t, []sdk.Coin{state.fee}, state.bexKeeper.refunded)
	require.Equal(t, sdk.NewCoins(state.fee), state.bankKeeper.GetAllBalances(state.ctx, state.reserve))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), state.bankKeeper.GetAllBalances(state.ctx, transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), state.k.GetTotalEscrowForDenom(state.ctx, "agxn"))
	require.False(t, state.k.HasRefundPacketData(state.ctx, GetRefundPacketDataKey(transtypes.PortID, "channel-7", 88)))
	require.Len(t, state.ics4.sent, 1)
}

func TestTimeoutForRetryPacketCreatesNextFeeFreeRetryWithoutDoubleFee(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	errAck := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		errAck,
	))
	requireExchangeRefundCallbackState(t, state)

	retryData := retryInternalTransferDataFromSentPacket(t, state.ics4.sent[0])
	state.ics4.sequence = 89
	require.NoError(t, state.k.OnTimeoutTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		88,
		retryData,
	))

	require.Empty(t, state.bexKeeper.released)
	require.Equal(t, []sdk.Coin{state.fee}, state.bexKeeper.refunded)
	require.Equal(t, sdk.NewCoins(state.fee), state.bankKeeper.GetAllBalances(state.ctx, state.reserve))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), state.bankKeeper.GetAllBalances(state.ctx, transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), state.k.GetTotalEscrowForDenom(state.ctx, "agxn"))
	require.Len(t, state.ics4.sent, 2)

	internal := retryInternalTransferDataFromSentPacket(t, state.ics4.sent[1])
	require.Equal(t, "1000", internal.Token.Amount)
	require.Equal(t, state.reserve.String(), internal.Sender)
	require.Equal(t, state.refundReceiver.String(), internal.Receiver)

	require.False(t, state.k.HasRefundPacketData(state.ctx, state.originalKey))
	require.False(t, state.k.HasRefundPacketData(state.ctx, GetRefundPacketDataKey(transtypes.PortID, "channel-7", 88)))
	nextRetry, err := state.k.GetRefundPacketData(state.ctx, GetRefundPacketDataKey(transtypes.PortID, "channel-7", 89))
	require.NoError(t, err)
	require.Nil(t, nextRetry.Fee)
	require.Equal(t, state.reserve.String(), nextRetry.Sender)
	require.Equal(t, state.refundReceiver.String(), nextRetry.Receiver)
	require.Equal(t, "7", nextRetry.ExchangeId)
}

func TestTimeoutAfterRetrySuccessAckIsNoop(t *testing.T) {
	state := setupExchangeRefundCallback(t)

	errAck := channeltypes.NewErrorAcknowledgement(errors.New("destination rejected packet"))
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		12,
		state.outboundData,
		errAck,
	))
	requireExchangeRefundCallbackState(t, state)

	retryData := retryInternalTransferDataFromSentPacket(t, state.ics4.sent[0])
	successAck := channeltypes.NewResultAcknowledgement([]byte{1})
	require.NoError(t, state.k.OnAcknowledgementTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		88,
		retryData,
		successAck,
	))
	require.False(t, state.k.HasRefundPacketData(state.ctx, GetRefundPacketDataKey(transtypes.PortID, "channel-7", 88)))

	require.NoError(t, state.k.OnTimeoutTransferPacket(
		state.ctx,
		transtypes.PortID,
		"channel-7",
		88,
		retryData,
	))

	require.Empty(t, state.bexKeeper.released)
	require.Equal(t, []sdk.Coin{state.fee}, state.bexKeeper.refunded)
	require.Equal(t, sdk.NewCoins(state.fee), state.bankKeeper.GetAllBalances(state.ctx, state.reserve))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), state.bankKeeper.GetAllBalances(state.ctx, transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), state.k.GetTotalEscrowForDenom(state.ctx, "agxn"))
	require.Len(t, state.ics4.sent, 1)
}

func TestPerformExchangeRefundReturnsFeeAndCreatesRetryPacket(t *testing.T) {
	k, ctx := setupRefundKeeper(t)
	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	fee := sdk.NewInt64Coin("agxn", 3)
	refundToken := transwapv1.Token{Denom: transtypes.NewDenom("agxn"), Amount: "1000"}

	bankKeeper := newRefundAccountingBankKeeper()
	bankKeeper.SetBalance(reserve, sdk.NewCoins(sdk.NewInt64Coin("agxn", 997)))
	bankKeeper.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(fee))
	ics4 := &refundAccountingICS4Wrapper{sequence: 88}
	bexKeeper := &refundAccountingBexKeeper{reserve: reserve, bankKeeper: bankKeeper}

	k.BankKeeper = bankKeeper
	k.BexKeeper = bexKeeper
	k.AuthKeeper = refundAccountingAccountKeeper{moduleAddr: authtypes.NewModuleAddress(transtypes.ModuleName)}
	k.channelKeeper = refundAccountingChannelKeeper{portID: transtypes.PortID, channelID: "channel-7"}
	k.ics4Wrapper = ics4

	packet := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-7",
		&refundToken,
		reserve.String(),
		receiver.String(),
		"refund coins through Guru station due to failure on the target chain",
		123456789,
		fee,
		"7",
	)
	originalKey := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 12)
	require.NoError(t, k.SetRefundPacketData(ctx, originalKey, packet))

	require.NoError(t, k.performExchangeRefund(ctx, originalKey))

	require.Empty(t, bexKeeper.released)
	require.Equal(t, []sdk.Coin{fee}, bexKeeper.refunded)
	require.True(t, bankKeeper.GetAllBalances(ctx, reserve).IsZero())
	require.True(t, bankKeeper.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	escrow := transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), bankKeeper.GetAllBalances(ctx, escrow))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), k.GetTotalEscrowForDenom(ctx, "agxn"))

	require.Len(t, ics4.sent, 1)
	require.Equal(t, transtypes.PortID, ics4.sent[0].sourcePort)
	require.Equal(t, "channel-7", ics4.sent[0].sourceChannel)
	expectedRetryTimeout := uint64(ctx.BlockTime().Add(refundRetryTimeout).UnixNano()) //nolint:gosec // fixed test block time is positive.
	require.Equal(t, expectedRetryTimeout, ics4.sent[0].timeoutTimestamp)

	internal, err := transtypes.UnmarshalPacketData(ics4.sent[0].data, transtypes.V1, transtypes.EncodingJSON)
	require.NoError(t, err)
	require.True(t, internal.IsTransferPacket())
	require.Equal(t, "1000", internal.Token.Amount)
	require.Equal(t, reserve.String(), internal.Sender)
	require.Equal(t, receiver.String(), internal.Receiver)

	require.False(t, k.HasRefundPacketData(ctx, originalKey))
	retryKey := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 88)
	retry, err := k.GetRefundPacketData(ctx, retryKey)
	require.NoError(t, err)
	require.Nil(t, retry.Fee)
	require.Equal(t, packet.Sender, retry.Sender)
	require.Equal(t, packet.Receiver, retry.Receiver)
	require.Equal(t, packet.ExchangeId, retry.ExchangeId)
}

type exchangeRefundCallbackState struct {
	k              Keeper
	ctx            sdk.Context
	bankKeeper     *refundAccountingBankKeeper
	bexKeeper      *refundAccountingBexKeeper
	ics4           *refundAccountingICS4Wrapper
	originalKey    string
	reserve        sdk.AccAddress
	refundReceiver sdk.AccAddress
	fee            sdk.Coin
	outboundData   transtypes.InternalTransferRepresentation
}

func setupExchangeRefundCallback(t *testing.T) exchangeRefundCallbackState {
	t.Helper()

	k, ctx := setupRefundKeeper(t)
	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	destinationReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	refundReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	fee := sdk.NewInt64Coin("agxn", 3)
	token := transwapv1.Token{Denom: transtypes.NewDenom("agxn"), Amount: "1000"}

	bankKeeper := newRefundAccountingBankKeeper()
	escrow := transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")
	bankKeeper.SetBalance(escrow, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)))
	bankKeeper.SetBalance(authtypes.NewModuleAddress(bextypes.ModuleName), sdk.NewCoins(fee))
	ics4 := &refundAccountingICS4Wrapper{sequence: 88}
	bexKeeper := &refundAccountingBexKeeper{reserve: reserve, bankKeeper: bankKeeper}

	k.BankKeeper = bankKeeper
	k.BexKeeper = bexKeeper
	k.AuthKeeper = refundAccountingAccountKeeper{moduleAddr: authtypes.NewModuleAddress(transtypes.ModuleName)}
	k.channelKeeper = refundAccountingChannelKeeper{portID: transtypes.PortID, channelID: "channel-7"}
	k.ics4Wrapper = ics4
	k.SetTotalEscrowForDenom(ctx, sdk.NewInt64Coin("agxn", 1000))

	packet := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-7",
		&token,
		reserve.String(),
		refundReceiver.String(),
		"refund coins through Guru station due to failure on the target chain",
		123456789,
		fee,
		"7",
	)
	originalKey := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 12)
	require.NoError(t, k.SetRefundPacketData(ctx, originalKey, packet))

	return exchangeRefundCallbackState{
		k:              k,
		ctx:            ctx,
		bankKeeper:     bankKeeper,
		bexKeeper:      bexKeeper,
		ics4:           ics4,
		originalKey:    originalKey,
		reserve:        reserve,
		refundReceiver: refundReceiver,
		fee:            fee,
		outboundData:   transtypes.NewInternalTransferRepresentation("0", &token, reserve.String(), destinationReceiver.String(), ""),
	}
}

func requireExchangeRefundCallbackState(t *testing.T, state exchangeRefundCallbackState) {
	t.Helper()

	require.Empty(t, state.bexKeeper.released)
	require.Equal(t, []sdk.Coin{state.fee}, state.bexKeeper.refunded)
	require.Equal(t, sdk.NewCoins(state.fee), state.bankKeeper.GetAllBalances(state.ctx, state.reserve))
	require.True(t, state.bankKeeper.GetAllBalances(state.ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())

	escrow := transtypes.GetEscrowAddress(transtypes.PortID, "channel-7")
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("agxn", 1000)), state.bankKeeper.GetAllBalances(state.ctx, escrow))
	require.Equal(t, sdk.NewInt64Coin("agxn", 1000), state.k.GetTotalEscrowForDenom(state.ctx, "agxn"))

	require.Len(t, state.ics4.sent, 1)
	require.Equal(t, transtypes.PortID, state.ics4.sent[0].sourcePort)
	require.Equal(t, "channel-7", state.ics4.sent[0].sourceChannel)
	expectedRetryTimeout := uint64(state.ctx.BlockTime().Add(refundRetryTimeout).UnixNano()) //nolint:gosec // fixed test block time is positive.
	require.Equal(t, expectedRetryTimeout, state.ics4.sent[0].timeoutTimestamp)

	internal := retryInternalTransferDataFromSentPacket(t, state.ics4.sent[0])
	require.Equal(t, "1000", internal.Token.Amount)
	require.Equal(t, state.reserve.String(), internal.Sender)
	require.Equal(t, state.refundReceiver.String(), internal.Receiver)

	require.False(t, state.k.HasRefundPacketData(state.ctx, state.originalKey))
	retryKey := GetRefundPacketDataKey(transtypes.PortID, "channel-7", 88)
	retry, err := state.k.GetRefundPacketData(state.ctx, retryKey)
	require.NoError(t, err)
	require.Equal(t, uint64(123456789), retry.GetOriginalTimeoutTimestamp())
	require.Equal(t, expectedRetryTimeout, retry.GetTimeoutTimestamp())
	require.Equal(t, uint32(1), retry.GetRetryCount())
	require.Nil(t, retry.Fee)
	require.Equal(t, state.reserve.String(), retry.Sender)
	require.Equal(t, state.refundReceiver.String(), retry.Receiver)
	require.Equal(t, "7", retry.ExchangeId)
}

func retryInternalTransferDataFromSentPacket(t *testing.T, packet sentRefundPacket) transtypes.InternalTransferRepresentation {
	t.Helper()

	internal, err := transtypes.UnmarshalPacketData(packet.data, transtypes.V1, transtypes.EncodingJSON)
	require.NoError(t, err)
	require.True(t, internal.IsTransferPacket())
	return internal
}

type refundAccountingBexKeeper struct {
	reserve    sdk.AccAddress
	bankKeeper *refundAccountingBankKeeper
	released   []sdk.Coin
	refunded   []sdk.Coin
	pending    sdk.Coins
}

func (m *refundAccountingBexKeeper) ValidateSwapInput(context.Context, uint64, string, string) (bexv1.SwapDirection, error) {
	return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, nil
}

func (m *refundAccountingBexKeeper) QuoteSwap(context.Context, *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	return nil, nil
}

func (m *refundAccountingBexKeeper) ReceiveToReserve(ctx context.Context, _ uint64, from sdk.AccAddress, amount sdk.Coins) error {
	return m.bankKeeper.SendCoins(ctx, from, m.reserve, amount)
}

func (m *refundAccountingBexKeeper) SendFromReserve(ctx context.Context, _ uint64, recipient sdk.AccAddress, amount sdk.Coins) error {
	return m.bankKeeper.SendCoins(ctx, m.reserve, recipient, amount)
}

func (m *refundAccountingBexKeeper) RecordVolumeWindow(context.Context, uint64, bexv1.SwapDirection, sdkmath.Int) error {
	return nil
}

func (m *refundAccountingBexKeeper) CollectFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (m *refundAccountingBexKeeper) LockExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}

func (m *refundAccountingBexKeeper) ReleaseExchangeFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.released = append(m.released, fee)
	return nil
}

func (m *refundAccountingBexKeeper) RefundLockedFee(ctx context.Context, _ uint64, fee sdk.Coin) error {
	if err := m.bankKeeper.SendCoinsFromModuleToAccount(ctx, bextypes.ModuleName, m.reserve, sdk.NewCoins(fee)); err != nil {
		return err
	}
	m.refunded = append(m.refunded, fee)
	return nil
}

func (m *refundAccountingBexKeeper) AddPendingLiability(_ context.Context, _ uint64, liability sdk.Coin) error {
	m.pending = m.pending.Add(liability)
	return nil
}

func (m *refundAccountingBexKeeper) ReleasePendingLiability(_ context.Context, _ uint64, liability sdk.Coin) error {
	if m.pending.AmountOf(liability.Denom).GTE(liability.Amount) {
		m.pending = m.pending.Sub(liability)
	}
	return nil
}

func (m *refundAccountingBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return m.reserve
}

type refundAccountingBankKeeper struct {
	balances map[string]sdk.Coins
}

func newRefundAccountingBankKeeper() *refundAccountingBankKeeper {
	return &refundAccountingBankKeeper{balances: map[string]sdk.Coins{}}
}

func (m *refundAccountingBankKeeper) SetBalance(addr sdk.AccAddress, coins sdk.Coins) {
	m.balances[string(addr)] = coins.Sort()
}

func (m *refundAccountingBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[string(addr)]
}

func (m *refundAccountingBankKeeper) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if !refundHasCoins(m.GetAllBalances(ctx, fromAddr), amt) {
		return fmt.Errorf("insufficient funds for %s: need %s, have %s", fromAddr, amt, m.GetAllBalances(ctx, fromAddr))
	}
	m.balances[string(fromAddr)] = m.GetAllBalances(ctx, fromAddr).Sub(amt...)
	m.balances[string(toAddr)] = m.GetAllBalances(ctx, toAddr).Add(amt...)
	return nil
}

func (m *refundAccountingBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return m.SendCoins(ctx, authtypes.NewModuleAddress(senderModule), recipientAddr, amt)
}

func (m *refundAccountingBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return m.SendCoins(ctx, senderAddr, authtypes.NewModuleAddress(recipientModule), amt)
}

func (m *refundAccountingBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	m.balances[string(moduleAddr)] = m.GetAllBalances(ctx, moduleAddr).Add(amt...)
	return nil
}

func (m *refundAccountingBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	moduleAddr := authtypes.NewModuleAddress(moduleName)
	if !refundHasCoins(m.GetAllBalances(ctx, moduleAddr), amt) {
		return fmt.Errorf("insufficient module funds")
	}
	m.balances[string(moduleAddr)] = m.GetAllBalances(ctx, moduleAddr).Sub(amt...)
	return nil
}

func (m *refundAccountingBankKeeper) BlockedAddr(sdk.AccAddress) bool { return false }

func (m *refundAccountingBankKeeper) IsSendEnabledCoins(context.Context, ...sdk.Coin) error {
	return nil
}

func (m *refundAccountingBankKeeper) HasDenomMetaData(context.Context, string) bool {
	return false
}

func (m *refundAccountingBankKeeper) SetDenomMetaData(context.Context, banktypes.Metadata) {}

func (m *refundAccountingBankKeeper) SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.GetAllBalances(ctx, addr).AmountOf(denom))
}

func refundHasCoins(balance sdk.Coins, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}

type refundAccountingAccountKeeper struct {
	moduleAddr sdk.AccAddress
}

func (m refundAccountingAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return m.moduleAddr
}

func (refundAccountingAccountKeeper) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}

type refundAccountingChannelKeeper struct {
	portID    string
	channelID string
}

func (m refundAccountingChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	if portID != m.portID || channelID != m.channelID {
		return channeltypes.Channel{}, false
	}
	return channeltypes.Channel{State: channeltypes.OPEN}, true
}

func (refundAccountingChannelKeeper) GetNextSequenceSend(sdk.Context, string, string) (uint64, bool) {
	return 0, false
}

func (refundAccountingChannelKeeper) GetAllChannelsWithPortPrefix(sdk.Context, string) []channeltypes.IdentifiedChannel {
	return nil
}

func (m refundAccountingChannelKeeper) HasChannel(ctx sdk.Context, portID, channelID string) bool {
	_, found := m.GetChannel(ctx, portID, channelID)
	return found
}

type sentRefundPacket struct {
	sourcePort       string
	sourceChannel    string
	timeoutTimestamp uint64
	data             []byte
}

type refundAccountingICS4Wrapper struct {
	sequence uint64
	sent     []sentRefundPacket
}

func (m *refundAccountingICS4Wrapper) SendPacket(_ sdk.Context, sourcePort, sourceChannel string, _ clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	m.sent = append(m.sent, sentRefundPacket{
		sourcePort:       sourcePort,
		sourceChannel:    sourceChannel,
		timeoutTimestamp: timeoutTimestamp,
		data:             append([]byte(nil), data...),
	})
	return m.sequence, nil
}

func (*refundAccountingICS4Wrapper) WriteAcknowledgement(sdk.Context, ibcexported.PacketI, ibcexported.Acknowledgement) error {
	return nil
}

func (*refundAccountingICS4Wrapper) GetAppVersion(sdk.Context, string, string) (string, bool) {
	return transtypes.V1, true
}

var _ porttypes.ICS4Wrapper = (*refundAccountingICS4Wrapper)(nil)
