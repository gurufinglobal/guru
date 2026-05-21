package keeper

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestStakingHooksValidatorLifecycleMarkers(t *testing.T) {
	f := setupSelfBondFixture(t)
	hooks := NewStakingHooks(&f.keeper)
	valAddr := sdk.ValAddress(bytes.Repeat([]byte{0x11}, 20))

	require.NoError(t, hooks.AfterValidatorCreated(f.ctx, valAddr))
	changed, err := f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, hooks.AfterValidatorRemoved(f.ctx, nil, valAddr))
	changed, err = f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
	require.NoError(t, err)
	require.False(t, changed)

	require.NoError(t, hooks.AfterValidatorBonded(f.ctx, nil, valAddr))
	changed, err = f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, hooks.AfterValidatorBeginUnbonding(f.ctx, nil, valAddr))
	changed, err = f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
	require.NoError(t, err)
	require.False(t, changed)

	require.NoError(t, hooks.BeforeValidatorSlashed(f.ctx, valAddr, math.LegacyZeroDec()))
	changed, err = f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
	require.NoError(t, err)
	require.True(t, changed)
}

func TestStakingHooksNoopCallbacks(t *testing.T) {
	f := setupSelfBondFixture(t)
	hooks := NewStakingHooks(&f.keeper)
	valAddr := sdk.ValAddress(bytes.Repeat([]byte{0x12}, 20))
	delAddr := sdk.AccAddress(bytes.Repeat([]byte{0x13}, 20))

	require.NoError(t, hooks.BeforeValidatorModified(f.ctx, valAddr))
	require.NoError(t, hooks.BeforeDelegationCreated(f.ctx, delAddr, valAddr))
	require.NoError(t, hooks.BeforeDelegationSharesModified(f.ctx, delAddr, valAddr))
	require.NoError(t, hooks.AfterUnbondingInitiated(f.ctx, 1))
}

func TestStakingHooksBeforeDelegationRemoved(t *testing.T) {
	f := setupSelfBondFixture(t)
	hooks := NewStakingHooks(&f.keeper)

	valAddr := sdk.ValAddress(bytes.Repeat([]byte{0x21}, 20))
	nonSelfDelAddr := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))

	err := hooks.BeforeDelegationRemoved(f.ctx, nonSelfDelAddr, valAddr)
	require.NoError(t, err)

	err = hooks.BeforeDelegationRemoved(f.ctx, sdk.AccAddress(valAddr), valAddr)
	require.Error(t, err)
}

func TestStakingHooksAfterDelegationModified(t *testing.T) {
	tests := []struct {
		name          string
		withParams    bool
		selfBond      int64
		isSelf        bool
		shouldErr     bool
		shouldMarkVal bool
	}{
		{"non-self delegation is ignored", true, 5, false, false, false},
		{"self delegation below minimum fails", true, 5, true, true, false},
		{"self delegation at minimum passes and marks changed", true, 10, true, false, true},
		{"fails when params are missing", false, 10, true, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f selfBondTestFixture
			if tc.withParams {
				f = setupSelfBondFixture(t)
			} else {
				f = setupSelfBondFixtureWithoutParams(t)
			}

			hooks := NewStakingHooks(&f.keeper)
			valAddr, _, _ := f.addValidatorWithSelfBond(t, 0x31, stakingtypes.Bonded, tc.selfBond)

			delAddr := sdk.AccAddress(bytes.Repeat([]byte{0x32}, 20))
			if tc.isSelf {
				delAddr = sdk.AccAddress(valAddr)
			}

			err := hooks.AfterDelegationModified(f.ctx, delAddr, valAddr)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			marked, markErr := f.keeper.changedValidators.Has(f.ctx, []byte(valAddr))
			require.NoError(t, markErr)
			require.Equal(t, tc.shouldMarkVal, marked)
		})
	}
}
