package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	legacyproto "github.com/golang/protobuf/proto" //nolint:staticcheck // verifies the grpc-gateway v1 enum registry bridge.
	"github.com/stretchr/testify/require"
)

//nolint:staticcheck // verifies the grpc-gateway v1 enum registry bridge.
func TestLegacyGatewayRefundStatusEnumRegistration(t *testing.T) {
	values := legacyproto.EnumValueMap(refundStatusProtoName)
	require.NotNil(t, values)
	require.Equal(t, int32(RefundStatus_REFUND_STATUS_PENDING), values["REFUND_STATUS_PENDING"])
	require.Equal(t, int32(RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE), values["REFUND_STATUS_MANUAL_CLAIMABLE"])
}

func TestCoinCodecRoundTripsAndFailures(t *testing.T) {
	coin := sdk.NewCoin("uatom", sdkmath.NewInt(1234))
	protoCoin := SDKCoinToProto(coin)
	require.Equal(t, coin.Denom, protoCoin.Denom)
	require.Equal(t, sdkmath.NewInt(1234), protoCoin.Amount)

	decoded, err := ProtoCoinToSDK(protoCoin)
	require.NoError(t, err)
	require.Equal(t, coin, decoded)

	coins := SDKCoinsToProto(sdk.NewCoins(sdk.NewInt64Coin("ugxusdc", 10), sdk.NewInt64Coin("uatom", 3)))
	require.Len(t, coins, 2)
	require.Equal(t, "ugxusdc", coins[1].Denom)
	require.Equal(t, "uatom", coins[0].Denom)

	requiredCoins, err := ProtoCoinsToSDK(coins)
	require.NoError(t, err)
	require.Len(t, requiredCoins, 2)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uatom", 3), sdk.NewInt64Coin("ugxusdc", 10)), requiredCoins)

	_, err = ProtoCoinToSDK(sdk.Coin{Denom: "bad denom", Amount: sdkmath.OneInt()})
	require.Error(t, err)

	_, err = ProtoCoinToSDK(sdk.Coin{Denom: "uatom"})
	require.Error(t, err)

	_, err = ProtoCoinToSDK(sdk.Coin{Denom: "uatom", Amount: sdkmath.NewInt(-1)})
	require.Error(t, err)

	_, err = ProtoCoinsToSDK(sdk.Coins{
		sdk.NewInt64Coin("uatom", 1),
		sdk.NewInt64Coin("uatom", 2),
	})
	require.Error(t, err)
}
