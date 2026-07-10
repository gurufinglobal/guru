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
