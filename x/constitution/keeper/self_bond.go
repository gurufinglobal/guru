package keeper

import (
	"bytes"
	"context"
	"errors"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k Keeper) ValidateTxSelfBondConstraints(ctx context.Context, tx sdk.Tx, accountCodec address.Codec) error {
	if tx == nil {
		return nil
	}

	minBond, err := k.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	projectedSelfBond := make(map[string]math.Int, len(tx.GetMsgs()))
	for _, msg := range tx.GetMsgs() {
		if err := k.validateMsgSelfBondConstraints(ctx, msg, accountCodec, minBond, projectedSelfBond); err != nil {
			return err
		}
	}

	return nil
}

func (k Keeper) validateMsgSelfBondConstraints(
	ctx context.Context,
	msg sdk.Msg,
	accountCodec address.Codec,
	minBond math.Int,
	projectedSelfBond map[string]math.Int,
) error {
	switch m := msg.(type) {
	case *stakingtypes.MsgCreateValidator:
		return k.validateCreateValidatorSelfBond(m, minBond, projectedSelfBond)
	case *stakingtypes.MsgDelegate:
		return k.validateDelegateSelfBond(ctx, m, accountCodec, projectedSelfBond)
	case *stakingtypes.MsgUndelegate:
		return k.validateUndelegateSelfBond(ctx, m, accountCodec, minBond, projectedSelfBond)
	case *stakingtypes.MsgBeginRedelegate:
		return k.validateBeginRedelegateSelfBond(ctx, m, accountCodec, minBond, projectedSelfBond)
	case *stakingtypes.MsgCancelUnbondingDelegation:
		return k.validateCancelUnbondingDelegationSelfBond(ctx, m, accountCodec, projectedSelfBond)
	case *authztypes.MsgExec:
		msgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nested := range msgs {
			if err := k.validateMsgSelfBondConstraints(ctx, nested, accountCodec, minBond, projectedSelfBond); err != nil {
				return err
			}
		}
	}

	return nil
}

func (k Keeper) validateCreateValidatorSelfBond(
	msg *stakingtypes.MsgCreateValidator,
	minBond math.Int,
	projectedSelfBond map[string]math.Int,
) error {
	if msg.Value.Denom != appparams.BaseDenom {
		return nil
	}

	if msg.Value.Amount.LT(minBond) {
		return constitutiontypes.ErrSelfBondBelowMin.Wrapf(
			"msg create validator self-bond %s below minimum %s",
			msg.Value.Amount,
			minBond,
		)
	}

	validatorAddr, err := k.stakingKeeper.ValidatorAddressCodec().StringToBytes(msg.ValidatorAddress)
	if err != nil {
		return err
	}
	projectedSelfBond[string(validatorAddr)] = msg.Value.Amount

	return nil
}

func (k Keeper) validateDelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgDelegate,
	accountCodec address.Codec,
	projectedSelfBond map[string]math.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress, accountCodec)
	if err != nil {
		return err
	}
	if !isSelfDelegation {
		return nil
	}

	currentSelfBond, exists, err := k.getProjectedSelfBond(ctx, validatorAddr, projectedSelfBond)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	projectedSelfBond[string(validatorAddr)] = currentSelfBond.Add(msg.Amount.Amount)
	return nil
}

func (k Keeper) validateUndelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgUndelegate,
	accountCodec address.Codec,
	minBond math.Int,
	projectedSelfBond map[string]math.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress, accountCodec)
	if err != nil {
		return err
	}
	if !isSelfDelegation {
		return nil
	}

	return k.ensureProjectedSelfBondAfterDecrease(
		ctx,
		validatorAddr,
		msg.Amount.Amount,
		minBond,
		"msg undelegate",
		projectedSelfBond,
	)
}

func (k Keeper) validateBeginRedelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgBeginRedelegate,
	accountCodec address.Codec,
	minBond math.Int,
	projectedSelfBond map[string]math.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorSrcAddress, msg.DelegatorAddress, accountCodec)
	if err != nil {
		return err
	}
	if !isSelfDelegation {
		return nil
	}

	return k.ensureProjectedSelfBondAfterDecrease(
		ctx,
		validatorAddr,
		msg.Amount.Amount,
		minBond,
		"msg begin redelegate",
		projectedSelfBond,
	)
}

