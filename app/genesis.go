package app

import (
	"encoding/json"
	"fmt"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

// BuildChainDefaultGenesis centralizes the chain-specific default genesis policy.
func (app *App) BuildChainDefaultGenesis() map[string]json.RawMessage {
	return app.DefaultGenesis()
}

// ValidateChainGenesis validates chain-level and cross-module invariants only.
// Module-level schema/default validation is owned by each module's ValidateGenesis.
func (app *App) ValidateChainGenesis(genesis map[string]json.RawMessage) error {
	if err := app.BasicModuleManager.ValidateGenesis(app.appCodec, app.txConfig, genesis); err != nil {
		return fmt.Errorf("module validation failed: %w", err)
	}

	bankGenesis := banktypes.DefaultGenesisState()
	if bz, ok := genesis[banktypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, bankGenesis); err != nil {
			return fmt.Errorf("failed to decode bank genesis: %w", err)
		}
	}
	if err := validateBaseDenomMetadata(bankGenesis.DenomMetadata); err != nil {
		return fmt.Errorf("invalid bank genesis metadata: %w", err)
	}
	for _, balance := range bankGenesis.Balances {
		if err := validateCoinsDenom(balance.Coins, "", false); err != nil {
			return fmt.Errorf("invalid bank balance for %s: %w", balance.Address, err)
		}
	}
	if err := validateCoinsDenom(bankGenesis.Supply, "", false); err != nil {
		return fmt.Errorf("invalid bank supply: %w", err)
	}

	stakingGenesis := stakingtypes.DefaultGenesisState()
	if bz, ok := genesis[stakingtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, stakingGenesis); err != nil {
			return fmt.Errorf("failed to decode staking genesis: %w", err)
		}
	}
	if stakingGenesis.Params.BondDenom != appparams.BaseDenom {
		return fmt.Errorf("staking bond denom must be %q, got %q", appparams.BaseDenom, stakingGenesis.Params.BondDenom)
	}

	mintGenesis := minttypes.DefaultGenesisState()
	if bz, ok := genesis[minttypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, mintGenesis); err != nil {
			return fmt.Errorf("failed to decode mint genesis: %w", err)
		}
	}
	if mintGenesis.Params.MintDenom != appparams.BaseDenom {
		return fmt.Errorf("mint denom must be %q, got %q", appparams.BaseDenom, mintGenesis.Params.MintDenom)
	}

	govGenesis := govv1.DefaultGenesisState()
	if bz, ok := genesis[govtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, govGenesis); err != nil {
			return fmt.Errorf("failed to decode gov genesis: %w", err)
		}
	}
	if govGenesis.Params == nil {
		return fmt.Errorf("gov params cannot be nil")
	}
	if err := validateCoinsDenom(govGenesis.Params.MinDeposit, appparams.BaseDenom, true); err != nil {
		return fmt.Errorf("invalid gov min_deposit: %w", err)
	}
	if err := validateCoinsDenom(govGenesis.Params.ExpeditedMinDeposit, appparams.BaseDenom, true); err != nil {
		return fmt.Errorf("invalid gov expedited_min_deposit: %w", err)
	}
	minDepositAmt := amountOfDenom(govGenesis.Params.MinDeposit, appparams.BaseDenom)
	expeditedDepositAmt := amountOfDenom(govGenesis.Params.ExpeditedMinDeposit, appparams.BaseDenom)
	if !expeditedDepositAmt.GT(minDepositAmt) {
		return fmt.Errorf(
			"gov expedited_min_deposit must be greater than min_deposit (got %s <= %s)",
			expeditedDepositAmt.String(),
			minDepositAmt.String(),
		)
	}

	evmGenesis := evmtypes.DefaultGenesisState()
	if bz, ok := genesis[evmtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, evmGenesis); err != nil {
			return fmt.Errorf("failed to decode evm genesis: %w", err)
		}
	}
	if evmGenesis.Params.EvmDenom != appparams.BaseDenom {
		return fmt.Errorf("evm denom must be %q, got %q", appparams.BaseDenom, evmGenesis.Params.EvmDenom)
	}
	if evmGenesis.Params.ExtendedDenomOptions == nil {
		return fmt.Errorf("evm extended denom options cannot be nil")
	}
	if evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom != appparams.BaseDenom {
		return fmt.Errorf(
			"evm extended denom must be %q, got %q",
			appparams.BaseDenom,
			evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom,
		)
	}

	return nil
}

