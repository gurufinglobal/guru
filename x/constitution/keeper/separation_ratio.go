package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k Keeper) GetSeparationRatio(ctx context.Context) (*constitutionv1.SeparationRatio, error) {
	ratio, err := k.separationRatio.Get(ctx)
	if err != nil {
		return nil, err
	}

	return ratio, nil
}

func (k Keeper) SetSeparationRatio(ctx context.Context, ratio *constitutionv1.SeparationRatio) error {
	if err := ValidateSeparationRatio(ratio); err != nil {
		return err
	}

	return k.separationRatio.Set(ctx, ratio)
}

func (k Keeper) UpdateSeparationRatio(ctx context.Context, ratio *constitutionv1.SeparationRatio) error {
	return k.SetSeparationRatio(ctx, ratio)
}

func ValidateSeparationRatio(ratio *constitutionv1.SeparationRatio) error {
	if ratio == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("separation_ratio cannot be nil")
	}

	base, err := validateSeparationRatioPart("base_ppm", ratio.GetBasePpm())
	if err != nil {
		return err
	}

	burn, err := validateSeparationRatioPart("burn_ppm", ratio.GetBurnPpm())
	if err != nil {
		return err
	}

	validators, err := validateSeparationRatioPart("validators_ppm", ratio.GetValidatorsPpm())
	if err != nil {
		return err
	}

	sum := uint64(base) + uint64(burn) + uint64(validators)
	if sum != uint64(constitutiontypes.SeparationRatioScalePPM) {
		return constitutiontypes.ErrInvalidParams.Wrapf(
			"separation_ratio total must be exactly %d ppm, got %d",
			constitutiontypes.SeparationRatioScalePPM,
			sum,
		)
	}

	return nil
}

func validateSeparationRatioPart(fieldName string, value uint32) (uint32, error) {
	if value > constitutiontypes.SeparationRatioScalePPM {
		return 0, constitutiontypes.ErrInvalidParams.Wrapf(
			"separation_ratio.%s cannot exceed %d ppm",
			fieldName,
			constitutiontypes.SeparationRatioScalePPM,
		)
	}

	return value, nil
}

func (k Keeper) ExecuteSeparation(ctx context.Context) error {
	if k.bankKeeper == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("bank keeper is not configured")
	}

	separationRatio, err := k.GetSeparationRatio(ctx)
	if err != nil {
		return err
	}
	if separationRatio.GetBasePpm() == 0 && separationRatio.GetBurnPpm() == 0 {
		return nil
	}

	baseAddressString, err := k.GetBaseAddress(ctx)
	if err != nil {
		return err
	}
	baseAddressBytes, err := k.accountCodec.StringToBytes(baseAddressString)
	if err != nil {
		return constitutiontypes.ErrInvalidParams.Wrapf("invalid base_address: %v", err)
	}

	feeCollectorAddress := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	feeCollectorBalances := k.bankKeeper.GetAllBalances(ctx, feeCollectorAddress)
	if feeCollectorBalances.Empty() {
		return nil
	}

	baseCoins, burnCoins := splitSeparationCoins(feeCollectorBalances, separationRatio)
	if !baseCoins.Empty() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx,
			authtypes.FeeCollectorName,
			sdk.AccAddress(baseAddressBytes),
			baseCoins,
		); err != nil {
			return err
		}
	}
	if !burnCoins.Empty() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			authtypes.FeeCollectorName,
			constitutiontypes.ModuleName,
			burnCoins,
		); err != nil {
			return err
		}
		if err := k.bankKeeper.BurnCoins(ctx, constitutiontypes.ModuleName, burnCoins); err != nil {
			return err
		}
	}

	return nil
}

func splitSeparationCoins(balances sdk.Coins, ratio *constitutionv1.SeparationRatio) (sdk.Coins, sdk.Coins) {
	baseCoins := sdk.NewCoins()
	burnCoins := sdk.NewCoins()

	for _, coin := range balances {
		baseAmount := calculatePPMAmount(coin.Amount, ratio.GetBasePpm())
		burnAmount := calculatePPMAmount(coin.Amount, ratio.GetBurnPpm())

		if baseAmount.IsPositive() {
			baseCoins = baseCoins.Add(sdk.NewCoin(coin.Denom, baseAmount))
		}
		if burnAmount.IsPositive() {
			burnCoins = burnCoins.Add(sdk.NewCoin(coin.Denom, burnAmount))
		}
	}

	return baseCoins, burnCoins
}

func calculatePPMAmount(amount sdkmath.Int, ppm uint32) sdkmath.Int {
	if ppm == 0 {
		return sdkmath.ZeroInt()
	}
	if ppm == constitutiontypes.SeparationRatioScalePPM {
		return amount
	}

	return amount.MulRaw(int64(ppm)).QuoRaw(int64(constitutiontypes.SeparationRatioScalePPM))
}
