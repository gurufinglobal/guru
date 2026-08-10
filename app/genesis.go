package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/ethereum/go-ethereum/common"
	ethvm "github.com/ethereum/go-ethereum/core/vm"

	"github.com/gurufinglobal/guru/v2/config"
)

// GenesisState contains the module genesis documents consumed by InitChain.
type GenesisState map[string]json.RawMessage

// DefaultGenesis builds a fresh-chain genesis with Guru's consensus-critical
// denomination and EVM policy applied over every module default.
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
	govGenesis.Params.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewCoin(config.BaseDenom, oneToken.MulRaw(5)))
	genesis[govtypes.ModuleName] = app.AppCodec().MustMarshalJSON(govGenesis)

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, feemarkettypes.ModuleName, feeMarketGenesis)
	// Keep EIP-1559 enabled and retain all upstream v0.6.1 adjustment defaults.
	feeMarketGenesis.Params.NoBaseFee = false
	feeMarketGenesis.Params.BaseFee = feemarkettypes.DefaultBaseFee
	feeMarketGenesis.Params.EnableHeight = 0
	// One agxn is one atto-GXN. This is the smallest non-zero integer gas
	// price and prevents long runs of empty blocks from decaying fees to zero.
	feeMarketGenesis.Params.MinGasPrice = sdkmath.LegacyOneDec()
	genesis[feemarkettypes.ModuleName] = app.AppCodec().MustMarshalJSON(feeMarketGenesis)

	evmGenesis := evmtypes.DefaultGenesisState()
	app.unmarshalGenesis(genesis, evmtypes.ModuleName, evmGenesis)
	evmGenesis.Params.EvmDenom = config.BaseDenom
	if evmGenesis.Params.ExtendedDenomOptions == nil {
		evmGenesis.Params.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{}
	}
	evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom = config.BaseDenom
	// Stateful Cosmos precompiles are intentionally unavailable in Stage C.
	evmGenesis.Params.ActiveStaticPrecompiles = []string{}
	genesis[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(evmGenesis)

	return genesis
}

func (app *App) unmarshalGenesis(genesis GenesisState, moduleName string, target gogoproto.Message) {
	if raw, ok := genesis[moduleName]; ok {
		app.AppCodec().MustUnmarshalJSON(raw, target)
	}
}

