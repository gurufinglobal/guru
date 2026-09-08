package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"

	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/ethereum/go-ethereum/common"
	ethparams "github.com/ethereum/go-ethereum/params"

	"github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

// GenesisState contains the module genesis documents consumed by InitChain.
type GenesisState map[string]json.RawMessage

// DefaultGenesis applies Guru's network policy over the upstream module
// defaults. ValidateGenesis enforces consensus-critical cross-module policy
// that upstream module validation cannot express.
func (app *App) DefaultGenesis() GenesisState {
	genesis := GenesisState(app.BasicModuleManager.DefaultGenesis(app.AppCodec()))

	bankGenesis := banktypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, banktypes.ModuleName, bankGenesis)
	bankGenesis.DenomMetadata = upsertNativeMetadata(bankGenesis.DenomMetadata)
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)

	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, stakingtypes.ModuleName, stakingGenesis)
	stakingGenesis.Params.BondDenom = config.BaseDenom
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	mintGenesis := minttypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, minttypes.ModuleName, mintGenesis)
	mintGenesis.Params.MintDenom = config.BaseDenom
	genesis[minttypes.ModuleName] = app.AppCodec().MustMarshalJSON(mintGenesis)

	govGenesis := govv1.DefaultGenesisState()
	app.unmarshalGenesis(genesis, govtypes.ModuleName, govGenesis)
	if govGenesis.Params == nil {
		params := govv1.DefaultParams()
		govGenesis.Params = &params
	}
	oneToken := mustInt("1000000000000000000")
	govGenesis.Params.MinDeposit = sdk.NewCoins(sdk.NewCoin(config.BaseDenom, oneToken))
	govGenesis.Params.ExpeditedMinDeposit = sdk.NewCoins(
		sdk.NewCoin(config.BaseDenom, oneToken.MulRaw(5)),
	)
	genesis[govtypes.ModuleName] = app.AppCodec().MustMarshalJSON(govGenesis)

	distributionGenesis := distrtypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, distrtypes.ModuleName, distributionGenesis)
	distributionGenesis.Params.CommunityTax = sdkmath.LegacyZeroDec()
	genesis[distrtypes.ModuleName] = app.AppCodec().MustMarshalJSON(distributionGenesis)

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, feemarkettypes.ModuleName, feeMarketGenesis)
	feeMarketGenesis.Params.NoBaseFee = true
	feeMarketGenesis.Params.BaseFee = sdkmath.LegacyZeroDec()
	feeMarketGenesis.Params.EnableHeight = 0
	feeMarketGenesis.Params.MinGasPrice = mustInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec()
	genesis[feemarkettypes.ModuleName] = app.AppCodec().MustMarshalJSON(feeMarketGenesis)

	evmGenesis := evmtypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, evmtypes.ModuleName, evmGenesis)
	evmGenesis.Params.EvmDenom = config.BaseDenom
	if evmGenesis.Params.ExtendedDenomOptions == nil {
		evmGenesis.Params.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{}
	}
	evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom = config.BaseDenom
	evmGenesis.Params.ActiveStaticPrecompiles = []string{}
	evmGenesis.Preinstalls = []evmtypes.Preinstall{mustHistoryStoragePreinstall()}
	genesis[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(evmGenesis)

	return genesis
}

// ConfigureConstitutionGenesis sets the operator-controlled addresses that the
// Constitution module intentionally leaves unset in its default genesis.
func (app *App) ConfigureConstitutionGenesis(
	genesis GenesisState,
	baseAddress string,
	moderatorAddress string,
) error {
	raw, ok := genesis[constitutiontypes.ModuleName]
	if !ok {
		return fmt.Errorf("constitution genesis is missing")
	}

	state := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode constitution genesis: %w", err)
	}
	state.BaseAddress = baseAddress
	state.ModeratorAddress = moderatorAddress
	genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(state)
	return nil
}

func mustHistoryStoragePreinstall() evmtypes.Preinstall {
	index := slices.IndexFunc(evmtypes.DefaultPreinstalls, func(preinstall evmtypes.Preinstall) bool {
		return common.HexToAddress(preinstall.Address) == ethparams.HistoryStorageAddress
	})
	if index < 0 {
		panic("Cosmos EVM default preinstalls do not contain EIP-2935 history storage")
	}
	return evmtypes.DefaultPreinstalls[index]
}

