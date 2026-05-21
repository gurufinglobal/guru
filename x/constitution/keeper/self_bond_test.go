package keeper

import (
	"bytes"
	"context"
	"sort"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

type selfBondTestFixture struct {
	ctx           sdk.Context
	keeper        Keeper
	stakingKeeper *mockStakingKeeper
	accountCodec  address.Codec
	valCodec      address.Codec
}

type mockStakingKeeper struct {
	valCodec             address.Codec
	validators           map[string]stakingtypes.Validator
	delegations          map[string]stakingtypes.Delegation
	beginUnbondingCalled []string
	iterateValidators    []stakingtypes.Validator

	getValidatorErr      error
	getValidatorErrByKey map[string]error

	getDelegationErr      error
	getDelegationErrByKey map[string]error

	beginUnbondingErr error
	iterateErr        error
}

func newMockStakingKeeper(valCodec address.Codec) *mockStakingKeeper {
	return &mockStakingKeeper{
		valCodec:              valCodec,
		validators:            make(map[string]stakingtypes.Validator),
		delegations:           make(map[string]stakingtypes.Delegation),
		getValidatorErrByKey:  make(map[string]error),
		getDelegationErrByKey: make(map[string]error),
	}
}

func (m *mockStakingKeeper) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	if err := m.getValidatorErr; err != nil {
		return stakingtypes.Validator{}, err
	}
	if err, ok := m.getValidatorErrByKey[string(addr)]; ok {
		return stakingtypes.Validator{}, err
	}

	validator, ok := m.validators[string(addr)]
	if !ok {
		return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
	}

	return validator, nil
}

func (m *mockStakingKeeper) BeginUnbondingValidator(_ context.Context, validator stakingtypes.Validator) (stakingtypes.Validator, error) {
	if m.beginUnbondingErr != nil {
		return stakingtypes.Validator{}, m.beginUnbondingErr
	}

	valAddr, err := m.valCodec.StringToBytes(validator.GetOperator())
	if err != nil {
		return stakingtypes.Validator{}, err
	}

	validator.Status = stakingtypes.Unbonding
	m.validators[string(valAddr)] = validator
	m.beginUnbondingCalled = append(m.beginUnbondingCalled, validator.GetOperator())
	return validator, nil
}

func (m *mockStakingKeeper) GetDelegation(_ context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error) {
	if err := m.getDelegationErr; err != nil {
		return stakingtypes.Delegation{}, err
	}

	key := m.delegationKey(delAddr, valAddr)
	if err, ok := m.getDelegationErrByKey[key]; ok {
		return stakingtypes.Delegation{}, err
	}

	delegation, ok := m.delegations[key]
	if !ok {
		return stakingtypes.Delegation{}, stakingtypes.ErrNoDelegation
	}

	return delegation, nil
}

