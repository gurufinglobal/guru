package keeper

import (
	"context"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k Keeper) GetParams(ctx context.Context) (*constitutionv1.Params, error) {
	params, err := k.params.Get(ctx)
	if err != nil {
		return nil, err
	}
	return params, nil
}

func (k Keeper) SetParams(ctx context.Context, params *constitutionv1.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}
	return k.params.Set(ctx, params)
}

func (k Keeper) UpdateParams(ctx context.Context, params *constitutionv1.Params) error {
	currentMinBond, err := k.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	newMinBond, err := validateAndGetMinValidatorBondAmount(params)
	if err != nil {
		return err
	}

	if err := k.params.Set(ctx, params); err != nil {
		return err
	}

	if newMinBond.GT(currentMinBond) {
		return k.SetEnforceAllBonded(ctx, true)
	}

	return nil
}

func ValidateParams(params *constitutionv1.Params) error {
	if _, err := validateAndGetMinValidatorBondAmount(params); err != nil {
		return err
	}

	return nil
}

func MinValidatorBondAmount(params *constitutionv1.Params) (math.Int, error) {
	return validateAndGetMinValidatorBondAmount(params)
}

func validateAndGetMinValidatorBondAmount(params *constitutionv1.Params) (math.Int, error) {
	if params == nil {
		return math.Int{}, types.ErrInvalidParams.Wrap("params cannot be nil")
	}

	return ValidateMinValidatorBondAmount(params.GetMinValidatorBondAmount())
}

func ValidateMinValidatorBondAmount(minBond *basev1beta1.Coin) (math.Int, error) {
	if minBond == nil {
		return math.Int{}, types.ErrInvalidParams.Wrap("min_validator_bond_amount cannot be nil")
	}
	if err := sdk.ValidateDenom(minBond.Denom); err != nil {
		return math.Int{}, types.ErrInvalidParams.Wrapf("invalid min_validator_bond_amount denom: %v", err)
	}
	if minBond.Denom != appparams.BaseDenom {
		return math.Int{}, types.ErrInvalidParams.Wrapf(
			"min_validator_bond_amount denom must be %q, got %q",
			appparams.BaseDenom,
			minBond.Denom,
		)
	}

	amount, ok := math.NewIntFromString(minBond.Amount)
	if !ok {
		return math.Int{}, types.ErrInvalidParams.Wrapf(
			"invalid min_validator_bond_amount amount %q: must be an integer string",
			minBond.Amount,
		)
	}
	if !amount.IsPositive() {
		return math.Int{}, types.ErrInvalidParams.Wrapf(
			"min_validator_bond_amount amount must be positive, got %s",
			minBond.Amount,
		)
	}

	return amount, nil
}

func (k Keeper) GetMinValidatorBondAmountCoin(ctx context.Context) (*basev1beta1.Coin, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return params.GetMinValidatorBondAmount(), nil
}

func (k Keeper) GetMinValidatorBondAmount(ctx context.Context) (math.Int, error) {
	coin, err := k.GetMinValidatorBondAmountCoin(ctx)
	if err != nil {
		return math.Int{}, err
	}

	amount, err := ValidateMinValidatorBondAmount(coin)
	if err != nil {
		return math.Int{}, err
	}

	return amount, nil
}

func (k Keeper) SetMinValidatorBondAmount(ctx context.Context, minBond *basev1beta1.Coin) error {
	if _, err := ValidateMinValidatorBondAmount(minBond); err != nil {
		return err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	params.MinValidatorBondAmount = minBond

	return k.UpdateParams(ctx, params)
}

func (k Keeper) ShouldEnforceAllBonded(ctx context.Context) (bool, error) {
	has, err := k.enforceAllBonded.Has(ctx)
	if err != nil {
		return false, err
	}
	if !has {
		return false, nil
	}

	return k.enforceAllBonded.Get(ctx)
}

func (k Keeper) SetEnforceAllBonded(ctx context.Context, enforce bool) error {
	if !enforce {
		return k.enforceAllBonded.Remove(ctx)
	}

	return k.enforceAllBonded.Set(ctx, true)
}
