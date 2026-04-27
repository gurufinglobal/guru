package app

import (
	"encoding/json"

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
