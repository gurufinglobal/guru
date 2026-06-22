package keeper

import (
	"bytes"
	"context"
	"testing"

	coreaddress "cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtestutil "github.com/cosmos/cosmos-sdk/x/staking/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type mutableMinValidatorBondSource struct {
	minBond sdkmath.Int
}

func (m *mutableMinValidatorBondSource) GetMinValidatorBondAmount(context.Context) (sdkmath.Int, error) {
	return m.minBond, nil
}

type validatorSetFixture struct {
	ctx           sdk.Context
	keeper        *Keeper
	minBondSource *mutableMinValidatorBondSource
	storeService  corestore.KVStoreService
	valAddrs      []sdk.ValAddress
}

func setupValidatorSetFixture(t *testing.T, minBondPower int64) validatorSetFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(stakingtypes.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutilDefaultContextWithDB(t, key)

	accountCodec := evmaddress.NewEvmCodec("gur")
	valCodec := evmaddress.NewEvmCodec("gurvaloper")
	consCodec := evmaddress.NewEvmCodec("gurvalcons")

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
	bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

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
	params := stakingtypes.DefaultParams()
	params.BondDenom = appparams.BaseDenom
	require.NoError(t, baseKeeper.SetParams(testCtx, params))

	minBondSource := &mutableMinValidatorBondSource{
		minBond: baseKeeper.TokensFromConsensusPower(testCtx, minBondPower),
	}
	customKeeper := NewKeeper(baseKeeper, minBondSource, accountCodec)

	pks := simtestutil.CreateTestPubKeys(3)
	valAddrs := []sdk.ValAddress{
		sdk.ValAddress(pks[0].Address().Bytes()),
		sdk.ValAddress(pks[1].Address().Bytes()),
		sdk.ValAddress(pks[2].Address().Bytes()),
	}

	for i, valAddr := range valAddrs {
		validator := newTestValidator(t, valCodec, valAddr, pks[i])
		require.NoError(t, customKeeper.SetValidator(testCtx, validator))
		require.NoError(t, customKeeper.SetValidatorByConsAddr(testCtx, validator))
	}

	return validatorSetFixture{
		ctx:           testCtx,
		keeper:        customKeeper,
		minBondSource: minBondSource,
		storeService:  storeService,
		valAddrs:      valAddrs,
	}
}

func newTestValidator(
	t *testing.T,
	valCodec coreaddress.Codec,
	valAddr sdk.ValAddress,
	pubKey cryptotypes.PubKey,
) stakingtypes.Validator {
	t.Helper()

	validatorAddress, err := valCodec.BytesToString(valAddr)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(validatorAddress, pubKey, stakingtypes.Description{})
	require.NoError(t, err)
	return validator
}

func sdktestutilDefaultContextWithDB(t *testing.T, key *storetypes.KVStoreKey) sdk.Context {
	t.Helper()

	return sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_validator_set_test")).Ctx
}

func (f validatorSetFixture) setValidatorWithDelegations(
	t *testing.T,
	valAddr sdk.ValAddress,
	status stakingtypes.BondStatus,
	selfBondPower int64,
	otherBondPower int64,
) stakingtypes.Validator {
	t.Helper()

	selfTokens := f.keeper.TokensFromConsensusPower(f.ctx, selfBondPower)
	otherTokens := f.keeper.TokensFromConsensusPower(f.ctx, otherBondPower)
	totalTokens := selfTokens.Add(otherTokens)
	totalShares := sdkmath.LegacyNewDecFromInt(totalTokens)

	validator, err := f.keeper.GetValidator(f.ctx, valAddr)
	require.NoError(t, err)
	validator = validator.UpdateStatus(status)
	validator.Tokens = totalTokens
	validator.DelegatorShares = totalShares

	f.deleteValidatorPowerIndexEntries(t, valAddr)
	require.NoError(t, f.keeper.SetValidator(f.ctx, validator))
	require.NoError(t, f.keeper.SetValidatorByPowerIndex(f.ctx, validator))

	validatorAddress, err := f.keeper.ValidatorAddressCodec().BytesToString(valAddr)
	require.NoError(t, err)
	selfDelegatorAddress, err := f.keeper.accountCodec.BytesToString(sdk.AccAddress(valAddr))
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetDelegation(
		f.ctx,
		stakingtypes.NewDelegation(selfDelegatorAddress, validatorAddress, sdkmath.LegacyNewDecFromInt(selfTokens)),
	))

	if !otherTokens.IsZero() {
		otherDelegatorAddress, err := f.keeper.accountCodec.BytesToString(sdk.AccAddress([]byte("other_delegator_addr")[:20]))
		require.NoError(t, err)
		require.NoError(t, f.keeper.SetDelegation(
			f.ctx,
			stakingtypes.NewDelegation(otherDelegatorAddress, validatorAddress, sdkmath.LegacyNewDecFromInt(otherTokens)),
		))
	}

	return validator
}

