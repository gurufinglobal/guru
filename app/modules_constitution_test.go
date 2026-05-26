package app

import (
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestConstitutionModuleHasNoEndBlockerOrderEntry(t *testing.T) {
	order := ModuleOrderEndBlockers()

	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	require.Equal(t, -1, constitutionIndex, "constitution module should not be in endblocker order")
}

func TestConstitutionModuleIsInGenesisOrder(t *testing.T) {
	order := ModuleOrderInitGenesis()
	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	stakingIndex := indexOf(order, stakingtypes.ModuleName)
	require.NotEqual(t, -1, constitutionIndex, "constitution module must be initialized in genesis")
	require.NotEqual(t, -1, stakingIndex, "staking module must be initialized in genesis")
	require.Less(t, constitutionIndex, stakingIndex, "constitution must initialize before staking")
}

func TestConstitutionModuleRunsBeforeDistributionBeginBlocker(t *testing.T) {
	order := ModuleOrderBeginBlockers()
	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	distributionIndex := indexOf(order, distrtypes.ModuleName)
	require.NotEqual(t, -1, constitutionIndex, "constitution module must be in beginblocker order")
	require.NotEqual(t, -1, distributionIndex, "distribution module must be in beginblocker order")
	require.Less(t, constitutionIndex, distributionIndex, "constitution must run before distribution in beginblocker")
}

func TestDefaultGenesisSetsDistributionCommunityTaxToZero(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := testApp.DefaultGenesis()

	distrGenesis := distrtypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[distrtypes.ModuleName], distrGenesis)

	require.True(t, distrGenesis.Params.CommunityTax.IsZero(), "distribution community_tax default must be zero")
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}

	return -1
}
