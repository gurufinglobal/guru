package keeper

import (
	"bytes"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

type keeperTestFixture struct {
	ctx    sdk.Context
	keeper Keeper
}

func setupKeeperFixture(t *testing.T) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	authority := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	keeper := NewKeeper(authority, runtime.NewKVStoreService(key))
	require.NoError(t, keeper.SetParams(testCtx.Ctx, testParams("10")))

	return keeperTestFixture{
		ctx:    testCtx.Ctx,
		keeper: keeper,
	}
}

func setupKeeperFixtureWithoutParams(t *testing.T) keeperTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	authority := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	keeper := NewKeeper(authority, runtime.NewKVStoreService(key))

	return keeperTestFixture{
		ctx:    testCtx.Ctx,
		keeper: keeper,
	}
}

func testParams(amount string) *constitutionv1.Params {
	return &constitutionv1.Params{
		MinValidatorBondAmount: &basev1beta1.Coin{
			Denom:  appparams.BaseDenom,
			Amount: amount,
		},
	}
}