func (app *App) unmarshalGenesis(
	genesis GenesisState,
	moduleName string,
	target gogoproto.Message,
) {
	if raw, ok := genesis[moduleName]; ok {
		app.AppCodec().MustUnmarshalJSON(raw, target)
	}
}

// ValidateGenesis delegates structural validation to the wired modules and
// then enforces Guru's consensus-critical cross-module policies.
func (app *App) ValidateGenesis(genesis GenesisState) error {
	if err := app.BasicModuleManager.ValidateGenesis(
		app.AppCodec(),
		app.GetTxConfig(),
		genesis,
	); err != nil {
		return fmt.Errorf("validate module genesis: %w", err)
	}
	if err := app.validateGenesisValidatorSelfBonds(genesis); err != nil {
		return err
	}
	if err := app.validateFeeMarketGenesisPolicy(genesis); err != nil {
		return err
	}
	if err := app.validateEVMGenesisDenomPolicy(genesis); err != nil {
		return err
	}
	return nil
}

// ValidateGenesisAtHeight validates the application genesis and the
// state-transition constraints that depend on its initial block height.
func (app *App) ValidateGenesisAtHeight(genesis GenesisState, initialHeight int64) error {
	if err := app.ValidateGenesis(genesis); err != nil {
		return err
	}
	if err := app.validateZeroHeightRestartState(genesis, initialHeight); err != nil {
		return err
	}

	raw, ok := genesis[constitutiontypes.ModuleName]
	if !ok {
		return fmt.Errorf("constitution genesis is missing")
	}
	constitutionGenesis := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, constitutionGenesis); err != nil {
		return fmt.Errorf("decode constitution genesis: %w", err)
	}
	if constitutionGenesis.PendingMinGasPrice == nil {
		return nil
	}
	if err := app.ConstitutionKeeper.ValidateMinGasPriceScheduleAtHeight(
		constitutionGenesis.PendingMinGasPrice,
		initialHeight,
	); err != nil {
		return fmt.Errorf(
			"validate constitution pending min gas price at initial height %d: %w",
			initialHeight,
			err,
		)
	}

	return nil
}

func (app *App) validateGenesisValidatorSelfBonds(genesis GenesisState) error {
	stakingGenesis := stakingtypes.DefaultGenesisState()
	if raw, ok := genesis[stakingtypes.ModuleName]; ok {
		if err := app.AppCodec().UnmarshalJSON(raw, stakingGenesis); err != nil {
			return fmt.Errorf("decode staking genesis: %w", err)
		}
	}

	constitutionGenesis := new(constitutiontypes.GenesisState)
	if raw, ok := genesis[constitutiontypes.ModuleName]; ok {
		if err := app.AppCodec().UnmarshalJSON(raw, constitutionGenesis); err != nil {
			return fmt.Errorf("decode constitution genesis: %w", err)
		}
	}

	params := constitutionGenesis.GetParams()
	if params == nil {
		defaultGenesis, err := app.defaultConstitutionGenesis()
		if err != nil {
			return err
		}
		params = defaultGenesis.GetParams()
	}
	minBondCoin := params.GetMinValidatorBondAmount()
	if minBondCoin == nil {
		return fmt.Errorf("constitution min_validator_bond_amount cannot be nil")
	}
	if minBondCoin.Denom != config.BaseDenom {
		return fmt.Errorf(
			"constitution min_validator_bond_amount denom must be %q, got %q",
			config.BaseDenom,
			minBondCoin.Denom,
		)
	}
	if minBondCoin.Amount.IsNil() {
		return fmt.Errorf("constitution min_validator_bond_amount amount cannot be nil")
	}
	minBond := minBondCoin.Amount

	activeExportedValidators, err := app.activeExportedGenesisValidators(stakingGenesis)
	if err != nil {
		return err
	}
	for _, validator := range stakingGenesis.Validators {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
		if err != nil {
			return fmt.Errorf("invalid genesis validator address %s: %w", validator.GetOperator(), err)
		}
		if activeExportedValidators != nil {
			if _, ok := activeExportedValidators[string(validatorAddr)]; !ok {
				continue
			}
		}

		selfBond, err := app.genesisValidatorSelfBond(stakingGenesis.Delegations, validator, validatorAddr)
		if err != nil {
			return err
		}
		if selfBond.LT(minBond) {
			return fmt.Errorf(
				"validator %s genesis self-bond %s below constitution minimum %s",
				validator.GetOperator(),
				selfBond.String(),
				minBond.String(),
			)
		}
	}

	return nil
}