// ValidateGenesis enforces both module schemas and Guru cross-module identity.
func (app *App) ValidateGenesis(genesis GenesisState) error {
	// FeeMarket v0.6.1 dereferences a nil BaseFee in its upstream validator.
	// Check the app policy first so malformed JSON is returned as an error.
	feeMarketGenesis := new(feemarkettypes.GenesisState)
	if err := app.decodeGenesis(genesis, feemarkettypes.ModuleName, feeMarketGenesis); err != nil {
		return err
	}
	if err := validateFeeMarketParameterPolicy(feeMarketGenesis.Params); err != nil {
		return fmt.Errorf("validate fee market params: %w", err)
	}
	// Gov v0.53.6 also dereferences nil params in its upstream validator.
	govRaw, ok := genesis[govtypes.ModuleName]
	if !ok {
		return fmt.Errorf("genesis is missing module %q", govtypes.ModuleName)
	}
	if err := validateGovGenesisJSONShape(govRaw); err != nil {
		return err
	}
	govGenesis := new(govv1.GenesisState)
	if err := app.decodeGenesis(genesis, govtypes.ModuleName, govGenesis); err != nil {
		return err
	}
	if govGenesis.Params == nil {
		return fmt.Errorf("governance params cannot be nil")
	}
	if err := validateGovParameterPolicy(*govGenesis.Params); err != nil {
		return fmt.Errorf("validate governance params: %w", err)
	}
	if len(govGenesis.Proposals) != 0 || len(govGenesis.Deposits) != 0 || len(govGenesis.Votes) != 0 {
		return fmt.Errorf("governance proposal, deposit, and vote records must remain empty in Stage C")
	}

	if err := app.validateBasicModuleGenesis(genesis); err != nil {
		return err
	}

	authGenesis := authtypes.DefaultGenesisState()
	if err := app.decodeGenesis(genesis, authtypes.ModuleName, authGenesis); err != nil {
		return err
	}
	if err := validateGenesisModuleAccounts(authGenesis); err != nil {
		return fmt.Errorf("validate auth module accounts: %w", err)
	}

	bankGenesis := banktypes.DefaultGenesisState()
	if err := app.decodeGenesis(genesis, banktypes.ModuleName, bankGenesis); err != nil {
		return err
	}
	if err := validateNativeMetadata(bankGenesis.DenomMetadata); err != nil {
		return fmt.Errorf("validate bank metadata: %w", err)
	}

	stakingGenesis := stakingtypes.DefaultGenesisState()
	if err := app.decodeGenesis(genesis, stakingtypes.ModuleName, stakingGenesis); err != nil {
		return err
	}
	if err := validateStakingParameterPolicy(stakingGenesis.Params); err != nil {
		return err
	}

	mintGenesis := minttypes.DefaultGenesisState()
	if err := app.decodeGenesis(genesis, minttypes.ModuleName, mintGenesis); err != nil {
		return err
	}
	if err := validateMintGenesisPolicy(mintGenesis); err != nil {
		return err
	}

	evmGenesis := evmtypes.DefaultGenesisState()
	if err := app.decodeGenesis(genesis, evmtypes.ModuleName, evmGenesis); err != nil {
		return err
	}
	if err := validateVMGenesisPolicy(*evmGenesis, app.installedPrecompiles); err != nil {
		return fmt.Errorf("validate VM params: %w", err)
	}
	if err := validateVMGenesisAccounts(authGenesis, evmGenesis); err != nil {
		return fmt.Errorf("validate VM genesis accounts: %w", err)
	}

	ibcGenesis := new(ibctypes.GenesisState)
	if err := app.decodeGenesis(genesis, ibcexported.ModuleName, ibcGenesis); err != nil {
		return err
	}
	if !gogoproto.Equal(ibcGenesis, ibctypes.DefaultGenesisState()) {
		return fmt.Errorf("IBC genesis must remain empty and equal to the upstream default in Stage C")
	}

	return nil
}

