package staking

import (
	"testing"

	sdkstaking "github.com/cosmos/cosmos-sdk/x/staking"
	customkeeper "github.com/gurufinglobal/guru/v3/x/staking/keeper"
	"github.com/stretchr/testify/require"
)

func TestNewAppModuleKeepsCustomKeeper(t *testing.T) {
	keeper := &customkeeper.Keeper{}
	module := NewAppModule(sdkstaking.AppModule{}, keeper)

	require.Equal(t, keeper, module.keeper)
}
