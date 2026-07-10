package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCoinCodecRoundTripsAndFailures(t *testing.T) {
	coin := sdk.NewCoin("uatom", sdkmath.NewInt(1234))
	protoCoin := SDKCoinToProto(coin)
	require.Equal(t, coin.Denom, protoCoin.Denom)
	require.Equal(t, "1234", protoCoin.Amount)

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

	_, err = ProtoCoinToSDK(&basev1beta1.Coin{Denom: "bad denom", Amount: "1"})
	require.Error(t, err)

	_, err = ProtoCoinToSDK(&basev1beta1.Coin{Denom: "uatom", Amount: "bad"})
	require.Error(t, err)

	invalid := sdk.NewCoins(sdk.NewInt64Coin("uatom", 1))
	protoCoins := SDKCoinsToProto(invalid)
	protoCoins[0].Amount = ""
	_, err = ProtoCoinsToSDK(protoCoins)
	require.Error(t, err)

	_, err = ProtoCoinsToSDK([]*basev1beta1.Coin{nil})
	require.Error(t, err)
}
