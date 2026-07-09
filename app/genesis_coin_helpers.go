package app

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

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
