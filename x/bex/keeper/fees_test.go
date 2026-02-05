package keeper

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	tmdb "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/bex/types"
)

func setupFeeKeeper(t *testing.T) (Keeper, sdk.Context) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	k := Keeper{
		storeKey:      storeKey,
		moduleAddress: sdk.AccAddress(make([]byte, 20)),
	}

	return k, ctx
}

func TestAddCollectedFeesAndGetCollectedFees(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"
	fees := sdk.NewCoins(sdk.NewInt64Coin("denom", 10))

	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, fees))

	got, err := k.GetCollectedFees(ctx, exchangeID)
	require.NoError(t, err)
	require.Equal(t, fees.Sort(), got.Sort())
}

func TestLockMoreThanCollectedFails(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"
	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 10))))

	err := k.LockExchangeFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 11)))
	require.Error(t, err)
}

func TestGetAvailableFeesInvariantViolation(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"

	// Set collected=1, locked=2 directly to simulate corrupted state.
	require.NoError(t, k.setCollectedFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 1))))

	lockedStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyLockedFees)
	bz, err := json.Marshal(sdk.NewCoins(sdk.NewInt64Coin("denom", 2)))
	require.NoError(t, err)
	lockedStore.Set([]byte(exchangeID), bz)

	_, err = k.GetAvailableFees(ctx, exchangeID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invariant violation")
}

func TestDeductMoreThanCollectedFails(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"
	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 10))))

	err := k.DeductCollectedFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 11)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot deduct more than collected")
}

func TestGetAvailableFeesNormalFlow(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"

	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 10))))
	require.NoError(t, k.LockExchangeFees(ctx, exchangeID, sdk.NewCoins(sdk.NewInt64Coin("denom", 4))))

	available, err := k.GetAvailableFees(ctx, exchangeID)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("denom", 6)).Sort(), available.Sort())
}

func TestAddCollectedFeesMultiDenomSortAndMerge(t *testing.T) {
	k, ctx := setupFeeKeeper(t)

	exchangeID := "9"

	// Add in unsorted order to ensure keeper normalizes/sorts on write.
	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, sdk.NewCoins(
		sdk.NewInt64Coin("ubar", 7),
		sdk.NewInt64Coin("ufoo", 3),
	)))
	// Add again to ensure merging across same denom.
	require.NoError(t, k.AddCollectedFees(ctx, exchangeID, sdk.NewCoins(
		sdk.NewInt64Coin("ufoo", 5),
	)))

	got, err := k.GetCollectedFees(ctx, exchangeID)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("ubar", 7),
		sdk.NewInt64Coin("ufoo", 8),
	).Sort(), got.Sort())
}
