package transwap

import (
	"bytes"
	"context"
	"errors"
	stdmath "math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestValidateTransferChannelParamsRejectsUnsafeChannels(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	k.SetPort(ctx, types.PortID)

	tests := []struct {
		name      string
		order     channeltypes.Order
		portID    string
		channelID string
	}{
		{"ordered channel", channeltypes.ORDERED, types.PortID, "channel-0"},
		{"wrong port", channeltypes.UNORDERED, "transfer", "channel-0"},
		{"sequence above max uint32", channeltypes.UNORDERED, types.PortID, "channel-4294967296"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransferChannelParams(ctx, k, tt.order, tt.portID, tt.channelID)
			require.Error(t, err)
		})
	}

	require.NoError(t, ValidateTransferChannelParams(ctx, k, channeltypes.UNORDERED, types.PortID, "channel-0"))
	require.NoError(t, ValidateTransferChannelParams(ctx, k, channeltypes.UNORDERED, types.PortID, "channel-"+strconv.FormatUint(stdmath.MaxUint32, 10)))
}

func TestIBCModuleVersionNegotiationAndClosePolicy(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	k.SetPort(ctx, types.PortID)
	im := NewIBCModule(k)

	version, err := im.OnChanOpenInit(ctx, channeltypes.UNORDERED, nil, types.PortID, "channel-0", channeltypes.Counterparty{}, "")
	require.NoError(t, err)
	require.Equal(t, types.V1, version)

	_, err = im.OnChanOpenInit(ctx, channeltypes.UNORDERED, nil, types.PortID, "channel-0", channeltypes.Counterparty{}, "ics20-2")
	require.Error(t, err)

	version, err = im.OnChanOpenTry(ctx, channeltypes.UNORDERED, nil, types.PortID, "channel-0", channeltypes.Counterparty{}, "unsupported")
	require.NoError(t, err)
	require.Equal(t, types.V1, version)

	require.NoError(t, im.OnChanOpenAck(ctx, types.PortID, "channel-0", "", types.V1))
	require.Error(t, im.OnChanOpenAck(ctx, types.PortID, "channel-0", "", "unsupported"))
	require.Error(t, im.OnChanCloseInit(ctx, types.PortID, "channel-0"))
}

func TestIBCModuleChannelOpenEntrypointsRejectUnsafeParams(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	k.SetPort(ctx, types.PortID)
	im := NewIBCModule(k)

	tests := []struct {
		name      string
		order     channeltypes.Order
		portID    string
		channelID string
	}{
		{"ordered channel", channeltypes.ORDERED, types.PortID, "channel-0"},
		{"wrong port", channeltypes.UNORDERED, "transfer", "channel-0"},
		{"channel sequence above max uint32", channeltypes.UNORDERED, types.PortID, "channel-4294967296"},
	}

	for _, tt := range tests {
		t.Run("init "+tt.name, func(t *testing.T) {
			_, err := im.OnChanOpenInit(ctx, tt.order, nil, tt.portID, tt.channelID, channeltypes.Counterparty{}, types.V1)
			require.Error(t, err)
		})

		t.Run("try "+tt.name, func(t *testing.T) {
			_, err := im.OnChanOpenTry(ctx, tt.order, nil, tt.portID, tt.channelID, channeltypes.Counterparty{}, types.V1)
			require.Error(t, err)
		})
	}

	version, err := im.OnChanOpenTry(ctx, channeltypes.UNORDERED, nil, types.PortID, "channel-"+strconv.FormatUint(stdmath.MaxUint32, 10), channeltypes.Counterparty{}, types.V1)
	require.NoError(t, err)
	require.Equal(t, types.V1, version)
}

func TestIBCModuleUnmarshalPacketDataUsesAppVersion(t *testing.T) {
	im := NewIBCModule(keeper.Keeper{})
	im.SetICS4Wrapper(versionedICS4Wrapper{version: types.V1, found: true})
	packet := types.NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")

	data, version, err := im.UnmarshalPacketData(sdk.Context{}, types.PortID, "channel-0", types.FungibleTokenPacketDataBytes(packet))
	require.NoError(t, err)
	require.Equal(t, types.V1, version)
	require.IsType(t, types.InternalTransferRepresentation{}, data)

	im.SetICS4Wrapper(versionedICS4Wrapper{found: false})
	_, _, err = im.UnmarshalPacketData(sdk.Context{}, types.PortID, "channel-404", types.FungibleTokenPacketDataBytes(packet))
	require.Error(t, err)
}

func TestIBCModuleOnRecvPacketReturnsErrorAckForMalformedData(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	packet := channeltypes.Packet{
		Sequence:      1,
		SourcePort:    types.PortID,
		SourceChannel: "channel-0",
		Data:          []byte("{bad-json"),
	}
	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})

	require.False(t, ack.Success())
}