func (app *App) defaultConstitutionGenesis() (*constitutiontypes.GenesisState, error) {
	defaultGenesis := app.BasicModuleManager.DefaultGenesis(app.AppCodec())
	raw, ok := defaultGenesis[constitutiontypes.ModuleName]
	if !ok {
		return nil, fmt.Errorf("constitution default genesis missing")
	}

	constitutionGenesis := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, constitutionGenesis); err != nil {
		return nil, fmt.Errorf("decode constitution default genesis: %w", err)
	}

	return constitutionGenesis, nil
}

func (app *App) activeExportedGenesisValidators(stakingGenesis *stakingtypes.GenesisState) (map[string]struct{}, error) {
	if !stakingGenesis.GetExported() {
		return nil, nil
	}

	activeValidators := make(map[string]struct{}, len(stakingGenesis.GetLastValidatorPowers()))
	for _, lastValidatorPower := range stakingGenesis.GetLastValidatorPowers() {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(lastValidatorPower.Address)
		if err != nil {
			return nil, fmt.Errorf("invalid exported last validator address %s: %w", lastValidatorPower.Address, err)
		}
		activeValidators[string(validatorAddr)] = struct{}{}
	}

	return activeValidators, nil
}

func (app *App) genesisValidatorSelfBond(
	delegations []stakingtypes.Delegation,
	validator stakingtypes.Validator,
	validatorAddr []byte,
) (sdkmath.Int, error) {
	selfBond := sdkmath.ZeroInt()
	selfDelegationFound := false
	for _, delegation := range delegations {
		delegationValidatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(delegation.ValidatorAddress)
		if err != nil {
			return sdkmath.Int{}, fmt.Errorf("invalid genesis delegation validator address %s: %w", delegation.ValidatorAddress, err)
		}
		if !bytes.Equal(delegationValidatorAddr, validatorAddr) {
			continue
		}

		delegatorAddr, err := app.AccountKeeper.AddressCodec().StringToBytes(delegation.DelegatorAddress)
		if err != nil {
			return sdkmath.Int{}, fmt.Errorf("invalid genesis delegation delegator address %s: %w", delegation.DelegatorAddress, err)
		}
		if !bytes.Equal(delegatorAddr, validatorAddr) {
			continue
		}

		if selfDelegationFound {
			return sdkmath.Int{}, fmt.Errorf("duplicate genesis self-delegation for validator %s", validator.GetOperator())
		}
		selfDelegationFound = true
		selfBond = selfBond.Add(validator.TokensFromSharesTruncated(delegation.GetShares()).TruncateInt())
	}

	return selfBond, nil
}

func (app *App) validateFeeMarketGenesisPolicy(genesis GenesisState) error {
	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	if raw, ok := genesis[feemarkettypes.ModuleName]; ok {
		if err := app.AppCodec().UnmarshalJSON(raw, feeMarketGenesis); err != nil {
			return fmt.Errorf("decode feemarket genesis: %w", err)
		}
	}

	params := feeMarketGenesis.Params
	if !params.NoBaseFee {
		return fmt.Errorf("feemarket no_base_fee must be true")
	}
	if !params.BaseFee.IsZero() {
		return fmt.Errorf("feemarket base_fee must be zero, got %s", params.BaseFee.String())
	}
	if !params.MinGasPrice.IsPositive() {
		return fmt.Errorf("feemarket min_gas_price must be positive, got %s", params.MinGasPrice.String())
	}

	return nil
}

func (app *App) validateEVMGenesisDenomPolicy(genesis GenesisState) error {
	evmGenesis := evmtypes.DefaultGenesisState()
	if raw, ok := genesis[evmtypes.ModuleName]; ok {
		if err := app.AppCodec().UnmarshalJSON(raw, evmGenesis); err != nil {
			return fmt.Errorf("decode evm genesis: %w", err)
		}
	}

	if evmGenesis.Params.EvmDenom != config.BaseDenom {
		return fmt.Errorf(
			"evm evm_denom must be immutable config base denom %q, got %q",
			config.BaseDenom,
			evmGenesis.Params.EvmDenom,
		)
	}
	extendedDenomOptions := evmGenesis.Params.ExtendedDenomOptions
	if extendedDenomOptions == nil {
		return fmt.Errorf("evm extended_denom_options cannot be nil")
	}
	if extendedDenomOptions.ExtendedDenom != config.BaseDenom {
		return fmt.Errorf(
			"evm extended_denom must be immutable config base denom %q, got %q",
			config.BaseDenom,
			extendedDenomOptions.ExtendedDenom,
		)
	}

	return nil
}