func (m *mockStakingKeeper) IterateBondedValidatorsByPower(_ context.Context, fn func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error {
	if m.iterateErr != nil {
		return m.iterateErr
	}

	validators := m.iterateValidators
	if validators == nil {
		validators = make([]stakingtypes.Validator, 0, len(m.validators))
		for _, validator := range m.validators {
			validators = append(validators, validator)
		}
	}
	sort.Slice(validators, func(i, j int) bool {
		return validators[i].GetOperator() < validators[j].GetOperator()
	})

	i := int64(0)
	for _, validator := range validators {
		if !validator.IsBonded() {
			continue
		}

		if stop := fn(int64(i), validator); stop {
			break
		}
		i++
	}

	return nil
}

func (m *mockStakingKeeper) ValidatorAddressCodec() address.Codec {
	return m.valCodec
}

func (m *mockStakingKeeper) delegationKey(delAddr sdk.AccAddress, valAddr sdk.ValAddress) string {
	return string(delAddr) + "|" + string(valAddr)
}

type testTx struct {
	msgs []sdk.Msg
}

func (tx testTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx testTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func setupSelfBondFixture(t *testing.T) selfBondTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_constitution"))

	valCodec := evmaddress.NewEvmCodec("gurvaloper")
	accountCodec := evmaddress.NewEvmCodec("gur")
	stakingKeeper := newMockStakingKeeper(valCodec)

	authority := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	keeper := NewKeeper(authority, stakingKeeper, runtime.NewKVStoreService(key))
	require.NoError(t, keeper.SetParams(testCtx.Ctx, testParams("10")))

	return selfBondTestFixture{
		ctx:           testCtx.Ctx,
		keeper:        keeper,
		stakingKeeper: stakingKeeper,
		accountCodec:  accountCodec,
		valCodec:      valCodec,
	}
}

func setupSelfBondFixtureWithoutParams(t *testing.T) selfBondTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_constitution"))

	valCodec := evmaddress.NewEvmCodec("gurvaloper")
	accountCodec := evmaddress.NewEvmCodec("gur")
	stakingKeeper := newMockStakingKeeper(valCodec)

	authority := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	keeper := NewKeeper(authority, stakingKeeper, runtime.NewKVStoreService(key))

	return selfBondTestFixture{
		ctx:           testCtx.Ctx,
		keeper:        keeper,
		stakingKeeper: stakingKeeper,
		accountCodec:  accountCodec,
		valCodec:      valCodec,
	}
}

func (f selfBondTestFixture) addValidatorWithSelfBond(
	t *testing.T,
	seed byte,
	status stakingtypes.BondStatus,
	selfBond int64,
) (sdk.ValAddress, string, string) {
	t.Helper()

	valAddr := sdk.ValAddress(bytes.Repeat([]byte{seed}, 20))
	validatorAddress, err := f.valCodec.BytesToString(valAddr)
	require.NoError(t, err)
	delegatorAddress, err := f.accountCodec.BytesToString(valAddr)
	require.NoError(t, err)

	shares := math.LegacyNewDec(selfBond)
	f.stakingKeeper.validators[string(valAddr)] = stakingtypes.Validator{
		OperatorAddress: validatorAddress,
		Status:          status,
		Tokens:          math.NewInt(selfBond),
		DelegatorShares: shares,
	}
	f.stakingKeeper.delegations[f.stakingKeeper.delegationKey(sdk.AccAddress(valAddr), valAddr)] =
		stakingtypes.NewDelegation(delegatorAddress, validatorAddress, shares)

	return valAddr, validatorAddress, delegatorAddress
}

func testParams(amount string) *constitutionv1.Params {
	return &constitutionv1.Params{
		MinValidatorBondAmount: &basev1beta1.Coin{
			Denom:  appparams.BaseDenom,
			Amount: amount,
		},
	}
}

func TestMsgUpdateParamsSetsFullBondedScan(t *testing.T) {
	tests := []struct {
		name          string
		oldAmount     string
		newAmount     string
		shouldEnforce bool
	}{
		{"sets full scan when minimum increases", "10", "20", true},
		{"keeps full scan disabled when minimum unchanged", "10", "10", false},
		{"keeps full scan disabled when minimum decreases", "20", "10", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupSelfBondFixture(t)
			require.NoError(t, f.keeper.SetParams(f.ctx, testParams(tc.oldAmount)))

			msgServer := NewMsgServer(&f.keeper)
			_, err := msgServer.UpdateParams(f.ctx, &constitutionv1.MsgUpdateParams{
				Authority: f.keeper.authority.String(),
				Params:    testParams(tc.newAmount),
			})
			require.NoError(t, err)

			enforceAll, err := f.keeper.ShouldEnforceAllBonded(f.ctx)
			require.NoError(t, err)
			require.Equal(t, tc.shouldEnforce, enforceAll)
		})
	}
}

