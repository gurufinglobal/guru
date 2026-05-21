package app

import (
	"testing"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestConstitutionEndBlockerRunsBeforeStaking(t *testing.T) {
	order := ModuleOrderEndBlockers()

	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	stakingIndex := indexOf(order, stakingtypes.ModuleName)

	require.NotEqual(t, -1, constitutionIndex, "constitution module must be in endblocker order")
	require.NotEqual(t, -1, stakingIndex, "staking module must be in endblocker order")
	require.Less(t, constitutionIndex, stakingIndex, "constitution must run before staking")
}

func TestConstitutionModuleIsInGenesisOrder(t *testing.T) {
	order := ModuleOrderInitGenesis()
	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	require.NotEqual(t, -1, constitutionIndex, "constitution module must be initialized in genesis")
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}

	return -1
}
