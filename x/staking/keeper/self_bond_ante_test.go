package keeper

import (
	"bytes"
	"context"
	"errors"
	"testing"

	coreaddress "cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/gurufinglobal/guru/v2/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	protov2 "google.golang.org/protobuf/proto"
)

type mockMinValidatorBondSource struct {
	minBond sdkmath.Int
	err     error
}

func (m mockMinValidatorBondSource) GetMinValidatorBondAmount(context.Context) (sdkmath.Int, error) {
	if m.err != nil {
		return sdkmath.Int{}, m.err
	}

	return m.minBond, nil
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

type anteKeeperFixture struct {
	ctx                  sdk.Context
	keeper               *Keeper
	accountCodec         coreaddress.Codec
	valCodec             coreaddress.Codec
	validatorAddr        sdk.ValAddress
	validatorAddress     string
	selfDelegatorAddress string
	storeService         corestore.KVStoreService
}

func mustInt(t *testing.T, amount string) sdkmath.Int {
	t.Helper()

	value, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		t.Fatalf("failed to parse int: %s", amount)
	}

	return value
}

func mustAnyWithValue(t *testing.T, msg sdk.Msg) *codectypes.Any {
	t.Helper()

	any, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		t.Fatalf("failed to pack message into Any: %v", err)
	}

	return any
}

func setupAnteKeeperFixture(t *testing.T, minBond string) anteKeeperFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(stakingtypes.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_staking_test"))

	accountCodec := evmaddress.NewEvmCodec(config.Bech32PrefixAccAddr)
	valCodec := evmaddress.NewEvmCodec(config.Bech32PrefixValAddr)
	consCodec := evmaddress.NewEvmCodec(config.Bech32PrefixConsAddr)

	ctrl := gomock.NewController(t)
	accountKeeper := stakingtestutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(stakingtypes.BondedPoolName).
		Return(authtypes.NewEmptyModuleAccount(stakingtypes.BondedPoolName).GetAddress()).
		AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(stakingtypes.NotBondedPoolName).
		Return(authtypes.NewEmptyModuleAccount(stakingtypes.NotBondedPoolName).GetAddress()).
		AnyTimes()
	accountKeeper.EXPECT().AddressCodec().Return(accountCodec).AnyTimes()

	bankKeeper := stakingtestutil.NewMockBankKeeper(ctrl)
	authority, err := accountCodec.BytesToString(authtypes.NewModuleAddress(govtypes.ModuleName))
	require.NoError(t, err)

	encCfg := moduletestutil.MakeTestEncodingConfig()
	baseKeeper := stakingkeeper.NewKeeper(
		encCfg.Codec,
		storeService,
		accountKeeper,
		bankKeeper,
		authority,
		valCodec,
		consCodec,
	)
	require.NoError(t, baseKeeper.SetParams(testCtx.Ctx, stakingtypes.DefaultParams()))

	customKeeper := &Keeper{
		Keeper:        baseKeeper,
		minBondSource: mockMinValidatorBondSource{minBond: mustInt(t, minBond)},
		accountCodec:  accountCodec,
	}

	validatorAddr := sdk.ValAddress(bytes.Repeat([]byte{0x11}, 20))
	validatorAddress, err := valCodec.BytesToString(validatorAddr)
	require.NoError(t, err)
	delegatorAddress, err := accountCodec.BytesToString(validatorAddr)
	require.NoError(t, err)

	shares := sdkmath.LegacyNewDec(120)
	validator := stakingtypes.Validator{
		OperatorAddress: validatorAddress,
		Status:          stakingtypes.Bonded,
		Tokens:          mustInt(t, "120"),
		DelegatorShares: shares,
	}
	require.NoError(t, customKeeper.SetValidator(testCtx.Ctx, validator))
	require.NoError(t, customKeeper.SetDelegation(testCtx.Ctx, stakingtypes.NewDelegation(delegatorAddress, validatorAddress, shares)))

	return anteKeeperFixture{
		ctx:                  testCtx.Ctx,
		keeper:               customKeeper,
		accountCodec:         accountCodec,
		valCodec:             valCodec,
		validatorAddr:        validatorAddr,
		validatorAddress:     validatorAddress,
		selfDelegatorAddress: delegatorAddress,
		storeService:         storeService,
	}
}