func (app *App) DefaultGenesis() map[string]json.RawMessage {
	genesis := app.BasicModuleManager.DefaultGenesis(app.appCodec)

	// 1) bank metadata: agxn(base) / gxn(display) / 18 decimals
	bankGenesis := banktypes.DefaultGenesisState()
	if bz, ok := genesis[banktypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, bankGenesis)
	}
	bankGenesis.DenomMetadata = upsertBaseDenomMetadata(bankGenesis.DenomMetadata)
	genesis[banktypes.ModuleName] = app.appCodec.MustMarshalJSON(bankGenesis)

	// 2) staking bond denom
	stakingGenesis := stakingtypes.DefaultGenesisState()
	if bz, ok := genesis[stakingtypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, stakingGenesis)
	}
	stakingGenesis.Params.BondDenom = appparams.BaseDenom
	genesis[stakingtypes.ModuleName] = app.appCodec.MustMarshalJSON(stakingGenesis)

	// 3) mint denom
	mintGenesis := minttypes.DefaultGenesisState()
	if bz, ok := genesis[minttypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, mintGenesis)
	}
	mintGenesis.Params.MintDenom = appparams.BaseDenom
	genesis[minttypes.ModuleName] = app.appCodec.MustMarshalJSON(mintGenesis)

	// 4) gov deposit denoms
	govGenesis := govv1.DefaultGenesisState()
	if bz, ok := genesis[govtypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, govGenesis)
	}
	if govGenesis.Params == nil {
		defaultParams := govv1.DefaultParams()
		govGenesis.Params = &defaultParams
	}
	govGenesis.Params.MinDeposit = normalizeDepositDenom(
		govGenesis.Params.MinDeposit,
		sdkmath.NewInt(1_000_000_000_000_000_000), // 1 gxn in agxn units
	)
	govGenesis.Params.ExpeditedMinDeposit = normalizeDepositDenom(
		govGenesis.Params.ExpeditedMinDeposit,
		sdkmath.NewInt(5_000_000_000_000_000_000), // 5 gxn in agxn units
	)
	minDepositAmt := amountOfDenom(govGenesis.Params.MinDeposit, appparams.BaseDenom)
	expeditedDepositAmt := amountOfDenom(govGenesis.Params.ExpeditedMinDeposit, appparams.BaseDenom)
	if !expeditedDepositAmt.GT(minDepositAmt) {
		govGenesis.Params.ExpeditedMinDeposit = sdk.NewCoins(
			sdk.NewCoin(appparams.BaseDenom, minDepositAmt.MulRaw(5)),
		)
	}
	genesis[govtypes.ModuleName] = app.appCodec.MustMarshalJSON(govGenesis)

	// 5) EVM denom and extended denom
	evmGenesis := evmtypes.DefaultGenesisState()
	if bz, ok := genesis[evmtypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, evmGenesis)
	}
	evmGenesis.Params.EvmDenom = appparams.BaseDenom
	if evmGenesis.Params.ExtendedDenomOptions == nil {
		evmGenesis.Params.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{}
	}
	evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom = appparams.BaseDenom
	genesis[evmtypes.ModuleName] = app.appCodec.MustMarshalJSON(evmGenesis)

	return genesis
}

func validateBaseDenomMetadata(metadata []banktypes.Metadata) error {
	for _, item := range metadata {
		if item.Base != appparams.BaseDenom {
			continue
		}

		if item.Display != appparams.DisplayDenom {
			return fmt.Errorf("display denom must be %q, got %q", appparams.DisplayDenom, item.Display)
		}
		if item.Name != "Guru" {
			return fmt.Errorf("name must be %q, got %q", "Guru", item.Name)
		}
		if item.Symbol != "GXN" {
			return fmt.Errorf("symbol must be %q, got %q", "GXN", item.Symbol)
		}

		hasBaseUnit := false
		hasDisplayUnit := false
		for _, unit := range item.DenomUnits {
			if unit.Denom == appparams.BaseDenom && unit.Exponent == 0 {
				hasBaseUnit = true
			}
			if unit.Denom == appparams.DisplayDenom && unit.Exponent == 18 {
				hasDisplayUnit = true
			}
		}

		if !hasBaseUnit {
			return fmt.Errorf("missing denom unit for base denom %q", appparams.BaseDenom)
		}
		if !hasDisplayUnit {
			return fmt.Errorf("missing denom unit for display denom %q with exponent 18", appparams.DisplayDenom)
		}

		return nil
	}

	return fmt.Errorf("missing bank metadata for base denom %q", appparams.BaseDenom)
}

func upsertBaseDenomMetadata(metadata []banktypes.Metadata) []banktypes.Metadata {
	baseMetadata := banktypes.Metadata{
		Description: "The native staking token of the Guru chain",
		Base:        appparams.BaseDenom,
		Display:     appparams.DisplayDenom,
		Name:        "Guru",
		Symbol:      "GXN",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: appparams.BaseDenom, Exponent: 0},
			{Denom: appparams.DisplayDenom, Exponent: 18},
		},
	}

	for i := range metadata {
		if metadata[i].Base == appparams.BaseDenom {
			metadata[i] = baseMetadata
			return metadata
		}
	}

	return append(metadata, baseMetadata)
}

func validateCoinsDenom(coins sdk.Coins, expectedDenom string, requirePositive bool) error {
	total := sdkmath.ZeroInt()

	for _, coin := range coins {
		if expectedDenom != "" && coin.Denom != expectedDenom {
			return fmt.Errorf("found denom %q, expected only %q", coin.Denom, expectedDenom)
		}
		if !coin.Amount.IsPositive() {
			return fmt.Errorf("amount for denom %q must be positive", coin.Denom)
		}
		total = total.Add(coin.Amount)
	}

	if requirePositive && !total.IsPositive() {
		if expectedDenom == "" {
			return fmt.Errorf("total coin amount must be positive")
		}
		return fmt.Errorf("total amount for denom %q must be positive", expectedDenom)
	}

	return nil
}

func normalizeDepositDenom(coins sdk.Coins, fallbackAmount sdkmath.Int) sdk.Coins {
	totalAmount := sdkmath.ZeroInt()
	for _, coin := range coins {
		totalAmount = totalAmount.Add(coin.Amount)
	}
	if !totalAmount.IsPositive() {
		totalAmount = fallbackAmount
	}

	return sdk.NewCoins(
		sdk.NewCoin(appparams.BaseDenom, totalAmount),
	)
}

func amountOfDenom(coins []sdk.Coin, denom string) sdkmath.Int {
	amount := sdkmath.ZeroInt()
	for _, coin := range coins {
		if coin.Denom == denom {
			amount = amount.Add(coin.Amount)
		}
	}

	return amount
}
