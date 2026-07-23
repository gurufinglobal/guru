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
	connectiontypes "github.com/cosmos/ibc-go/v11/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	keeperpkg "github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func setupIBCModuleAckRefund(t *testing.T) (keeperpkg.Keeper, sdk.Context, *moduleAckRefundBankKeeper, *moduleAckRefundBexKeeper, *moduleAckRefundICS4Wrapper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "transwap-test"}, false, log.NewNopLogger()).
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
		moduleAckRefundChannelKeeper{
			channels: map[string]bool{"channel-0": true, "channel-7": true},
			ics4:     ics4,
		},
		moduleAckRefundMsgRouter{},
		moduleAckRefundAccountKeeper{moduleAddr: authtypes.NewModuleAddress(types.ModuleName)},
		bank,
		bex,
		"authority",
	)
	k.WithIBCClientKeepers(
		moduleAckRefundConnectionKeeper{},
		moduleAckRefundClientKeeper{timestamp: ctx.BlockTime()},
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

func (m *moduleAckRefundBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
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

func (*moduleAckRefundBexKeeper) ValidateSwapInput(context.Context, uint64, string, string) (bextypes.SwapDirection, error) {
	return bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B, nil
}

func (*moduleAckRefundBexKeeper) QuoteSwap(context.Context, *bextypes.QuoteSwapRequest) (*bextypes.QuoteSwapResponse, error) {
	return nil, errors.New("unexpected quote")
}

func (m *moduleAckRefundBexKeeper) ReceiveToReserve(ctx context.Context, _ uint64, from sdk.AccAddress, amount sdk.Coins) error {
	return m.bank.SendCoins(ctx, from, m.reserve, amount)
}

func (m *moduleAckRefundBexKeeper) SendSwapOutputFromReserve(ctx context.Context, _ uint64, recipient sdk.AccAddress, amount sdk.Coin) error {
	return m.bank.SendCoins(ctx, m.reserve, recipient, sdk.NewCoins(amount))
}

func (m *moduleAckRefundBexKeeper) SendRefundFromReserve(ctx context.Context, _ uint64, recipient sdk.AccAddress, amount sdk.Coin) error {
	if m.livePending().AmountOf(amount.Denom).LT(amount.Amount) {
		return errors.New("refund exceeds pending liability")
	}
	return m.bank.SendCoins(ctx, m.reserve, recipient, sdk.NewCoins(amount))
}

func (m *moduleAckRefundBexKeeper) ClaimRefundFromReserve(ctx context.Context, _ uint64, recipient sdk.AccAddress, amount sdk.Coin) error {
	if m.livePending().AmountOf(amount.Denom).LT(amount.Amount) {
		return errors.New("claim exceeds pending liability")
	}
	if err := m.bank.SendCoins(ctx, m.reserve, recipient, sdk.NewCoins(amount)); err != nil {
		return err
	}
	m.addLedger("liability_released", amount)
	return nil
}

func (*moduleAckRefundBexKeeper) ReserveVolumeWindow(_ context.Context, exchangeID uint64, direction bextypes.SwapDirection, amount sdkmath.Int) (*bextypes.VolumeReservation, error) {
	return &bextypes.VolumeReservation{ExchangeId: exchangeID, Direction: direction, EpochSeconds: bextypes.MinVolumeEpochSeconds, Amount: amount.String(), VolumeWindowGeneration: 1}, nil
}

func (*moduleAckRefundBexKeeper) ReleaseVolumeWindow(context.Context, *bextypes.VolumeReservation) error {
	return nil
}

func (*moduleAckRefundBexKeeper) CollectFee(context.Context, uint64, sdk.Coin) error { return nil }

func (m *moduleAckRefundBexKeeper) LockExchangeFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("locked", fee)
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
	if m.livePending().AmountOf(liability.Denom).LT(liability.Amount) {
		return errors.New("release exceeds pending liability")
	}
	m.addLedger("liability_released", liability)
	return nil
}

func (m *moduleAckRefundBexKeeper) GetPendingLiabilities(context.Context, uint64) (sdk.Coins, error) {
	return m.livePending(), nil
}

func (m *moduleAckRefundBexKeeper) GetLockedFees(context.Context, uint64) (sdk.Coins, error) {
	locked := m.ledger("locked")
	settled := m.ledger("released").Add(m.ledger("refunded")...)
	if !moduleAckRefundHasCoins(locked, settled) {
		return sdk.Coins{}, nil
	}
	return locked.Sub(settled...), nil
}

func (m *moduleAckRefundBexKeeper) GetRefundAccountingExchangeIDs(context.Context) ([]uint64, error) {
	if m.livePending().IsZero() && m.ledger("locked").IsZero() {
		return nil, nil
	}
	return []uint64{7}, nil
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

func (m *moduleAckRefundBexKeeper) livePending() sdk.Coins {
	pending := m.ledger("pending")
	released := m.ledger("liability_released")
	if !moduleAckRefundHasCoins(pending, released) {
		return sdk.Coins{}
	}
	return pending.Sub(released...)
}

type moduleAckRefundChannelKeeper struct {
	channels map[string]bool
	ics4     *moduleAckRefundICS4Wrapper
}

type moduleAckRefundConnectionKeeper struct{}

func (moduleAckRefundConnectionKeeper) GetConnection(_ sdk.Context, connectionID string) (connectiontypes.ConnectionEnd, bool) {
	if connectionID != "connection-0" {
		return connectiontypes.ConnectionEnd{}, false
	}
	return connectiontypes.ConnectionEnd{ClientId: "client-0"}, true
}

type moduleAckRefundClientKeeper struct {
	timestamp time.Time
}

func (m moduleAckRefundClientKeeper) GetLatestClientConsensusState(_ sdk.Context, clientID string) (ibcexported.ConsensusState, bool) {
	if clientID != "client-0" {
		return nil, false
	}
	return &ibctm.ConsensusState{Timestamp: m.timestamp}, true
}

func (m moduleAckRefundChannelKeeper) GetChannel(_ sdk.Context, portID, channelID string) (channeltypes.Channel, bool) {
	if portID != types.PortID || !m.channels[channelID] {
		return channeltypes.Channel{}, false
	}
	return channeltypes.Channel{State: channeltypes.OPEN, ConnectionHops: []string{"connection-0"}}, true
}

func (moduleAckRefundChannelKeeper) GetNextSequenceSend(sdk.Context, string, string) (uint64, bool) {
	return 0, false
}

func (m moduleAckRefundChannelKeeper) GetPacketCommitment(
	_ sdk.Context,
	portID, channelID string,
	sequence uint64,
) []byte {
	if m.ics4 == nil {
		return nil
	}
	return m.ics4.packetCommitment(portID, channelID, sequence)
}

func (moduleAckRefundChannelKeeper) GetAllChannelsWithPortPrefix(sdk.Context, string) []channeltypes.IdentifiedChannel {
	return nil
}

func (m moduleAckRefundChannelKeeper) HasChannel(ctx sdk.Context, portID, channelID string) bool {
	_, found := m.GetChannel(ctx, portID, channelID)
	return found
}

type moduleAckRefundICS4Wrapper struct {
	sequence    uint64
	sent        []moduleAckRefundSentPacket
	commitments map[string][]byte
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

func (m *moduleAckRefundICS4Wrapper) recordPacketCommitment(packet channeltypes.Packet) {
	if m.commitments == nil {
		m.commitments = make(map[string][]byte)
	}
	m.commitments[moduleAckRefundPacketKey(packet.SourcePort, packet.SourceChannel, packet.Sequence)] =
		channeltypes.CommitPacket(packet)
}

func (m *moduleAckRefundICS4Wrapper) packetCommitment(portID, channelID string, sequence uint64) []byte {
	if commitment := m.commitments[moduleAckRefundPacketKey(portID, channelID, sequence)]; len(commitment) != 0 {
		return append([]byte(nil), commitment...)
	}
	for _, packet := range m.sent {
		if packet.sequence != sequence || packet.sourcePort != portID || packet.sourceChannel != channelID {
			continue
		}
		commitment := channeltypes.CommitPacket(channeltypes.NewPacket(
			packet.data,
			packet.sequence,
			packet.sourcePort,
			packet.sourceChannel,
			"",
			"",
			clienttypes.ZeroHeight(),
			packet.timeoutTimestamp,
		))
		return commitment
	}
	return nil
}

func moduleAckRefundPacketKey(portID, channelID string, sequence uint64) string {
	return portID + "/" + channelID + "/" + sdkmath.NewIntFromUint64(sequence).String()
}

type moduleAckRefundSentPacket struct {
	sequence         uint64
	sourcePort       string
	sourceChannel    string
	timeoutTimestamp uint64
	data             []byte
}

type moduleAckRefundMsgRouter struct{}

func (moduleAckRefundMsgRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return nil }

func moduleAckRefundHasCoins(balance, needed sdk.Coins) bool {
	for _, coin := range needed {
		if balance.AmountOf(coin.Denom).LT(coin.Amount) {
			return false
		}
	}
	return true
}

var _ porttypes.ICS4Wrapper = (*moduleAckRefundICS4Wrapper)(nil)
