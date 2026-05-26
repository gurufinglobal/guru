package keeper

import (
	"context"

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
