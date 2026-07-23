package keeper

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k Keeper) GetParams(ctx context.Context) (*constitutiontypes.Params, error) {
	params, err := k.params.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &params, nil
}

func (k Keeper) SetParams(ctx context.Context, params *constitutiontypes.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}
	return k.params.Set(ctx, *params)
}

func (k Keeper) UpdateParams(ctx context.Context, params *constitutiontypes.Params) error {
	if err := ValidateParams(params); err != nil {
		return err
	}

	return k.params.Set(ctx, *params)
}

func ValidateParams(params *constitutiontypes.Params) error {
	if _, err := validateAndGetMinValidatorBondAmount(params); err != nil {
		return err
	}

	return nil
}

func MinValidatorBondAmount(params *constitutiontypes.Params) (math.Int, error) {
	return validateAndGetMinValidatorBondAmount(params)
}

func validateAndGetMinValidatorBondAmount(params *constitutiontypes.Params) (math.Int, error) {
	if params == nil {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrap("params cannot be nil")
	}

	return ValidateMinValidatorBondAmount(params.GetMinValidatorBondAmount())
}

func ValidateMinValidatorBondAmount(minBond *sdk.Coin) (math.Int, error) {
	if minBond == nil {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrap("min_validator_bond_amount cannot be nil")
	}
	if err := sdk.ValidateDenom(minBond.Denom); err != nil {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrapf("invalid min_validator_bond_amount denom: %v", err)
	}
	if minBond.Denom != appparams.BaseDenom {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrapf(
			"min_validator_bond_amount denom must be %q, got %q",
			appparams.BaseDenom,
			minBond.Denom,
		)
	}

	if minBond.Amount.IsNil() {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrap("min_validator_bond_amount amount cannot be nil")
	}
	if !minBond.Amount.IsPositive() {
		return math.Int{}, constitutiontypes.ErrInvalidParams.Wrapf(
			"min_validator_bond_amount amount must be positive, got %s",
			minBond.Amount.String(),
		)
	}

	return minBond.Amount, nil
}

func (k Keeper) GetMinValidatorBondAmountCoin(ctx context.Context) (*sdk.Coin, error) {
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

func (k Keeper) SetMinValidatorBondAmount(ctx context.Context, minBond *sdk.Coin) error {
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
