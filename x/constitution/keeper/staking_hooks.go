package keeper

import (
	"bytes"
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

var _ stakingtypes.StakingHooks = StakingHooks{}

type StakingHooks struct {
	keeper *Keeper
}

func NewStakingHooks(keeper *Keeper) StakingHooks {
	return StakingHooks{keeper: keeper}
}

func (h StakingHooks) AfterValidatorCreated(ctx context.Context, valAddr sdk.ValAddress) error {
	return h.keeper.MarkValidatorChanged(ctx, valAddr)
}

func (h StakingHooks) BeforeValidatorModified(context.Context, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) AfterValidatorRemoved(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	return h.keeper.changedValidators.Remove(ctx, []byte(valAddr))
}

func (h StakingHooks) AfterValidatorBonded(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	return h.keeper.MarkValidatorChanged(ctx, valAddr)
}

func (h StakingHooks) AfterValidatorBeginUnbonding(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	return h.keeper.changedValidators.Remove(ctx, []byte(valAddr))
}

func (h StakingHooks) BeforeDelegationCreated(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationSharesModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

func (h StakingHooks) BeforeDelegationRemoved(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) error {
	if !bytes.Equal(delAddr, valAddr) {
		return nil
	}

	minBond, err := h.keeper.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	return constitutiontypes.ErrSelfBondBelowMin.Wrapf(
		"removing self-delegation is not allowed while minimum self-bond is %s",
		minBond,
	)
}

func (h StakingHooks) AfterDelegationModified(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) error {
	if !bytes.Equal(delAddr, valAddr) {
		return nil
	}

	minBond, err := h.keeper.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	if err := h.keeper.EnsureValidatorMinSelfBond(ctx, valAddr, minBond); err != nil {
		return err
	}

	return h.keeper.MarkValidatorChanged(ctx, valAddr)
}

func (h StakingHooks) BeforeValidatorSlashed(ctx context.Context, valAddr sdk.ValAddress, _ sdkmath.LegacyDec) error {
	return h.keeper.MarkValidatorChanged(ctx, valAddr)
}

func (h StakingHooks) AfterUnbondingInitiated(context.Context, uint64) error {
	return nil
}
