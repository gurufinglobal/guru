package app

import (
	"testing"

	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	pruningtypes "github.com/cosmos/cosmos-sdk/store/v2/pruning/types"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	"github.com/stretchr/testify/require"
)

func TestV1UpgradeWiringAddsBEXAndTranswapStores(t *testing.T) {
	t.Setenv(envEnableUpgradeHandlerV1, "")
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.AppOptionsMap{
		flags.FlagHome: t.TempDir(),
	})
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	require.True(t, testApp.UpgradeKeeper.HasHandler(upgradeNameV1))
	require.Equal(t, []string{bextypes.StoreKey, transwaptypes.StoreKey}, storeUpgradesForPlan(upgradeNameV1).Added)
	require.Nil(t, storeUpgradesForPlan("unknown"))
}

func TestV1UpgradeHandlerRemainsDisabledForOldBinaryMode(t *testing.T) {
	t.Setenv(envEnableUpgradeHandlerV1, "0")
	testApp := NewApp(log.NewNopLogger(), dbm.NewMemDB(), false, simtestutil.AppOptionsMap{
		flags.FlagHome: t.TempDir(),
	})
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	require.False(t, testApp.UpgradeKeeper.HasHandler(upgradeNameV1))
}

func TestV1StoreLoaderAddsBEXAndTranswapToLegacyDatabase(t *testing.T) {
	const legacyStoreName = "legacy"
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	legacyStore := rootmulti.NewStore(db, logger)
	legacyStore.SetPruning(pruningtypes.NewPruningOptions(pruningtypes.PruningNothing))
	legacyKey := storetypes.NewKVStoreKey(legacyStoreName)
	legacyStore.MountStoreWithDB(legacyKey, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, legacyStore.LoadLatestVersion())
	legacyKV, ok := legacyStore.GetStore(legacyKey).(storetypes.KVStore)
	require.True(t, ok)
	legacyKV.Set([]byte("key"), []byte("value"))
	require.Equal(t, int64(1), legacyStore.Commit().Version)

	upgradedApp := baseapp.NewBaseApp(t.Name(), logger, db, nil)
	upgradedApp.MountStores(
		storetypes.NewKVStoreKey(legacyStoreName),
		storetypes.NewKVStoreKey(bextypes.StoreKey),
		storetypes.NewKVStoreKey(transwaptypes.StoreKey),
	)
	upgradedApp.SetStoreLoader(upgradetypes.UpgradeStoreLoader(2, storeUpgradesForPlan(upgradeNameV1)))
	require.NoError(t, upgradedApp.LoadLatestVersion())
	require.Equal(t, int64(1), upgradedApp.LastBlockHeight())
	_, err := upgradedApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2})
	require.NoError(t, err)
	_, err = upgradedApp.Commit()
	require.NoError(t, err)
	require.NoError(t, upgradedApp.Close())

	restartedApp := baseapp.NewBaseApp(t.Name(), logger, db, nil)
	restartedApp.MountStores(
		storetypes.NewKVStoreKey(legacyStoreName),
		storetypes.NewKVStoreKey(bextypes.StoreKey),
		storetypes.NewKVStoreKey(transwaptypes.StoreKey),
	)
	require.NoError(t, restartedApp.LoadLatestVersion())
	require.Equal(t, int64(2), restartedApp.LastBlockHeight())
	require.NoError(t, restartedApp.Close())
}