func validateGovGenesisJSONShape(raw json.RawMessage) error {
	var shape struct {
		Deposits  []json.RawMessage `json:"deposits"`
		Votes     []json.RawMessage `json:"votes"`
		Proposals []json.RawMessage `json:"proposals"`
		Params    json.RawMessage   `json:"params"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return fmt.Errorf("decode %s genesis shape: %w", govtypes.ModuleName, err)
	}
	if len(shape.Params) == 0 || bytes.Equal(bytes.TrimSpace(shape.Params), []byte("null")) {
		return fmt.Errorf("governance params cannot be nil")
	}
	lists := []struct {
		name   string
		values []json.RawMessage
	}{
		{name: "deposits", values: shape.Deposits},
		{name: "votes", values: shape.Votes},
		{name: "proposals", values: shape.Proposals},
	}
	for _, list := range lists {
		for index, value := range list.values {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("governance %s cannot contain null entry at index %d", list.name, index)
			}
		}
	}
	return nil
}

// validateBasicModuleGenesis converts upstream validators that dereference
// malformed nil decimal fields into an ordinary genesis validation error.
func (app *App) validateBasicModuleGenesis(genesis GenesisState) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("validate module genesis: upstream validator panic: %v", recovered)
		}
	}()
	if err := app.BasicModuleManager.ValidateGenesis(app.AppCodec(), app.TxConfig(), genesis); err != nil {
		return fmt.Errorf("validate module genesis: %w", err)
	}
	return nil
}

func validateGenesisModuleAccounts(genesis *authtypes.GenesisState) error {
	accounts, err := authtypes.UnpackAccounts(genesis.Accounts)
	if err != nil {
		return err
	}

	expectedPermissions := moduleAccountPermissions()
	expectedNamesByAddress := make(map[string]string, len(expectedPermissions))
	for moduleName := range expectedPermissions {
		expectedNamesByAddress[string(authtypes.NewModuleAddress(moduleName))] = moduleName
	}

	for _, account := range accounts {
		expectedName, collides := expectedNamesByAddress[string(account.GetAddress())]
		moduleAccount, isModuleAccount := account.(sdk.ModuleAccountI)
		if !collides {
			if isModuleAccount {
				return fmt.Errorf("unexpected module account %q", moduleAccount.GetName())
			}
			continue
		}
		if !isModuleAccount {
			return fmt.Errorf("account %s collides with module account %q", account.GetAddress(), expectedName)
		}
		if moduleAccount.GetName() != expectedName {
			return fmt.Errorf("module account at %s must be named %q, got %q", account.GetAddress(), expectedName, moduleAccount.GetName())
		}

		expected := slices.Clone(expectedPermissions[expectedName])
		actual := slices.Clone(moduleAccount.GetPermissions())
		slices.Sort(expected)
		slices.Sort(actual)
		if !slices.Equal(expected, actual) {
			return fmt.Errorf("module account %q permissions must be %v, got %v", expectedName, expected, actual)
		}
	}
	return nil
}

func validateMintGenesisPolicy(genesis *minttypes.GenesisState) error {
	if err := validateMintParameterPolicy(genesis.Params); err != nil {
		return err
	}
	if genesis.Minter.Inflation.IsNil() {
		return fmt.Errorf("mint genesis inflation cannot be nil")
	}
	if genesis.Minter.Inflation.IsNegative() || genesis.Minter.Inflation.GT(genesis.Params.InflationMax) {
		return fmt.Errorf(
			"mint genesis inflation must be between zero and inflation max %s, got %s",
			genesis.Params.InflationMax,
			genesis.Minter.Inflation,
		)
	}
	if genesis.Minter.AnnualProvisions.IsNil() || genesis.Minter.AnnualProvisions.IsNegative() {
		return fmt.Errorf("mint genesis annual provisions must be non-negative and non-nil")
	}
	return nil
}

func validateVMGenesisAccounts(
	authGenesis *authtypes.GenesisState,
	vmGenesis *evmtypes.GenesisState,
) error {
	authAccounts, err := authtypes.UnpackAccounts(authGenesis.Accounts)
	if err != nil {
		return err
	}
	authAddresses := make(map[string]struct{}, len(authAccounts))
	for _, account := range authAccounts {
		authAddresses[string(account.GetAddress())] = struct{}{}
	}

	reserved := make(map[string]string)
	for moduleName := range moduleAccountPermissions() {
		reserved[string(authtypes.NewModuleAddress(moduleName))] = fmt.Sprintf("module account %q", moduleName)
	}
	for _, rawAddress := range evmtypes.AvailableStaticPrecompiles {
		address := common.HexToAddress(rawAddress)
		reserved[string(address.Bytes())] = fmt.Sprintf("reserved precompile %s", address.Hex())
	}
	for _, address := range ethvm.PrecompiledAddressesPrague {
		reserved[string(address.Bytes())] = fmt.Sprintf("reserved precompile %s", address.Hex())
	}

	for _, account := range vmGenesis.Accounts {
		address := common.HexToAddress(account.Address)
		if reservation, found := reserved[string(address.Bytes())]; found {
			return fmt.Errorf("VM genesis account %s collides with %s", address.Hex(), reservation)
		}
		if _, found := authAddresses[string(address.Bytes())]; !found {
			return fmt.Errorf("VM genesis account %s has no matching auth account", address.Hex())
		}
	}
	return nil
}

func validateVMGenesisAccountEncoding(genesis *evmtypes.GenesisState) error {
	seenAccounts := make(map[common.Address]struct{}, len(genesis.Accounts))
	for accountIndex, account := range genesis.Accounts {
		address := common.HexToAddress(account.Address)
		if account.Address != address.Hex() {
			return fmt.Errorf(
				"VM genesis account %d address must use canonical EIP-55 form %s",
				accountIndex,
				address.Hex(),
			)
		}
		if _, found := seenAccounts[address]; found {
			return fmt.Errorf("duplicate canonical VM genesis account %s", address.Hex())
		}
		seenAccounts[address] = struct{}{}

		if len(account.Code)%2 != 0 || strings.ToLower(account.Code) != account.Code {
			return fmt.Errorf("VM genesis account %s code must be canonical lowercase even-length hex", address.Hex())
		}
		if _, err := hex.DecodeString(account.Code); err != nil {
			return fmt.Errorf("VM genesis account %s has invalid code hex: %w", address.Hex(), err)
		}

		seenSlots := make(map[common.Hash]struct{}, len(account.Storage))
		for storageIndex, state := range account.Storage {
			key, err := canonicalHash(state.Key)
			if err != nil {
				return fmt.Errorf("VM genesis account %s storage %d key: %w", address.Hex(), storageIndex, err)
			}
			if _, found := seenSlots[key]; found {
				return fmt.Errorf("VM genesis account %s has duplicate canonical storage key %s", address.Hex(), key.Hex())
			}
			seenSlots[key] = struct{}{}
			if _, err := canonicalHash(state.Value); err != nil {
				return fmt.Errorf("VM genesis account %s storage %d value: %w", address.Hex(), storageIndex, err)
			}
		}
	}
	return nil
}

func canonicalHash(raw string) (common.Hash, error) {
	if len(raw) != 66 || !strings.HasPrefix(raw, "0x") || strings.ToLower(raw) != raw {
		return common.Hash{}, fmt.Errorf("must be canonical lowercase 32-byte 0x-prefixed hex")
	}
	decoded, err := hex.DecodeString(raw[2:])
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid hex: %w", err)
	}
	return common.BytesToHash(decoded), nil
}

func (app *App) decodeGenesis(genesis GenesisState, moduleName string, target gogoproto.Message) error {
	raw, ok := genesis[moduleName]
	if !ok {
		return fmt.Errorf("genesis is missing module %q", moduleName)
	}
	if err := app.AppCodec().UnmarshalJSON(raw, target); err != nil {
		return fmt.Errorf("decode %s genesis: %w", moduleName, err)
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

func validateNativeMetadata(metadata []banktypes.Metadata) error {
	expected := nativeMetadata()
	for _, candidate := range metadata {
		if candidate.Base != config.BaseDenom {
			continue
		}
		if candidate.Display != expected.Display || candidate.Name != expected.Name || candidate.Symbol != expected.Symbol {
			return fmt.Errorf("metadata for %q does not match Guru identity", config.BaseDenom)
		}
		baseUnit, displayUnit := false, false
		for _, unit := range candidate.DenomUnits {
			baseUnit = baseUnit || unit.Denom == config.BaseDenom && unit.Exponent == 0
			displayUnit = displayUnit || unit.Denom == config.DisplayDenom && unit.Exponent == config.DenomExponent
		}
		if !baseUnit || !displayUnit {
			return fmt.Errorf("metadata must contain %s/0 and %s/%d units", config.BaseDenom, config.DisplayDenom, config.DenomExponent)
		}
		return nil
	}
	return fmt.Errorf("metadata for base denom %q is missing", config.BaseDenom)
}

func validateOnlyNativeCoins(coins sdk.Coins) error {
	if len(coins) == 0 {
		return fmt.Errorf("coin list cannot be empty")
	}
	for _, coin := range coins {
		if coin.Denom != config.BaseDenom {
			return fmt.Errorf("expected denom %q, got %q", config.BaseDenom, coin.Denom)
		}
		if coin.Amount.IsNil() {
			return fmt.Errorf("amount cannot be nil")
		}
		if !coin.Amount.IsPositive() {
			return fmt.Errorf("amount must be positive")
		}
	}
	return nil
}

func mustInt(value string) sdkmath.Int {
	result, ok := sdkmath.NewIntFromString(value)
	if !ok {
		panic(fmt.Errorf("invalid integer constant %q", value))
	}
	return result
}
