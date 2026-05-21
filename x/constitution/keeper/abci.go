package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (k Keeper) EndBlocker(ctx context.Context) error {
	minBond, err := k.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	enforceAllBonded, err := k.ShouldEnforceAllBonded(ctx)
	if err != nil {
		return err
	}

	var validatorsToUnbond []sdk.ValAddress
	if enforceAllBonded {
		validatorsToUnbond, err = k.collectBondedValidatorsBelowMinSelfBond(ctx, minBond)
		if err != nil {
			return err
		}
	} else {
		validatorsToUnbond, err = k.collectChangedBondedValidatorsBelowMinSelfBond(ctx, minBond)
		if err != nil {
			return err
		}
	}

	if err := k.beginUnbondingValidators(ctx, minBond, validatorsToUnbond); err != nil {
		return err
	}

	if enforceAllBonded {
		if err := k.SetEnforceAllBonded(ctx, false); err != nil {
			return err
		}
	}

	return k.changedValidators.Clear(ctx, nil)
}

func (k Keeper) MarkValidatorChanged(ctx context.Context, validatorAddr sdk.ValAddress) error {
	return k.changedValidators.Set(ctx, []byte(validatorAddr))
}

func (k Keeper) collectChangedBondedValidatorsBelowMinSelfBond(ctx context.Context, minBond math.Int) ([]sdk.ValAddress, error) {
	iterator, err := k.changedValidators.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	validatorsToUnbond := make([]sdk.ValAddress, 0)
	for ; iterator.Valid(); iterator.Next() {
		validatorAddr, err := iterator.Key()
		if err != nil {
			return nil, err
		}

		validator, selfBond, err := k.getValidatorAndSelfBond(ctx, sdk.ValAddress(validatorAddr))
		if err != nil {
			if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
				continue
			}
			return nil, err
		}

		if !validator.IsBonded() || !selfBond.LT(minBond) {
			continue
		}

		validatorsToUnbond = append(validatorsToUnbond, cloneValAddress(validatorAddr))
	}

	return validatorsToUnbond, nil
}

func (k Keeper) collectBondedValidatorsBelowMinSelfBond(ctx context.Context, minBond math.Int) ([]sdk.ValAddress, error) {
	validatorsToUnbond := make([]sdk.ValAddress, 0)
	var iterErr error

	err := k.stakingKeeper.IterateBondedValidatorsByPower(ctx, func(_ int64, validatorI stakingtypes.ValidatorI) bool {
		validatorAddr, err := k.stakingKeeper.ValidatorAddressCodec().StringToBytes(validatorI.GetOperator())
		if err != nil {
			iterErr = err
			return true
		}

		selfBond, err := k.GetValidatorSelfBond(ctx, sdk.ValAddress(validatorAddr))
		if err != nil {
			iterErr = err
			return true
		}

		if selfBond.LT(minBond) {
			validatorsToUnbond = append(validatorsToUnbond, cloneValAddress(validatorAddr))
		}

		return false
	})
	if err != nil {
		return nil, err
	}
	if iterErr != nil {
		return nil, iterErr
	}

	return validatorsToUnbond, nil
}

func (k Keeper) beginUnbondingValidators(ctx context.Context, minBond math.Int, validators []sdk.ValAddress) error {
	for _, validatorAddr := range validators {
		validator, selfBond, err := k.getValidatorAndSelfBond(ctx, validatorAddr)
		if err != nil {
			if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
				continue
			}
			return err
		}

		if !validator.IsBonded() || !selfBond.LT(minBond) {
			continue
		}

		if _, err := k.stakingKeeper.BeginUnbondingValidator(ctx, validator); err != nil {
			return err
		}
	}

	return nil
}

func cloneValAddress(addr []byte) sdk.ValAddress {
	clone := make([]byte, len(addr))
	copy(clone, addr)
	return clone
}