func TestValidateTxSelfBondConstraints(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")

	otherDelegatorAddress, err := f.accountCodec.BytesToString(bytes.Repeat([]byte{0x22}, 20))
	require.NoError(t, err)
	otherValidatorAddress, err := f.valCodec.BytesToString(bytes.Repeat([]byte{0x44}, 20))
	require.NoError(t, err)

	tests := []struct {
		name      string
		tx        sdk.Tx
		shouldErr bool
	}{
		{"nil tx ignored", nil, false},
		{
			name: "rejects create validator below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: f.validatorAddress,
					Value:            sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "99")},
				},
			}},
			shouldErr: true,
		},
		{
			name: "allows create validator at minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: f.validatorAddress,
					Value:            sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "100")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows self delegate",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgDelegate{
					DelegatorAddress: f.selfDelegatorAddress,
					ValidatorAddress: f.validatorAddress,
					Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "1")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows self undelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: f.selfDelegatorAddress,
					ValidatorAddress: f.validatorAddress,
					Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows self begin redelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgBeginRedelegate{
					DelegatorAddress:    f.selfDelegatorAddress,
					ValidatorSrcAddress: f.validatorAddress,
					ValidatorDstAddress: otherValidatorAddress,
					Amount:              sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows self cancel unbonding delegation below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCancelUnbondingDelegation{
					DelegatorAddress: f.selfDelegatorAddress,
					ValidatorAddress: f.validatorAddress,
					Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
					CreationHeight:   1,
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows non-self undelegate",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: otherDelegatorAddress,
					ValidatorAddress: f.validatorAddress,
					Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows nested authz undelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgUndelegate{
							DelegatorAddress: f.selfDelegatorAddress,
							ValidatorAddress: f.validatorAddress,
							Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
						}),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows nested authz begin redelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgBeginRedelegate{
							DelegatorAddress:    f.selfDelegatorAddress,
							ValidatorSrcAddress: f.validatorAddress,
							ValidatorDstAddress: otherValidatorAddress,
							Amount:              sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "30")},
						}),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "rejects nested authz create validator below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgCreateValidator{
							ValidatorAddress: f.validatorAddress,
							Value:            sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "99")},
						}),
					},
				},
			}},
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.keeper.ValidateTxSelfBondConstraints(f.ctx, tc.tx)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateTxSelfBondConstraintsLoadsMinBondOnlyForBaseCreateValidator(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")
	f.keeper.minBondSource = mockMinValidatorBondSource{err: errors.New("min bond loaded")}

	tests := []struct {
		name      string
		tx        sdk.Tx
		shouldErr bool
	}{
		{
			name: "self delegate does not load min bond",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgDelegate{
					DelegatorAddress: f.selfDelegatorAddress,
					ValidatorAddress: f.validatorAddress,
					Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "1")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "non-base create validator does not load min bond",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: f.validatorAddress,
					Value:            sdk.Coin{Denom: "stake", Amount: mustInt(t, "1")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "nested non-create message does not load min bond",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgUndelegate{
							DelegatorAddress: f.selfDelegatorAddress,
							ValidatorAddress: f.validatorAddress,
							Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "1")},
						}),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "base create validator loads min bond",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: f.validatorAddress,
					Value:            sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "100")},
				},
			}},
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.keeper.ValidateTxSelfBondConstraints(f.ctx, tc.tx)
			if tc.shouldErr {
				require.Error(t, err)
				require.ErrorContains(t, err, "min bond loaded")
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateTxSelfBondConstraintsHandlesMissingValidator(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")

	missingValAddr := sdk.ValAddress(bytes.Repeat([]byte{0x33}, 20))
	validatorAddress, err := f.valCodec.BytesToString(missingValAddr)
	require.NoError(t, err)
	delegatorAddress, err := f.accountCodec.BytesToString(missingValAddr)
	require.NoError(t, err)

	tx := testTx{msgs: []sdk.Msg{
		&stakingtypes.MsgUndelegate{
			DelegatorAddress: delegatorAddress,
			ValidatorAddress: validatorAddress,
			Amount:           sdk.Coin{Denom: config.BaseDenom, Amount: mustInt(t, "1")},
		},
	}}

	require.NoError(t, f.keeper.ValidateTxSelfBondConstraints(f.ctx, tx))
}

func TestGetValidatorSelfBond(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")

	selfBond, err := f.keeper.GetValidatorSelfBond(f.ctx, f.validatorAddr)
	require.NoError(t, err)
	require.Equal(t, "120", selfBond.String())
}

func TestGetValidatorSelfBondReturnsZeroWhenNoDelegation(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")

	delegation, err := f.keeper.GetDelegation(f.ctx, sdk.AccAddress(f.validatorAddr), f.validatorAddr)
	require.NoError(t, err)
	require.NoError(t, f.keeper.RemoveDelegation(f.ctx, delegation))

	selfBond, err := f.keeper.GetValidatorSelfBond(f.ctx, f.validatorAddr)
	require.NoError(t, err)
	require.True(t, selfBond.IsZero())
}

func TestGetValidatorSelfBondPropagatesDelegationError(t *testing.T) {
	f := setupAnteKeeperFixture(t, "100")

	store := f.storeService.OpenKVStore(f.ctx)
	err := store.Set(stakingtypes.GetDelegationKey(sdk.AccAddress(f.validatorAddr), f.validatorAddr), []byte("not-a-valid-delegation"))
	require.NoError(t, err)

	_, err = f.keeper.GetValidatorSelfBond(f.ctx, f.validatorAddr)
	require.Error(t, err)
}
