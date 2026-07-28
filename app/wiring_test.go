package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestInitGenesisOrderIsUniqueAndRefundDependenciesAreOrdered(t *testing.T) {
	order := ModuleOrderInitGenesis()
	seen := make(map[string]struct{}, len(order))
	for _, moduleName := range order {
		_, duplicate := seen[moduleName]
		require.False(t, duplicate, "module %s appears more than once in init genesis order", moduleName)
		seen[moduleName] = struct{}{}
	}

	bankIndex := indexOf(order, banktypes.ModuleName)
	bexIndex := indexOf(order, bextypes.ModuleName)
	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	feePolicyIndex := indexOf(order, feepolicytypes.ModuleName)
	ibcIndex := indexOf(order, ibcexported.ModuleName)
	transwapIndex := indexOf(order, transwaptypes.ModuleName)
	for name, index := range map[string]int{
		banktypes.ModuleName:         bankIndex,
		bextypes.ModuleName:          bexIndex,
		constitutiontypes.ModuleName: constitutionIndex,
		feepolicytypes.ModuleName:    feePolicyIndex,
		ibcexported.ModuleName:       ibcIndex,
		transwaptypes.ModuleName:     transwapIndex,
	} {
		require.NotEqual(t, -1, index, "module %s must be initialized", name)
	}
	require.Less(t, bankIndex, bexIndex)
	require.Less(t, constitutionIndex, feePolicyIndex)
	require.Less(t, feePolicyIndex, bexIndex)
	require.Less(t, bexIndex, transwapIndex)
	require.Less(t, ibcIndex, transwapIndex)
}

func TestTransSwapRefundRetryEndBlockerIsScheduled(t *testing.T) {
	order := ModuleOrderEndBlockers()
	require.Contains(t, order, transwaptypes.ModuleName)
	require.Less(t, indexOf(order, banktypes.ModuleName), indexOf(order, transwaptypes.ModuleName))
}

func TestConstitutionModuleRunsAfterFeeMarketEndBlocker(t *testing.T) {
	order := ModuleOrderEndBlockers()

	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	feeMarketIndex := indexOf(order, feemarkettypes.ModuleName)
	require.NotEqual(t, -1, constitutionIndex, "constitution module must be in endblocker order")
	require.NotEqual(t, -1, feeMarketIndex, "feemarket module must be in endblocker order")
	require.Greater(t, constitutionIndex, feeMarketIndex, "constitution must run after feemarket in endblocker")
}

func TestConstitutionModuleIsInGenesisOrder(t *testing.T) {
	order := ModuleOrderInitGenesis()
	constitutionIndex := indexOf(order, constitutiontypes.ModuleName)
	feePolicyIndex := indexOf(order, feepolicytypes.ModuleName)
	stakingIndex := indexOf(order, stakingtypes.ModuleName)
	require.NotEqual(t, -1, constitutionIndex, "constitution module must be initialized in genesis")
	require.NotEqual(t, -1, feePolicyIndex, "feepolicy module must be initialized in genesis")
	require.NotEqual(t, -1, stakingIndex, "staking module must be initialized in genesis")
	require.Less(t, constitutionIndex, feePolicyIndex, "Constitution must initialize before feepolicy")
	require.Less(t, constitutionIndex, stakingIndex, "constitution must initialize before staking")
}

