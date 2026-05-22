package keeper

import (
	"bytes"
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k *Keeper) ValidateTxSelfBondConstraints(ctx context.Context, tx sdk.Tx) error {
	if tx == nil {
		return nil
	}

	minBond, err := k.minBondSource.GetMinValidatorBondAmount(ctx)
	if err != nil {
		return err
	}

	projectedSelfBond := make(map[string]sdkmath.Int, len(tx.GetMsgs()))
	for _, msg := range tx.GetMsgs() {
		if err := k.validateMsgSelfBondConstraints(ctx, msg, minBond, projectedSelfBond); err != nil {
			return err
		}
	}

	return nil
}

func (k *Keeper) validateMsgSelfBondConstraints(
	ctx context.Context,
	msg sdk.Msg,
	minBond sdkmath.Int,
	projectedSelfBond map[string]sdkmath.Int,
) error {
	switch m := msg.(type) {
	case *stakingtypes.MsgCreateValidator:
		return k.validateCreateValidatorSelfBond(m, minBond, projectedSelfBond)
	case *stakingtypes.MsgDelegate:
		return k.validateDelegateSelfBond(ctx, m, projectedSelfBond)
	case *stakingtypes.MsgUndelegate:
		return k.validateUndelegateSelfBond(ctx, m, minBond, projectedSelfBond)
	case *stakingtypes.MsgBeginRedelegate:
		return k.validateBeginRedelegateSelfBond(ctx, m, minBond, projectedSelfBond)
	case *stakingtypes.MsgCancelUnbondingDelegation:
		return k.validateCancelUnbondingDelegationSelfBond(ctx, m, projectedSelfBond)
	case *authztypes.MsgExec:
		msgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nested := range msgs {
			if err := k.validateMsgSelfBondConstraints(ctx, nested, minBond, projectedSelfBond); err != nil {
				return err
			}
		}
	}

	return nil
}

func (k *Keeper) validateCreateValidatorSelfBond(
	msg *stakingtypes.MsgCreateValidator,
	minBond sdkmath.Int,
	projectedSelfBond map[string]sdkmath.Int,
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

	validatorAddr, err := k.ValidatorAddressCodec().StringToBytes(msg.ValidatorAddress)
	if err != nil {
		return err
	}
	projectedSelfBond[string(validatorAddr)] = msg.Value.Amount

	return nil
}

func (k *Keeper) validateDelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgDelegate,
	projectedSelfBond map[string]sdkmath.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress)
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

func (k *Keeper) validateUndelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgUndelegate,
	minBond sdkmath.Int,
	projectedSelfBond map[string]sdkmath.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress)
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

func (k *Keeper) validateBeginRedelegateSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgBeginRedelegate,
	minBond sdkmath.Int,
	projectedSelfBond map[string]sdkmath.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorSrcAddress, msg.DelegatorAddress)
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

func (k *Keeper) validateCancelUnbondingDelegationSelfBond(
	ctx context.Context,
	msg *stakingtypes.MsgCancelUnbondingDelegation,
	projectedSelfBond map[string]sdkmath.Int,
) error {
	if msg.Amount.Denom != appparams.BaseDenom {
		return nil
	}

	validatorAddr, isSelfDelegation, err := k.parseSelfDelegationAddresses(msg.ValidatorAddress, msg.DelegatorAddress)
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

func (k *Keeper) ensureProjectedSelfBondAfterDecrease(
	ctx context.Context,
	validatorAddr sdk.ValAddress,
	decrease sdkmath.Int,
	minBond sdkmath.Int,
	messageType string,
	projectedSelfBond map[string]sdkmath.Int,
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

func (k *Keeper) parseSelfDelegationAddresses(
	validatorAddress string,
	delegatorAddress string,
) (sdk.ValAddress, bool, error) {
	validatorAddr, err := k.ValidatorAddressCodec().StringToBytes(validatorAddress)
	if err != nil {
		return nil, false, err
	}
	delegatorAddr, err := k.accountCodec.StringToBytes(delegatorAddress)
	if err != nil {
		return nil, false, err
	}

	return sdk.ValAddress(validatorAddr), bytes.Equal(validatorAddr, delegatorAddr), nil
}

func (k *Keeper) getProjectedSelfBond(
	ctx context.Context,
	validatorAddr sdk.ValAddress,
	projectedSelfBond map[string]sdkmath.Int,
) (sdkmath.Int, bool, error) {
	key := string(validatorAddr)
	if selfBond, ok := projectedSelfBond[key]; ok {
		return selfBond, true, nil
	}

	selfBond, err := k.GetValidatorSelfBond(ctx, validatorAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return sdkmath.Int{}, false, nil
		}
		return sdkmath.Int{}, false, err
	}

	projectedSelfBond[key] = selfBond
	return selfBond, true, nil
}

func (k *Keeper) mustValidatorAddressString(addr sdk.ValAddress) string {
	validatorAddress, err := k.ValidatorAddressCodec().BytesToString(addr)
	if err != nil {
		return "<invalid-validator-address>"
	}

	return validatorAddress
}
