package keeper

import (
	"bytes"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	"github.com/stretchr/testify/require"
)

func TestEndBlockerFailsWithoutParams(t *testing.T) {
	f := setupSelfBondFixtureWithoutParams(t)

	err := f.keeper.EndBlocker(f.ctx)
	require.Error(t, err)
}

func TestEndBlockerFullScanIterateError(t *testing.T) {
	f := setupSelfBondFixture(t)
	f.stakingKeeper.iterateErr = errors.New("iterate failed")
	require.NoError(t, f.keeper.SetEnforceAllBonded(f.ctx, true))

	err := f.keeper.EndBlocker(f.ctx)
	require.Error(t, err)
}

func TestEndBlockerFullScanInvalidValidatorAddress(t *testing.T) {
	f := setupSelfBondFixture(t)
	f.stakingKeeper.iterateValidators = []stakingtypes.Validator{
		{
			OperatorAddress: "invalid-validator-address",
			Status:          stakingtypes.Bonded,
		},
	}
	require.NoError(t, f.keeper.SetEnforceAllBonded(f.ctx, true))

	err := f.keeper.EndBlocker(f.ctx)
	require.Error(t, err)
}

func TestEndBlockerChangedPathBeginUnbondingError(t *testing.T) {
	f := setupSelfBondFixture(t)
	valAddr, _, _ := f.addValidatorWithSelfBond(t, 0x11, stakingtypes.Bonded, 9)
	require.NoError(t, f.keeper.MarkValidatorChanged(f.ctx, valAddr))

	f.stakingKeeper.beginUnbondingErr = errors.New("begin unbonding failed")
	err := f.keeper.EndBlocker(f.ctx)
	require.Error(t, err)
}

func TestEndBlockerChangedPathIsDeterministic(t *testing.T) {
	f := setupSelfBondFixture(t)
	valAddrB, opAddrB, _ := f.addValidatorWithSelfBond(t, 0x22, stakingtypes.Bonded, 9)
	valAddrA, opAddrA, _ := f.addValidatorWithSelfBond(t, 0x11, stakingtypes.Bonded, 9)

	require.NoError(t, f.keeper.MarkValidatorChanged(f.ctx, valAddrB))
	require.NoError(t, f.keeper.MarkValidatorChanged(f.ctx, valAddrA))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	require.Equal(t, []string{opAddrA, opAddrB}, f.stakingKeeper.beginUnbondingCalled)
}

func TestCollectChangedBondedValidatorsSkipsMissingValidator(t *testing.T) {
	f := setupSelfBondFixture(t)

	missingValAddr := sdk.ValAddress(bytes.Repeat([]byte{0x99}, 20))
	require.NoError(t, f.keeper.MarkValidatorChanged(f.ctx, missingValAddr))

	validators, err := f.keeper.collectChangedBondedValidatorsBelowMinSelfBond(f.ctx, mustInt(t, "10"))
	require.NoError(t, err)
	require.Empty(t, validators)
}

func TestBeginUnbondingValidatorsSkipsAlreadyUnbonding(t *testing.T) {
	f := setupSelfBondFixture(t)
	valAddr, _, _ := f.addValidatorWithSelfBond(t, 0x33, stakingtypes.Unbonding, 9)

	err := f.keeper.beginUnbondingValidators(f.ctx, mustInt(t, "10"), []sdk.ValAddress{valAddr})
	require.NoError(t, err)
	require.Empty(t, f.stakingKeeper.beginUnbondingCalled)
}

func TestCloneValAddress(t *testing.T) {
	original := []byte{1, 2, 3}
	cloned := cloneValAddress(original)

	require.Equal(t, []byte{1, 2, 3}, []byte(cloned))
	cloned[0] = 9
	require.Equal(t, []byte{1, 2, 3}, original)
}

func TestMsgUpdateParamsTriggersSameBlockUnbonding(t *testing.T) {
	f := setupSelfBondFixture(t)
	valAddr, operatorAddress, _ := f.addValidatorWithSelfBond(t, 0x44, stakingtypes.Bonded, 10)

	msgServer := NewMsgServer(&f.keeper)
	_, err := msgServer.UpdateParams(f.ctx, &constitutionv1.MsgUpdateParams{
		Authority: f.keeper.authority.String(),
		Params:    testParams("11"),
	})
	require.NoError(t, err)

	enforceAll, err := f.keeper.ShouldEnforceAllBonded(f.ctx)
	require.NoError(t, err)
	require.True(t, enforceAll)

	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, []string{operatorAddress}, f.stakingKeeper.beginUnbondingCalled)

	enforceAll, err = f.keeper.ShouldEnforceAllBonded(f.ctx)
	require.NoError(t, err)
	require.False(t, enforceAll)

	validator := f.stakingKeeper.validators[string(valAddr)]
	require.True(t, validator.IsUnbonding())
}