func TestEndBlockerSelfBondEnforcement(t *testing.T) {
	tests := []struct {
		name         string
		status       stakingtypes.BondStatus
		selfBond     int64
		minBond      string
		markChanged  bool
		enforceAll   bool
		shouldUnbond bool
	}{
		{"unbonds changed bonded validator below minimum", stakingtypes.Bonded, 9, "10", true, false, true},
		{"keeps changed bonded validator above minimum", stakingtypes.Bonded, 11, "10", true, false, false},
		{"skips non-bonded validator below minimum", stakingtypes.Unbonding, 9, "10", true, false, false},
		{"unbonds on full scan when minimum increased", stakingtypes.Bonded, 9, "10", false, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupSelfBondFixture(t)
			require.NoError(t, f.keeper.SetParams(f.ctx, testParams(tc.minBond)))

			valAddr, _, _ := f.addValidatorWithSelfBond(t, 0x11, tc.status, tc.selfBond)

			if tc.markChanged {
				require.NoError(t, f.keeper.MarkValidatorChanged(f.ctx, valAddr))
			}
			if tc.enforceAll {
				require.NoError(t, f.keeper.SetEnforceAllBonded(f.ctx, true))
			}

			run := func() { require.NoError(t, f.keeper.EndBlocker(f.ctx)) }
			require.NotPanics(t, run)

			if tc.shouldUnbond {
				require.Len(t, f.stakingKeeper.beginUnbondingCalled, 1)
			} else {
				require.Empty(t, f.stakingKeeper.beginUnbondingCalled)
			}

			changedIter, err := f.keeper.changedValidators.Iterate(f.ctx, nil)
			require.NoError(t, err)
			defer changedIter.Close()
			changed, err := changedIter.Keys()
			require.NoError(t, err)
			require.Empty(t, changed)
		})
	}
}

func TestValidateTxSelfBondConstraints(t *testing.T) {
	f := setupSelfBondFixture(t)
	require.NoError(t, f.keeper.SetParams(f.ctx, testParams("100")))

	_, validatorAddress, selfDelegatorAddress := f.addValidatorWithSelfBond(t, 0x22, stakingtypes.Bonded, 120)
	otherDelegatorAddress, err := f.accountCodec.BytesToString(bytes.Repeat([]byte{0x33}, 20))
	require.NoError(t, err)

	undelegateSelf := &stakingtypes.MsgUndelegate{
		DelegatorAddress: selfDelegatorAddress,
		ValidatorAddress: validatorAddress,
		Amount: sdk.Coin{
			Denom:  appparams.BaseDenom,
			Amount: math.NewInt(30),
		},
	}

	execMsg := authztypes.NewMsgExec(sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20)), []sdk.Msg{undelegateSelf})

	tests := []struct {
		name      string
		msg       sdk.Msg
		shouldErr bool
	}{
		{
			name: "rejects create validator below minimum self-bond",
			msg: &stakingtypes.MsgCreateValidator{
				ValidatorAddress: validatorAddress,
				Value: sdk.Coin{
					Denom:  appparams.BaseDenom,
					Amount: math.NewInt(99),
				},
			},
			shouldErr: true,
		},
		{
			name: "allows create validator at minimum self-bond",
			msg: &stakingtypes.MsgCreateValidator{
				ValidatorAddress: validatorAddress,
				Value: sdk.Coin{
					Denom:  appparams.BaseDenom,
					Amount: math.NewInt(100),
				},
			},
			shouldErr: false,
		},
		{
			name:      "rejects undelegate when self-bond drops below minimum",
			msg:       undelegateSelf,
			shouldErr: true,
		},
		{
			name: "allows undelegate from non-self delegator",
			msg: &stakingtypes.MsgUndelegate{
				DelegatorAddress: otherDelegatorAddress,
				ValidatorAddress: validatorAddress,
				Amount: sdk.Coin{
					Denom:  appparams.BaseDenom,
					Amount: math.NewInt(30),
				},
			},
			shouldErr: false,
		},
		{
			name:      "rejects nested authz execute message",
			msg:       &execMsg,
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx{msgs: []sdk.Msg{tc.msg}}
			err := f.keeper.ValidateTxSelfBondConstraints(f.ctx, tx, f.accountCodec)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