func TestIBCModuleOnRecvPacketRejectsMissingExchangeDiscriminator(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	packetData := types.NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	packetData.ExchangeId = ""
	packet := channeltypes.Packet{
		Sequence:      2,
		SourcePort:    types.PortID,
		SourceChannel: "channel-0",
		Data:          types.FungibleTokenPacketDataBytes(packetData),
	}
	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})

	require.False(t, ack.Success())
}

func TestIBCModuleOnRecvPacketReturnsErrorAckForBlockedReceiver(t *testing.T) {
	k, ctx := newIBCValidationKeeper(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	k.BankKeeper = ibcValidationBankKeeper{blockedAddr: receiver.String()}
	im := NewIBCModule(k)

	packetData := types.NewFungibleTokenPacketData("atgxkrw", "100", sender.String(), receiver.String(), "Station exchange")
	packet := channeltypes.Packet{
		Sequence:           2,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-7",
		DestinationPort:    "xswap",
		DestinationChannel: "channel-1",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
	}
	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})

	require.False(t, ack.Success())
	foundAckErr := false
	for _, event := range ctx.EventManager().Events() {
		for _, attr := range event.Attributes {
			if attr.Key != types.AttributeKeyAckError {
				continue
			}
			foundAckErr = true
			require.Contains(t, attr.Value, "is not allowed to receive funds")
		}
	}
	require.True(t, foundAckErr)
}

func TestIBCModuleOnAcknowledgementPacketRejectsNonCanonicalAck(t *testing.T) {
	im := NewIBCModule(keeper.Keeper{})
	packet := channeltypes.Packet{
		Sequence:           1,
		SourcePort:         types.PortID,
		SourceChannel:      "channel-0",
		DestinationChannel: "channel-1",
		DestinationPort:    types.PortID,
		Data: types.FungibleTokenPacketDataBytes(types.NewFungibleTokenPacketData(
			"ugxusd",
			"1",
			"cosmos1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
			"cosmos1z4e5l6e3h2qj4xw6h6f9c2f8y6d8m4e2x3s4e0",
			"",
		)),
	}
	ack := channeltypes.NewResultAcknowledgement([]byte{1})
	bz := types.ModuleCdc.MustMarshalJSON(&ack)
	canonical := append([]byte(" "), bz...)

	require.Error(t, im.OnAcknowledgementPacket(sdk.Context{}, types.V1, packet, canonical, sdk.AccAddress{}))
}

func TestIBCModuleOnTimeoutPacketRejectsMalformedData(t *testing.T) {
	im := NewIBCModule(keeper.Keeper{})
	packet := channeltypes.Packet{
		Sequence:      1,
		SourcePort:    types.PortID,
		SourceChannel: "channel-0",
		Data:          []byte("invalid"),
	}
	err := im.OnTimeoutPacket(sdk.Context{}, types.V1, packet, sdk.AccAddress{})
	require.Error(t, err)
}

func TestOnAcknowledgementPacketRejectsNonCanonicalAckBeforeKeeperUse(t *testing.T) {
	im := NewIBCModule(keeper.Keeper{})
	packetData := types.NewFungibleTokenPacketData("ugxusd", "1", "sender", "receiver", "")
	packet := channeltypes.Packet{
		Sequence:      1,
		SourcePort:    types.PortID,
		SourceChannel: "channel-0",
		Data:          types.FungibleTokenPacketDataBytes(packetData),
	}

	ack := channeltypes.NewResultAcknowledgement([]byte{1})
	canonical := types.ModuleCdc.MustMarshalJSON(&ack)
	nonCanonical := append([]byte(" \n\t"), canonical...)

	err := im.OnAcknowledgementPacket(sdk.Context{}, types.V1, packet, nonCanonical, nil)
	require.Error(t, err)
}

func TestV1ExchangeSourceTimeoutTimestamp(t *testing.T) {
	packet := channeltypes.Packet{TimeoutTimestamp: 987654321}
	require.Equal(t, uint64(987654321), v1ExchangeSourceTimeoutTimestamp(packet))
}

type ibcValidationAccountKeeper struct {
	moduleAddr sdk.AccAddress
}

func (m ibcValidationAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return m.moduleAddr
}

func (ibcValidationAccountKeeper) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}

type ibcValidationBankKeeper struct {
	blockedAddr string
}

