package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/core/address"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type Keeper struct {
	*stakingkeeper.Keeper
	minBondSource MinValidatorBondSource
	accountCodec  address.Codec
}

func NewKeeper(
	stakingKeeper *stakingkeeper.Keeper,
	minBondSource MinValidatorBondSource,
	accountCodec address.Codec,
) *Keeper {
	return &Keeper{
		Keeper:        stakingKeeper,
		minBondSource: minBondSource,
		accountCodec:  accountCodec,
	}
}

func (k *Keeper) EndBlocker(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	defer telemetry.ModuleMeasureSince(stakingtypes.ModuleName, telemetry.Now(), telemetry.MetricKeyEndBlocker) //nolint:staticcheck // TODO: switch to OpenTelemetry
	return k.BlockValidatorUpdates(ctx)
}

func (k *Keeper) BlockValidatorUpdates(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	validatorUpdates, err := k.ApplyAndReturnValidatorSetUpdates(ctx)
	if err != nil {
		return nil, err
	}

	if err := k.UnbondAllMatureValidators(ctx); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	matureUnbonds, err := k.DequeueAllMatureUBDQueue(ctx, sdkCtx.BlockHeader().Time)
	if err != nil {
		return nil, err
	}

	for _, dvPair := range matureUnbonds {
		validatorAddr, err := k.ValidatorAddressCodec().StringToBytes(dvPair.ValidatorAddress)
		if err != nil {
			return nil, err
		}
		delegatorAddr, err := k.accountCodec.StringToBytes(dvPair.DelegatorAddress)
		if err != nil {
			return nil, err
		}

		balances, err := k.CompleteUnbonding(ctx, delegatorAddr, validatorAddr)
		if err != nil {
			continue
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				stakingtypes.EventTypeCompleteUnbonding,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(stakingtypes.AttributeKeyValidator, dvPair.ValidatorAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeyDelegator, dvPair.DelegatorAddress),
			),
		)
	}

	matureRedelegations, err := k.DequeueAllMatureRedelegationQueue(ctx, sdkCtx.BlockHeader().Time)
	if err != nil {
		return nil, err
	}

	for _, dvvTriplet := range matureRedelegations {
		valSrcAddr, err := k.ValidatorAddressCodec().StringToBytes(dvvTriplet.ValidatorSrcAddress)
		if err != nil {
			return nil, err
		}
		valDstAddr, err := k.ValidatorAddressCodec().StringToBytes(dvvTriplet.ValidatorDstAddress)
		if err != nil {
			return nil, err
		}
		delegatorAddr, err := k.accountCodec.StringToBytes(dvvTriplet.DelegatorAddress)
		if err != nil {
			return nil, err
		}

		balances, err := k.CompleteRedelegation(ctx, delegatorAddr, valSrcAddr, valDstAddr)
		if err != nil {
			continue
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				stakingtypes.EventTypeCompleteRedelegation,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(stakingtypes.AttributeKeyDelegator, dvvTriplet.DelegatorAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeySrcValidator, dvvTriplet.ValidatorSrcAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeyDstValidator, dvvTriplet.ValidatorDstAddress),
			),
		)
	}

	return validatorUpdates, nil
}

func (k *Keeper) ApplyAndReturnValidatorSetUpdates(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	if err := k.excludeValidatorsBelowMinSelfBondFromPowerIndex(ctx); err != nil {
		return nil, err
	}

	return k.Keeper.ApplyAndReturnValidatorSetUpdates(ctx)
}

func (k *Keeper) excludeValidatorsBelowMinSelfBondFromPowerIndex(ctx context.Context) error {
	minBond, err := k.minBondSource.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	iterator, err := k.ValidatorsPowerStoreIterator(ctx)
	if err != nil {
		return err
	}
	iteratorClosed := false
	defer func() {
		if !iteratorClosed {
			iterator.Close()
		}
	}()

	validatorsToRemove := make([]stakingtypes.Validator, 0)
	for ; iterator.Valid(); iterator.Next() {
		validatorAddr := sdk.ValAddress(iterator.Value())
		validator, err := k.GetValidator(ctx, validatorAddr)
		if err != nil {
			return err
		}

		selfBond, err := k.getValidatorSelfBondFromDelegation(ctx, validatorAddr, validator)
		if err != nil {
			return err
		}
		if selfBond.LT(minBond) {
			validatorsToRemove = append(validatorsToRemove, validator)
		}
	}
	iterator.Close()
	iteratorClosed = true

	for _, validator := range validatorsToRemove {
		if err := k.DeleteValidatorByPowerIndex(ctx, validator); err != nil {
			return err
		}
	}

	return nil
}

func (k *Keeper) GetValidatorSelfBond(ctx context.Context, validatorAddr sdk.ValAddress) (sdkmath.Int, error) {
	validator, err := k.GetValidator(ctx, validatorAddr)
	if err != nil {
		return sdkmath.Int{}, err
	}

	selfBond, err := k.getValidatorSelfBondFromDelegation(ctx, validatorAddr, validator)
	if err != nil {
		return sdkmath.Int{}, err
	}

	return selfBond, nil
}

func (k *Keeper) getValidatorSelfBondFromDelegation(ctx context.Context, validatorAddr sdk.ValAddress, validator stakingtypes.Validator) (sdkmath.Int, error) {
	delegation, err := k.GetDelegation(ctx, sdk.AccAddress(validatorAddr), validatorAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoDelegation) {
			return sdkmath.ZeroInt(), nil
		}

		return sdkmath.Int{}, err
	}

	return validator.TokensFromSharesTruncated(delegation.GetShares()).TruncateInt(), nil
}
