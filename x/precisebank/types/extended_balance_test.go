package types_test

import (
	"testing"

	testconstants "github.com/gurufinglobal/guru/v2/testutil/constants"
	"github.com/gurufinglobal/guru/v2/x/precisebank/types"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestSumExtendedCoin(t *testing.T) {
	coinInfo := testconstants.ExampleChainCoinInfo[testconstants.SixDecimalsChainID]
	configurator := evmtypes.NewEVMConfigurator()
	err := configurator.
		WithEVMCoinInfo(coinInfo).
		Configure()
	require.NoError(t, err)

	tests := []struct {
		name string
		amt  sdk.Coins
		want sdk.Coin
	}{
		{
			"empty",
			sdk.NewCoins(),
			sdk.NewCoin(types.ExtendedCoinDenom(), sdkmath.ZeroInt()),
		},
		{
			"only integer",
			sdk.NewCoins(sdk.NewInt64Coin(types.IntegerCoinDenom(), 100)),
			sdk.NewCoin(types.ExtendedCoinDenom(), types.ConversionFactor().MulRaw(100)),
		},
		{
			"only extended",
			sdk.NewCoins(sdk.NewInt64Coin(types.ExtendedCoinDenom(), 100)),
			sdk.NewCoin(types.ExtendedCoinDenom(), sdkmath.NewInt(100)),
		},
		{
			"integer and extended",
			sdk.NewCoins(
				sdk.NewInt64Coin(types.IntegerCoinDenom(), 100),
				sdk.NewInt64Coin(types.ExtendedCoinDenom(), 100),
			),
			sdk.NewCoin(types.ExtendedCoinDenom(), types.ConversionFactor().MulRaw(100).AddRaw(100)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extVal := types.SumExtendedCoin(tt.amt)
			require.Equal(t, tt.want, extVal)
		})
	}
}