func (ibcValidationBankKeeper) SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	return errors.New("unexpected bank send")
}
func (ibcValidationBankKeeper) MintCoins(context.Context, string, sdk.Coins) error { return nil }
func (ibcValidationBankKeeper) BurnCoins(context.Context, string, sdk.Coins) error { return nil }
func (ibcValidationBankKeeper) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (ibcValidationBankKeeper) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}
func (m ibcValidationBankKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return m.blockedAddr != "" && addr.String() == m.blockedAddr
}
func (ibcValidationBankKeeper) IsSendEnabledCoins(context.Context, ...sdk.Coin) error {
	return nil
}
func (ibcValidationBankKeeper) HasDenomMetaData(context.Context, string) bool        { return false }
func (ibcValidationBankKeeper) SetDenomMetaData(context.Context, banktypes.Metadata) {}
func (ibcValidationBankKeeper) SpendableCoin(context.Context, sdk.AccAddress, string) sdk.Coin {
	return sdk.Coin{}
}
func (ibcValidationBankKeeper) GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins {
	return nil
}

type ibcValidationBexKeeper struct{}

func (ibcValidationBexKeeper) ValidateSwapInput(context.Context, uint64, string, string) (bextypes.SwapDirection, error) {
	return bextypes.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, nil
}
func (ibcValidationBexKeeper) QuoteSwap(context.Context, *bextypes.QuoteSwapRequest) (*bextypes.QuoteSwapResponse, error) {
	return nil, nil
}
func (ibcValidationBexKeeper) ReceiveToReserve(context.Context, uint64, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (ibcValidationBexKeeper) SendSwapOutputFromReserve(context.Context, uint64, sdk.AccAddress, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) SendRefundFromReserve(context.Context, uint64, sdk.AccAddress, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) ClaimRefundFromReserve(context.Context, uint64, sdk.AccAddress, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) ReserveVolumeWindow(_ context.Context, exchangeID uint64, direction bextypes.SwapDirection, amount sdkmath.Int) (*bextypes.VolumeReservation, error) {
	return &bextypes.VolumeReservation{ExchangeId: exchangeID, Direction: direction, EpochSeconds: bextypes.MinVolumeEpochSeconds, Amount: amount.String(), VolumeWindowGeneration: 1}, nil
}
func (ibcValidationBexKeeper) ReleaseVolumeWindow(context.Context, *bextypes.VolumeReservation) error {
	return nil
}
func (ibcValidationBexKeeper) CollectFee(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) LockExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) ReleaseExchangeFee(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) RefundLockedFee(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) AddPendingLiability(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) ReleasePendingLiability(context.Context, uint64, sdk.Coin) error {
	return nil
}
func (ibcValidationBexKeeper) GetPendingLiabilities(context.Context, uint64) (sdk.Coins, error) {
	return nil, nil
}
func (ibcValidationBexKeeper) GetLockedFees(context.Context, uint64) (sdk.Coins, error) {
	return nil, nil
}
func (ibcValidationBexKeeper) GetRefundAccountingExchangeIDs(context.Context) ([]uint64, error) {
	return nil, nil
}
func (ibcValidationBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return sdk.AccAddress("reserve")
}

type ibcValidationChannelKeeper struct{}

func (ibcValidationChannelKeeper) GetChannel(sdk.Context, string, string) (channeltypes.Channel, bool) {
	return channeltypes.Channel{}, false
}
func (ibcValidationChannelKeeper) GetNextSequenceSend(sdk.Context, string, string) (uint64, bool) {
	return 0, false
}
func (ibcValidationChannelKeeper) GetAllChannelsWithPortPrefix(sdk.Context, string) []channeltypes.IdentifiedChannel {
	return nil
}
func (ibcValidationChannelKeeper) HasChannel(sdk.Context, string, string) bool { return false }
func (ibcValidationChannelKeeper) GetPacketCommitment(sdk.Context, string, string, uint64) []byte {
	return nil
}

type ibcValidationMsgRouter struct{}

func (ibcValidationMsgRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return nil }

type versionedICS4Wrapper struct {
	version string
	found   bool
}

func (versionedICS4Wrapper) SendPacket(sdk.Context, string, string, clienttypes.Height, uint64, []byte) (uint64, error) {
	return 0, errors.New("unexpected send packet")
}
func (versionedICS4Wrapper) WriteAcknowledgement(sdk.Context, ibcexported.PacketI, ibcexported.Acknowledgement) error {
	return errors.New("unexpected write acknowledgement")
}
func (v versionedICS4Wrapper) GetAppVersion(sdk.Context, string, string) (string, bool) {
	return v.version, v.found
}

var _ porttypes.ICS4Wrapper = versionedICS4Wrapper{}

func newIBCValidationKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "transwap-test"}, false, log.NewNopLogger())

	return keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		nil,
		nil,
		ibcValidationChannelKeeper{},
		ibcValidationMsgRouter{},
		ibcValidationAccountKeeper{moduleAddr: sdk.AccAddress("transwap-module")},
		ibcValidationBankKeeper{},
		ibcValidationBexKeeper{},
		"authority",
	), ctx
}