func nativeMetadata() banktypes.Metadata {
	return banktypes.Metadata{
		Description: "The native staking and EVM gas token of the Guru chain",
		Base:        config.BaseDenom,
		Display:     config.DisplayDenom,
		Name:        "Guru",
		Symbol:      "GXN",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: config.BaseDenom, Exponent: 0},
			{Denom: config.DisplayDenom, Exponent: config.DenomExponent},
		},
	}
}

func upsertNativeMetadata(metadata []banktypes.Metadata) []banktypes.Metadata {
	replacement := nativeMetadata()
	for index := range metadata {
		if metadata[index].Base == config.BaseDenom {
			metadata[index] = replacement
			return metadata
		}
	}
	return append(metadata, replacement)
}

func mustInt(value string) sdkmath.Int {
	result, ok := sdkmath.NewIntFromString(value)
	if !ok {
		panic(fmt.Errorf("invalid integer constant %q", value))
	}
	return result
}

func (app *App) ValidateGenesisConsensusAtHeight(
	genesis GenesisState,
	initialHeight int64,
	consensusParams *cmtproto.ConsensusParams,
) error {
	// Module validation permits an empty schedule as an export/configuration
	// artifact. An active chain must never start with unscheduled enabled tasks,
	// including ordinary and height-preserving genesis imports.
	if consensusParams != nil && consensusParams.Abci != nil && consensusParams.Abci.VoteExtensionsEnableHeight > 0 {
		state := new(oracletypes.GenesisState)
		if err := app.AppCodec().UnmarshalJSON(genesis[oracletypes.ModuleName], state); err != nil {
			return fmt.Errorf("decode oracle genesis: %w", err)
		}
		if len(state.TaskSchedule) == 0 {
			for _, task := range state.Tasks {
				if task.GetEnabled() {
					return fmt.Errorf("initial Oracle schedule has 0 entries for enabled task %q; configure target Oracle before startup", task.GetSymbol())
				}
			}
		}
	}
	if initialHeight != zeroHeightEffectiveInitialHeight {
		return nil
	}
	stakingRaw, ok := genesis[stakingtypes.ModuleName]
	if !ok {
		return fmt.Errorf("staking genesis is missing")
	}
	stakingState := new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(stakingRaw, stakingState); err != nil {
		return fmt.Errorf("decode staking genesis: %w", err)
	}
	if !stakingState.Exported {
		return nil
	}
	if consensusParams == nil {
		return fmt.Errorf("consensus params are required to validate the initial Oracle schedule")
	}
	if consensusParams.Abci == nil {
		return fmt.Errorf("ABCI consensus params are required to validate the initial Oracle schedule")
	}
	voteExtensionsEnableHeight := consensusParams.Abci.VoteExtensionsEnableHeight
	raw, ok := genesis[oracletypes.ModuleName]
	if !ok {
		return fmt.Errorf("oracle genesis is missing")
	}
	state := new(oracletypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode oracle genesis: %w", err)
	}
	if voteExtensionsEnableHeight == 0 {
		if len(state.TaskSchedule) != 0 {
			return fmt.Errorf("disabled target Oracle must have an empty initial schedule")
		}
		return nil
	}
	expected, err := buildInitialOracleSchedule(state.Tasks, voteExtensionsEnableHeight)
	if err != nil {
		return err
	}
	if len(state.TaskSchedule) != len(expected) {
		return fmt.Errorf(
			"initial Oracle schedule has %d entries; expected %d from vote_extensions_enable_height %d",
			len(state.TaskSchedule),
			len(expected),
			voteExtensionsEnableHeight,
		)
	}
	for i := range expected {
		actual := state.TaskSchedule[i]
		if actual == nil {
			return fmt.Errorf("initial Oracle schedule contains a nil entry at index %d", i)
		}
		if actual.Symbol != expected[i].Symbol || actual.Height != expected[i].Height {
			return fmt.Errorf(
				"initial Oracle schedule entry %d is %s@%d; expected %s@%d from vote_extensions_enable_height %d",
				i,
				actual.Symbol,
				actual.Height,
				expected[i].Symbol,
				expected[i].Height,
				voteExtensionsEnableHeight,
			)
		}
	}
	return nil
}

