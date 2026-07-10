package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestParseExchangeIDRequiresPositiveNumericID(t *testing.T) {
	for _, raw := range []string{"", "0", "abc", "-1"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseExchangeID(raw)
			require.Error(t, err)
		})
	}

	id, err := parseExchangeID("42")
	require.NoError(t, err)
	require.Equal(t, uint64(42), id)
}

func TestOutboundChannelFromTokenValidatesTraceRoute(t *testing.T) {
	tests := []struct {
		name  string
		token transwapv1.Token
	}{
		{"nil denom", transwapv1.Token{Amount: "1"}},
		{"native output", transwapv1.Token{Denom: types.NewDenom("ugxkrw"), Amount: "1"}},
		{"nil first hop", transwapv1.Token{Denom: types.NewDenom("ugxkrw", nil), Amount: "1"}},
		{"wrong port", transwapv1.Token{Denom: types.NewDenom("ugxkrw", types.NewHop("transfer", "channel-0")), Amount: "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := outboundChannelFromToken(tt.token)
			require.Error(t, err)
		})
	}

	channel, err := outboundChannelFromToken(transwapv1.Token{
		Denom:  types.NewDenom("ugxkrw", types.NewHop(types.PortID, "channel-7")),
		Amount: "1",
	})
	require.NoError(t, err)
	require.Equal(t, "channel-7", channel)
}

func TestLocalReceivedCoinUsesSourceSinkPathWithoutMutatingPacketDenom(t *testing.T) {
	sourceDenom := types.NewDenom("ugxusd")
	sourceData := types.NewInternalTransferRepresentation("7", transwapv1.Token{Denom: sourceDenom, Amount: "12"}, "sender", "receiver", "")

	sourceCoin, err := localReceivedCoin(sourceData, types.PortID, "channel-0", types.PortID, "channel-9")
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoin(types.DenomIBCDenom(types.NewDenom("ugxusd", types.NewHop(types.PortID, "channel-9"))), sdkmath.NewInt(12)), sourceCoin)
	require.Empty(t, sourceDenom.Trace)

	sinkDenom := types.NewDenom("ugxusd", types.NewHop(types.PortID, "channel-0"), types.NewHop("transfer", "channel-2"))
	sinkData := types.NewInternalTransferRepresentation("7", transwapv1.Token{Denom: sinkDenom, Amount: "5"}, "sender", "receiver", "")

	sinkCoin, err := localReceivedCoin(sinkData, types.PortID, "channel-0", types.PortID, "channel-9")
	require.NoError(t, err)
	expected := types.NewDenom("ugxusd", types.NewHop("transfer", "channel-2"))
	require.Equal(t, sdk.NewCoin(types.DenomIBCDenom(expected), sdkmath.NewInt(5)), sinkCoin)
	require.Equal(t, "channel-0", sinkDenom.Trace[0].ChannelId)
	require.Len(t, sinkDenom.Trace, 2)
}
