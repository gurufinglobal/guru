package transwap

import (
	"bytes"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestIBCModuleOnRecvTransferPacketScopesSwapProtectionMarkerToExchange(t *testing.T) {
	tests := []struct {
		name    string
		memo    string
		wantAck bool
	}{
		{name: "empty memo", memo: "", wantAck: true},
		{name: "plain memo", memo: "ordinary transfer memo", wantAck: true},
		{name: "other JSON memo", memo: `{"forward":{"receiver":"receiver"}}`, wantAck: true},
		{name: "malformed unrelated JSON remains opaque", memo: `{"forward":`, wantAck: true},
		{name: "valid protection marker conflicts with transfer", memo: `guru.transwap.protection:v1:{"min_amount_out":"1"}`},
		{name: "malformed protection marker conflicts with transfer", memo: `xguru.transwap.protection:v1:{"min_amount_out":"1"}`},
		{name: "deprecated protection namespace conflicts with transfer", memo: `{"transwap":{"min_amount_out":"1"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, ctx, bank, _, ics4 := setupIBCModuleAckRefund(t)
			ctx = ctx.WithEventManager(sdk.NewEventManager())
			im := NewIBCModule(k)

			sender := sdk.AccAddress(bytes.Repeat([]byte{0x61}, 20))
			receiver := sdk.AccAddress(bytes.Repeat([]byte{0x71}, 20))
			packetData := types.NewFungibleTokenPacketData("atgxusd", "42", sender.String(), receiver.String(), tt.memo)
			packet := channeltypes.Packet{
				Sequence:           40,
				SourcePort:         "xswap",
				SourceChannel:      "channel-1",
				DestinationPort:    types.PortID,
				DestinationChannel: "channel-0",
				Data:               types.FungibleTokenPacketDataBytes(packetData),
			}

			ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
			require.Equal(t, tt.wantAck, ack.Success())
			require.Empty(t, ics4.sent)

			voucherDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
			voucherIBCDenom := types.DenomIBCDenom(voucherDenom)
			if tt.wantAck {
				require.True(t, k.HasDenom(ctx, types.DenomHash(voucherDenom)))
				require.Equal(t, sdkmath.NewInt(42), bank.GetAllBalances(ctx, receiver).AmountOf(voucherIBCDenom))
				return
			}

			require.False(t, k.HasDenom(ctx, types.DenomHash(voucherDenom)))
			require.True(t, bank.GetAllBalances(ctx, receiver).IsZero())
		})
	}
}

func TestIBCModuleOnRecvTransferPacketUnescrowsReturningNativeCoin(t *testing.T) {
	tests := []struct {
		name       string
		fundEscrow bool
		wantAck    bool
	}{
		{name: "unescrow succeeds", fundEscrow: true, wantAck: true},
		{name: "insufficient escrow returns error acknowledgement", wantAck: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, ctx, bank, _, _ := setupIBCModuleAckRefund(t)
			im := NewIBCModule(k)

			sender := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
			receiver := sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20))
			amount := sdkmath.NewInt(42)
			nativeCoin := sdk.NewCoin("uatom", amount)
			escrow := types.GetEscrowAddress(types.PortID, "channel-0")
			k.SetTotalEscrowForDenom(ctx, nativeCoin)
			if tt.fundEscrow {
				bank.SetBalance(escrow, sdk.NewCoins(nativeCoin))
			}

			packetData := types.NewFungibleTokenPacketData(
				"transwap/channel-1/uatom",
				amount.String(),
				sender.String(),
				receiver.String(),
				"return to source chain",
			)
			packet := channeltypes.Packet{
				Sequence:           41,
				SourcePort:         types.PortID,
				SourceChannel:      "channel-1",
				DestinationPort:    types.PortID,
				DestinationChannel: "channel-0",
				Data:               types.FungibleTokenPacketDataBytes(packetData),
			}

			ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
			require.Equal(t, tt.wantAck, ack.Success())
			require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

			if tt.wantAck {
				require.True(t, bank.GetAllBalances(ctx, escrow).IsZero())
				require.Equal(t, amount, bank.GetAllBalances(ctx, receiver).AmountOf(nativeCoin.Denom))
				require.True(t, k.GetTotalEscrowForDenom(ctx, nativeCoin.Denom).Amount.IsZero())
				return
			}

			require.True(t, bank.GetAllBalances(ctx, receiver).IsZero())
			require.Equal(t, nativeCoin, k.GetTotalEscrowForDenom(ctx, nativeCoin.Denom))
		})
	}
}

func TestIBCModuleOnRecvTransferPacketValidatesResolvedLocalBankDenom(t *testing.T) {
	tests := []struct {
		name      string
		wireDenom string
		wantAck   bool
	}{
		{
			name:      "returning invalid native denom produces error ack",
			wireDenom: "xswap/channel-1/!",
		},
		{
			name:      "remote non SDK base remains interoperable as voucher",
			wireDenom: "!",
			wantAck:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, ctx, bank, _, _ := setupIBCModuleAckRefund(t)
			ctx = ctx.WithEventManager(sdk.NewEventManager())
			im := NewIBCModule(k)

			sender := sdk.AccAddress(bytes.Repeat([]byte{0x68}, 20))
			receiver := sdk.AccAddress(bytes.Repeat([]byte{0x78}, 20))
			packetData := types.NewFungibleTokenPacketData(
				tt.wireDenom,
				"42",
				sender.String(),
				receiver.String(),
				"denom materialization boundary",
			)
			packet := channeltypes.Packet{
				Sequence:           42,
				SourcePort:         "xswap",
				SourceChannel:      "channel-1",
				DestinationPort:    types.PortID,
				DestinationChannel: "channel-0",
				Data:               types.FungibleTokenPacketDataBytes(packetData),
			}

			ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
			require.Equal(t, tt.wantAck, ack.Success())
			require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())

			voucherDenom := types.NewDenom("!", types.NewHop(types.PortID, "channel-0"))
			voucherIBCDenom := types.DenomIBCDenom(voucherDenom)
			if tt.wantAck {
				require.True(t, k.HasDenom(ctx, types.DenomHash(voucherDenom)))
				require.Equal(t, sdkmath.NewInt(42), bank.GetAllBalances(ctx, receiver).AmountOf(voucherIBCDenom))
				return
			}

			require.False(t, k.HasDenom(ctx, types.DenomHash(voucherDenom)))
			require.True(t, bank.GetAllBalances(ctx, receiver).IsZero())
		})
	}
}
