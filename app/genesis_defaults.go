package app

import (
	"encoding/json"
	"fmt"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

// BuildChainDefaultGenesis centralizes the chain-specific default genesis policy.
func (app *App) BuildChainDefaultGenesis() map[string]json.RawMessage {
	return app.DefaultGenesis()
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

	// 5) distribution community tax
	distrGenesis := distrtypes.DefaultGenesisState()
	if bz, ok := genesis[distrtypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, distrGenesis)
	}
	distrGenesis.Params.CommunityTax = sdkmath.LegacyZeroDec()
	genesis[distrtypes.ModuleName] = app.appCodec.MustMarshalJSON(distrGenesis)

	// 6) feemarket base fee policy
	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	if bz, ok := genesis[feemarkettypes.ModuleName]; ok {
		app.appCodec.MustUnmarshalJSON(bz, feeMarketGenesis)
	}
	feeMarketGenesis.Params.NoBaseFee = true
	feeMarketGenesis.Params.BaseFee = sdkmath.LegacyZeroDec()
	initialMinGasPrice, ok := sdkmath.NewIntFromString(constitutiontypes.MinGasPriceScaleFactor)
	if !ok {
		panic(fmt.Sprintf("invalid initial min gas price %q", constitutiontypes.MinGasPriceScaleFactor))
	}
	feeMarketGenesis.Params.MinGasPrice = initialMinGasPrice.ToLegacyDec()
	genesis[feemarkettypes.ModuleName] = app.appCodec.MustMarshalJSON(feeMarketGenesis)

	// 7) EVM denom and extended denom
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
