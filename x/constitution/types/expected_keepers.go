package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	BeginUnbondingValidator(ctx context.Context, validator stakingtypes.Validator) (stakingtypes.Validator, error)
	GetDelegation(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error)
	IterateBondedValidatorsByPower(ctx context.Context, fn func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error
	ValidatorAddressCodec() address.Codec
}
