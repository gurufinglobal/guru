package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
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

func TestDefaultGenesisDisablesFeeMarketBaseFee(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := testApp.DefaultGenesis()

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], feeMarketGenesis)

	require.True(t, feeMarketGenesis.Params.NoBaseFee, "feemarket no_base_fee default must be true")
	require.True(t, feeMarketGenesis.Params.BaseFee.IsZero(), "feemarket base_fee default must be zero")
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

func TestValidateChainGenesisRejectsBlockedConstitutionBaseAddress(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)

	constitutionGenesis := &constitutionv1.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = "0x0000000000000000000000000000000000000001"
	genesis[constitutiontypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(constitutionGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "base_address cannot be a blocked address")
}

func TestValidateChainGenesisRejectsEnabledFeeMarketBaseFee(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], feeMarketGenesis)
	feeMarketGenesis.Params.NoBaseFee = false
	genesis[feemarkettypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(feeMarketGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "feemarket no_base_fee must be true")
}

func TestValidateChainGenesisRejectsNonZeroFeeMarketBaseFee(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], feeMarketGenesis)
	feeMarketGenesis.Params.BaseFee = sdkmath.LegacyNewDec(1)
	genesis[feemarkettypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(feeMarketGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "feemarket base_fee must be zero")
}

func TestOracleModuleWiringAndAppBoot(t *testing.T) {
	app := NewApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.EmptyAppOptions{}, baseapp.SetChainID(appparams.SDKChainID))

	require.Contains(t, app.ModuleManager.Modules, oracletypes.ModuleName)
	require.NotNil(t, app.OracleProposalHandler)

	genesis := app.BuildChainDefaultGenesis()
	var oracleGenesis oraclev1.GenesisState
	require.NoError(t, app.AppCodec().UnmarshalJSON(genesis[oracletypes.ModuleName], &oracleGenesis))
	require.Equal(t, uint32(1), oracleGenesis.GetParams().GetMinValidators())
	require.Equal(t, uint32(3), oracleGenesis.GetParams().GetMinSources())
	require.Equal(t, uint32(100), oracleGenesis.GetParams().GetHistoryLimit())

	setWiringConstitutionGenesisAddresses(t, app, genesis)
	require.NoError(t, app.ValidateChainGenesis(genesis))
}

func TestOracleProposalWiringUsesNoOpMempoolFallback(t *testing.T) {
	app := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.AppOptionsMap{server.FlagMempoolMaxTxs: -1},
		baseapp.SetChainID(appparams.SDKChainID),
	)

	require.NotNil(t, app.OracleProposalHandler)
	require.Nil(t, app.EVMMempool)
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
	setWiringConstitutionGenesisAddresses(t, testApp, genesis)
	return genesis
}

func setWiringConstitutionGenesisAddresses(t *testing.T, app *App, genesis map[string]json.RawMessage) {
	t.Helper()

	constitutionGenesis := &constitutionv1.GenesisState{}
	app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = wiringAddress(t, 0x21)
	constitutionGenesis.ModeratorAddress = wiringAddress(t, 0x22)
	genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(constitutionGenesis)
}

func wiringAddress(t *testing.T, b byte) string {
	t.Helper()

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := accountCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