func (f validatorSetFixture) deleteValidatorPowerIndexEntries(t *testing.T, valAddr sdk.ValAddress) {
	t.Helper()

	store := f.storeService.OpenKVStore(f.ctx)
	iterator, err := store.Iterator(
		stakingtypes.ValidatorsByPowerIndexKey,
		storetypes.PrefixEndBytes(stakingtypes.ValidatorsByPowerIndexKey),
	)
	require.NoError(t, err)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		indexedValAddr := stakingtypes.ParseValidatorPowerRankKey(iterator.Key())
		if bytes.Equal(indexedValAddr, valAddr) {
			require.NoError(t, store.Delete(iterator.Key()))
		}
	}
}

func TestApplyAndReturnValidatorSetUpdatesExcludesBelowMinSelfBond(t *testing.T) {
	f := setupValidatorSetFixture(t, 100)
	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Unbonded, 90, 1000)

	updates, err := f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Empty(t, updates)

	lastValidators, err := f.keeper.GetLastValidators(f.ctx)
	require.NoError(t, err)
	require.Empty(t, lastValidators)

	validator, err := f.keeper.GetValidator(f.ctx, f.valAddrs[0])
	require.NoError(t, err)
	require.True(t, validator.IsUnbonded())
}

func TestApplyAndReturnValidatorSetUpdatesRemovesBondedValidatorBelowMinSelfBond(t *testing.T) {
	f := setupValidatorSetFixture(t, 100)
	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Unbonded, 120, 0)

	updates, err := f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Positive(t, updates[0].Power)

	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Bonded, 90, 0)
	updates, err = f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Zero(t, updates[0].Power)

	lastValidators, err := f.keeper.GetLastValidators(f.ctx)
	require.NoError(t, err)
	require.Empty(t, lastValidators)

	validator, err := f.keeper.GetValidator(f.ctx, f.valAddrs[0])
	require.NoError(t, err)
	require.True(t, validator.IsUnbonding())
}

func TestApplyAndReturnValidatorSetUpdatesAllowsRestoredSelfBondToReenter(t *testing.T) {
	f := setupValidatorSetFixture(t, 100)
	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Unbonded, 90, 1000)

	updates, err := f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Empty(t, updates)

	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Unbonded, 100, 1000)
	updates, err = f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Positive(t, updates[0].Power)

	lastValidators, err := f.keeper.GetLastValidators(f.ctx)
	require.NoError(t, err)
	require.Len(t, lastValidators, 1)
}

func TestApplyAndReturnValidatorSetUpdatesExcludesAfterMinBondIncrease(t *testing.T) {
	f := setupValidatorSetFixture(t, 100)
	f.setValidatorWithDelegations(t, f.valAddrs[0], stakingtypes.Unbonded, 120, 0)

	updates, err := f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Positive(t, updates[0].Power)

	f.minBondSource.minBond = f.keeper.TokensFromConsensusPower(f.ctx, 130)
	updates, err = f.keeper.ApplyAndReturnValidatorSetUpdates(f.ctx)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Zero(t, updates[0].Power)

	lastValidators, err := f.keeper.GetLastValidators(f.ctx)
	require.NoError(t, err)
	require.Empty(t, lastValidators)
}
