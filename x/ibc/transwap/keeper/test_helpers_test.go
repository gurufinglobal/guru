package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"

	transtypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

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

func (m *refundAccountingBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
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

func refundHasCoins(balance, needed sdk.Coins) bool {
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
	portID      string
	channelID   string
	commitments map[string][]byte
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

func (m refundAccountingChannelKeeper) GetPacketCommitment(_ sdk.Context, portID, channelID string, sequence uint64) []byte {
	commitment := m.commitments[fmt.Sprintf("%s/%s/%d", portID, channelID, sequence)]
	return append([]byte(nil), commitment...)
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