func (k Keeper) validateCancelUnbondingDelegationSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgCancelUnbondingDelegation,
	accountCodec address.Codec,
	projectedSelfBond map[string]math.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress, accountCodec)
	if err != nil {
		return err
	}
	if !isSelfDelegation {
		return nil
	}

	currentSelfBond, exists, err := k.getProjectedSelfBond(ctx, validatorAddr, projectedSelfBond)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	projectedSelfBond[string(validatorAddr)] = currentSelfBond.Add(msg.Amount.Amount)
	return nil
}

func (k Keeper) ensureProjectedSelfBondAfterDecrease(
	ctx context.Context,
	validatorAddr sdk.ValAddress,
	decrease math.Int,
	minBond math.Int,
	messageType string,
	projectedSelfBond map[string]math.Int,
) error {
	currentSelfBond, exists, err := k.getProjectedSelfBond(ctx, validatorAddr, projectedSelfBond)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	remaining := currentSelfBond.Sub(decrease)
	if remaining.LT(minBond) {
		return constitutiontypes.ErrSelfBondBelowMin.Wrapf(
			"%s reduces self-bond below minimum: remaining=%s min=%s validator=%s",
			messageType,
			remaining,
			minBond,
			k.mustValidatorAddressString(validatorAddr),
		)
	}

	projectedSelfBond[string(validatorAddr)] = remaining
	return nil
}

func (k Keeper) parseSelfDelegationAddresses(
	validatorAddress string,
	delegatorAddress string,
	accountCodec address.Codec,
) (sdk.ValAddress, bool, error) {
	validatorAddr, err := k.stakingKeeper.ValidatorAddressCodec().StringToBytes(validatorAddress)
	if err != nil {
		return nil, false, err
	}
	delegatorAddr, err := accountCodec.StringToBytes(delegatorAddress)
	if err != nil {
		return nil, false, err
	}

	return sdk.ValAddress(validatorAddr), bytes.Equal(validatorAddr, delegatorAddr), nil
}

func (k Keeper) getProjectedSelfBond(
	ctx context.Context,
	validatorAddr sdk.ValAddress,
	projectedSelfBond map[string]math.Int,
) (math.Int, bool, error) {
	key := string(validatorAddr)
	if selfBond, ok := projectedSelfBond[key]; ok {
		return selfBond, true, nil
	}

	selfBond, err := k.GetValidatorSelfBond(ctx, validatorAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return math.Int{}, false, nil
		}
		return math.Int{}, false, err
	}

	projectedSelfBond[key] = selfBond
	return selfBond, true, nil
}

func (k Keeper) EnsureValidatorMinSelfBond(ctx context.Context, validatorAddr sdk.ValAddress, minBond math.Int) error {
	selfBond, err := k.GetValidatorSelfBond(ctx, validatorAddr)
	if err != nil {
		return err
	}
	if selfBond.LT(minBond) {
		return constitutiontypes.ErrSelfBondBelowMin.Wrapf(
			"validator self-bond below minimum: self_bond=%s min=%s validator=%s",
			selfBond,
			minBond,
			k.mustValidatorAddressString(validatorAddr),
		)
	}

	return nil
}

func (k Keeper) GetValidatorSelfBond(ctx context.Context, validatorAddr sdk.ValAddress) (math.Int, error) {
	_, selfBond, err := k.getValidatorAndSelfBond(ctx, validatorAddr)
	return selfBond, err
}

func (k Keeper) getValidatorAndSelfBond(ctx context.Context, validatorAddr sdk.ValAddress) (stakingtypes.Validator, math.Int, error) {
	validator, err := k.stakingKeeper.GetValidator(ctx, validatorAddr)
	if err != nil {
		return stakingtypes.Validator{}, math.Int{}, err
	}

	delegation, err := k.stakingKeeper.GetDelegation(ctx, sdk.AccAddress(validatorAddr), validatorAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoDelegation) {
			return validator, math.ZeroInt(), nil
		}
		return stakingtypes.Validator{}, math.Int{}, err
	}

	selfBond := validator.TokensFromSharesTruncated(delegation.GetShares()).TruncateInt()
	return validator, selfBond, nil
}

func (k Keeper) mustValidatorAddressString(addr sdk.ValAddress) string {
	validatorAddress, err := k.stakingKeeper.ValidatorAddressCodec().BytesToString(addr)
	if err != nil {
		return "<invalid-validator-address>"
	}

	return validatorAddress
}