func TestValidateChainGenesisFeePolicyModeratorMirrorsConstitution(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	seedGenesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	constitutionGenesis := &constitutiontypes.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(seedGenesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionModeratorBytes, err := testApp.AccountKeeper.AddressCodec().StringToBytes(
		constitutionGenesis.GetModeratorAddress(),
	)
	require.NoError(t, err)
	matchingHexAlias := fmt.Sprintf("0x%X", constitutionModeratorBytes)
	require.NotEqual(t, constitutionGenesis.GetModeratorAddress(), matchingHexAlias)

	tests := []struct {
		name               string
		feePolicyModerator string
		expectedError      string
	}{
		{name: "empty legacy field inherits Constitution moderator"},
		{
			name:               "nonempty hex alias matches Constitution by decoded bytes",
			feePolicyModerator: matchingHexAlias,
		},
		{
			name:               "different decoded address is rejected",
			feePolicyModerator: fmt.Sprintf("0x%X", bytes.Repeat([]byte{0x23}, 20)),
			expectedError:      "does not match Constitution moderator",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
			feePolicyGenesis := &feepolicytypes.GenesisState{}
			testApp.AppCodec().MustUnmarshalJSON(genesis[feepolicytypes.ModuleName], feePolicyGenesis)
			feePolicyGenesis.ModeratorAddress = test.feePolicyModerator
			genesis[feepolicytypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(feePolicyGenesis)

			err := testApp.ValidateChainGenesis(genesis)
			if test.expectedError != "" {
				require.ErrorContains(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestFeePolicyStoreKeyWiring(t *testing.T) {
	configureFeePolicyTestBech32Prefixes(t, true)
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	feePolicyKey := testApp.GetKVStoreKey(feepolicytypes.StoreKey)
	require.NotNil(t, feePolicyKey)
	var occurrences int
	for _, key := range testApp.GetNonTransientKeys() {
		if key.Name() == feepolicytypes.StoreKey {
			occurrences++
			require.Same(t, feePolicyKey, key)
		}
	}
	require.Equal(t, 1, occurrences)
	require.Nil(t, testApp.GetTransientStoreKey(feepolicytypes.StoreKey))

	evmKey, ok := testApp.EVMKeeper.KVStoreKeys()[feepolicytypes.StoreKey]
	require.True(t, ok)
	require.Same(t, feePolicyKey, evmKey)
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
	require.Equal(t, constitutiontypes.MinGasPriceScaleFactor, feeMarketGenesis.Params.MinGasPrice.TruncateInt().String())
}

func TestValidateChainGenesisRejectsZeroFeeMarketMinGasPrice(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], feeMarketGenesis)
	feeMarketGenesis.Params.MinGasPrice = sdkmath.LegacyZeroDec()
	genesis[feemarkettypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(feeMarketGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "feemarket min_gas_price must be positive")
}

func TestValidateChainGenesisAllowsPendingMinGasPricePreviousMismatch(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	constitutionGenesis := &constitutiontypes.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.PendingMinGasPrice = &constitutiontypes.MinGasPriceSchedule{
		EffectiveHeight:                15,
		ScheduledMinGasPrice:           "1.1",
		SourceSymbol:                   appparams.MinGasPriceOracleSymbol,
		SourceValue:                    "1.0",
		SourceOracleHeight:             10,
		SourceSubmissionIntervalBlocks: 5,
		PendingDelayBlocks:             5,
		PendingDelayCapBlocks:          constitutiontypes.MinGasPricePendingDelayCap,
		RawMinGasPrice:                 constitutiontypes.MinGasPriceScaleFactor,
		PreviousMinGasPrice:            "1",
	}
	genesis[constitutiontypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(constitutionGenesis)

	require.NoError(t, testApp.ValidateChainGenesis(genesis))
}

func TestValidateChainGenesisRejectsInvalidConstitutionSeparationRatio(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	constitutionGenesis := &constitutiontypes.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.SeparationRatio = &constitutiontypes.SeparationRatio{
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

	constitutionGenesis := &constitutiontypes.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = "0x0000000000000000000000000000000000000001"
	genesis[constitutiontypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(constitutionGenesis)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "base_address cannot be a blocked address")
}

func TestValidateChainGenesisRejectsValidatorSelfBondBelowConstitutionMinimum(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	setWiringStakingValidator(t, testApp, genesis, "99")

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "genesis self-bond 99 below constitution minimum 100")
}

func TestValidateChainGenesisAllowsValidatorSelfBondAtConstitutionMinimum(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	setWiringStakingValidator(t, testApp, genesis, "100")

	require.NoError(t, testApp.ValidateChainGenesis(genesis))
}

func TestValidateChainGenesisUsesDefaultConstitutionParamsWhenOmitted(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	removeWiringConstitutionParams(t, genesis)

	require.NoError(t, testApp.ValidateChainGenesis(genesis))
}

func TestValidateChainGenesisAllowsExportedInactiveValidatorBelowConstitutionMinimum(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	setWiringStakingValidator(t, testApp, genesis, "99")
	setWiringStakingExported(t, testApp, genesis)

	require.NoError(t, testApp.ValidateChainGenesis(genesis))
}

func TestValidateChainGenesisRejectsExportedActiveValidatorBelowConstitutionMinimum(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	validatorAddress := setWiringStakingValidator(t, testApp, genesis, "99")
	setWiringStakingExported(t, testApp, genesis, validatorAddress)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "genesis self-bond 99 below constitution minimum 100")
}

func TestValidateChainGenesisRejectsDuplicateSelfDelegation(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	validatorAddress := setWiringStakingValidator(t, testApp, genesis, "120")
	setWiringStakingExported(t, testApp, genesis, validatorAddress)
	setWiringDuplicateSelfDelegations(t, testApp, genesis, 60, 60)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate genesis self-delegation")
}

func TestValidateChainGenesisRejectsInitialDuplicateSelfDelegation(t *testing.T) {
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.EmptyAppOptions{})
	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	setWiringMinValidatorBond(t, testApp, genesis, "100")
	setWiringStakingValidator(t, testApp, genesis, "120")
	setWiringDuplicateSelfDelegations(t, testApp, genesis, 60, 60)

	err := testApp.ValidateChainGenesis(genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate genesis self-delegation")
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
	var oracleGenesis oracletypes.GenesisState
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

	constitutionGenesis := &constitutiontypes.GenesisState{}
	app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = wiringAddress(t, 0x21)
	constitutionGenesis.ModeratorAddress = wiringAddress(t, 0x22)
	genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(constitutionGenesis)
}

func removeWiringConstitutionParams(t *testing.T, genesis map[string]json.RawMessage) {
	t.Helper()

	constitutionGenesisFields := make(map[string]json.RawMessage)
	require.NoError(t, json.Unmarshal(genesis[constitutiontypes.ModuleName], &constitutionGenesisFields))
	delete(constitutionGenesisFields, "params")
	bz, err := json.Marshal(constitutionGenesisFields)
	require.NoError(t, err)
	genesis[constitutiontypes.ModuleName] = bz
}

func setWiringMinValidatorBond(t *testing.T, app *App, genesis map[string]json.RawMessage, amount string) {
	t.Helper()

	constitutionGenesis := &constitutiontypes.GenesisState{}
	app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	minBond, ok := sdkmath.NewIntFromString(amount)
	require.True(t, ok)
	constitutionGenesis.Params.MinValidatorBondAmount = &sdk.Coin{
		Denom:  appparams.BaseDenom,
		Amount: minBond,
	}
	genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(constitutionGenesis)
}

func setWiringStakingValidator(t *testing.T, app *App, genesis map[string]json.RawMessage, selfBondAmount string) string {
	t.Helper()

	selfBond, ok := sdkmath.NewIntFromString(selfBondAmount)
	require.True(t, ok)

	pubKey := simtestutil.CreateTestPubKeys(1)[0]
	valAddr := sdk.ValAddress(pubKey.Address().Bytes())
	validatorAddress, err := app.StakingKeeper.ValidatorAddressCodec().BytesToString(valAddr)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(
		validatorAddress,
		pubKey,
		stakingtypes.Description{Moniker: "validator"},
	)
	require.NoError(t, err)
	validator.Tokens = selfBond
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(selfBond)

	delegatorAddress, err := app.AccountKeeper.AddressCodec().BytesToString(sdk.AccAddress(valAddr))
	require.NoError(t, err)

	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Validators = []stakingtypes.Validator{validator}
	stakingGenesis.Delegations = []stakingtypes.Delegation{
		stakingtypes.NewDelegation(
			delegatorAddress,
			validatorAddress,
			sdkmath.LegacyNewDecFromInt(selfBond),
		),
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
	return validatorAddress
}

func setWiringStakingExported(t *testing.T, app *App, genesis map[string]json.RawMessage, lastValidatorAddresses ...string) {
	t.Helper()

	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Exported = true
	stakingGenesis.LastValidatorPowers = make([]stakingtypes.LastValidatorPower, 0, len(lastValidatorAddresses))
	for _, validatorAddress := range lastValidatorAddresses {
		stakingGenesis.LastValidatorPowers = append(stakingGenesis.LastValidatorPowers, stakingtypes.LastValidatorPower{
			Address: validatorAddress,
			Power:   1,
		})
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
}

func setWiringDuplicateSelfDelegations(t *testing.T, app *App, genesis map[string]json.RawMessage, amounts ...int64) {
	t.Helper()

	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	require.Len(t, stakingGenesis.Delegations, 1)
	selfDelegation := stakingGenesis.Delegations[0]
	stakingGenesis.Delegations = make([]stakingtypes.Delegation, 0, len(amounts))
	for _, amount := range amounts {
		stakingGenesis.Delegations = append(
			stakingGenesis.Delegations,
			stakingtypes.NewDelegation(
				selfDelegation.DelegatorAddress,
				selfDelegation.ValidatorAddress,
				sdkmath.LegacyNewDec(amount),
			),
		)
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
}

func wiringAddress(t *testing.T, b byte) string {
	t.Helper()

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := accountCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}