// validateZeroHeightRestartState rejects an edited or hand-built exported
// genesis before InitGenesis can reintroduce source-height lifecycle state.
// Consensus-owned Oracle scheduling is checked separately with the CometBFT
// consensus parameters in ValidateGenesisConsensusAtHeight.
func (app *App) validateZeroHeightRestartState(
	genesis GenesisState,
	initialHeight int64,
) error {
	if initialHeight != zeroHeightEffectiveInitialHeight {
		return nil
	}

	stakingState := new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(genesis[stakingtypes.ModuleName], stakingState); err != nil {
		return fmt.Errorf("decode staking genesis for zero-height restart: %w", err)
	}
	if !stakingState.Exported {
		return nil
	}
	if err := validateZeroHeightStakingTarget(stakingState); err != nil {
		return fmt.Errorf("validate zero-height restart staking state: %w", err)
	}
	distributionState := new(distrtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(
		genesis[distrtypes.ModuleName],
		distributionState,
	); err != nil {
		return fmt.Errorf("decode distribution genesis for zero-height restart: %w", err)
	}
	if err := validateZeroHeightDistributionTarget(distributionState, stakingState); err != nil {
		return fmt.Errorf("validate zero-height restart distribution state: %w", err)
	}

	oracleState := new(oracletypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(genesis[oracletypes.ModuleName], oracleState); err != nil {
		return fmt.Errorf("decode oracle genesis for zero-height restart: %w", err)
	}
	if err := validateZeroHeightOracleTarget(oracleState); err != nil {
		return fmt.Errorf("validate zero-height restart Oracle state: %w", err)
	}

	constitutionState := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(
		genesis[constitutiontypes.ModuleName],
		constitutionState,
	); err != nil {
		return fmt.Errorf("decode constitution genesis for zero-height restart: %w", err)
	}
	if err := validateZeroHeightConstitutionTarget(constitutionState); err != nil {
		return fmt.Errorf("validate zero-height restart Constitution state: %w", err)
	}
	if err := app.validateEVMHistoryContract(genesis, true); err != nil {
		return fmt.Errorf("validate zero-height restart EIP-2935 state: %w", err)
	}

	slashingState := new(slashingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(genesis[slashingtypes.ModuleName], slashingState); err != nil {
		return fmt.Errorf("decode slashing genesis for zero-height restart: %w", err)
	}
	if err := app.validateZeroHeightSlashingTarget(slashingState, stakingState); err != nil {
		return fmt.Errorf("validate zero-height restart slashing state: %w", err)
	}
	if err := app.validateExportInvariants(genesis); err != nil {
		return fmt.Errorf("validate zero-height restart export invariants: %w", err)
	}

	return nil
}

// buildInitialOracleSchedule defines the target launch schedule, never the export transform.
func buildInitialOracleSchedule(
	tasks []*oracletypes.OracleTask,
	voteExtensionsEnableHeight int64,
) ([]*oracletypes.OracleTaskScheduleEntry, error) {
	if voteExtensionsEnableHeight < 0 {
		return nil, fmt.Errorf(
			"vote extensions enable height cannot be negative: %d",
			voteExtensionsEnableHeight,
		)
	}
	baseHeight := zeroHeightEffectiveInitialHeight
	if voteExtensionsEnableHeight > baseHeight {
		baseHeight = voteExtensionsEnableHeight
	}
	schedule := make([]*oracletypes.OracleTaskScheduleEntry, 0, 2*len(tasks))
	for _, task := range tasks {
		if task == nil {
			return nil, fmt.Errorf("oracle genesis contains a nil task")
		}
		if !task.Enabled {
			continue
		}
		interval := int64(task.GetSubmissionInterval())
		if interval == 0 {
			return nil, fmt.Errorf(
				"enabled oracle task %q has zero submission_interval",
				task.Symbol,
			)
		}
		if baseHeight > math.MaxInt64-(2*interval) {
			return nil, fmt.Errorf("oracle task %q target schedule overflows int64", task.Symbol)
		}
		symbol := oraclekeeper.NormalizeSymbol(task.Symbol)
		schedule = append(
			schedule,
			&oracletypes.OracleTaskScheduleEntry{
				Symbol: symbol,
				Height: baseHeight + interval,
			},
			&oracletypes.OracleTaskScheduleEntry{
				Symbol: symbol,
				Height: baseHeight + 2*interval,
			},
		)
	}
	sort.Slice(schedule, func(i, j int) bool {
		if schedule[i].Symbol == schedule[j].Symbol {
			return schedule[i].Height < schedule[j].Height
		}
		return schedule[i].Symbol < schedule[j].Symbol
	})
	return schedule, nil
}
