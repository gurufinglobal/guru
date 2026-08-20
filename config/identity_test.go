package config

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestConfigureSDKSetsBIP44Path(t *testing.T) {
	sdkConfig := sdk.NewConfig()

	require.NoError(t, ConfigureSDK(sdkConfig))
	require.Equal(t, BIP44Purpose, sdkConfig.GetPurpose())
	require.Equal(t, BIP44CoinType, sdkConfig.GetCoinType())
	require.Equal(t, BIP44HDPath, sdkConfig.GetFullBIP44Path())
}
