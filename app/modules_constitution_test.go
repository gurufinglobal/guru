package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
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

func TestValidateChainGenesisRejectsInvalidConstitutionSeparationRatio(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	constitutionGenesis := &constitutionv1.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.SeparationRatio = &constitutionv1.SeparationRatio{
		BasePpm:       200_000,
		BurnPpm:       300_000,
		ValidatorsPpm: 400_000,
	}
	genesis[constitutiontypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(constitutionGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "separation_ratio total must be exactly")
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}

	return -1
}

func defaultGenesisWithConstitutionAddresses(t *testing.T, testApp *App) map[string]json.RawMessage {
	t.Helper()

	genesis := testApp.DefaultGenesis()
	constitutionGenesis := &constitutionv1.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = testGuruAddress(t, 0x21)
	constitutionGenesis.ModeratorAddress = testGuruAddress(t, 0x22)
	genesis[constitutiontypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(constitutionGenesis)

	return genesis
}

func testGuruAddress(t *testing.T, b byte) string {
	t.Helper()

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := accountCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
