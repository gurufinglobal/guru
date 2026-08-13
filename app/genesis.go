package app

import (
	"encoding/json"
	"fmt"
	"slices"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/ethereum/go-ethereum/common"
	ethparams "github.com/ethereum/go-ethereum/params"

	"github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
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
// then enforces Guru's Oracle-pegged FeeMarket policy.
func (app *App) ValidateGenesis(genesis GenesisState) error {
	if err := app.BasicModuleManager.ValidateGenesis(
		app.AppCodec(),
		app.GetTxConfig(),
		genesis,
	); err != nil {
		return fmt.Errorf("validate module genesis: %w", err)
	}
	if err := app.validateFeeMarketGenesisPolicy(genesis); err != nil {
		return err
	}
	return nil
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
