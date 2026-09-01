package constitution

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate1To2DoesNotCreatePendingMinGasPriceSchedule(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_migration_test")
	testCtx := testutil.DefaultContextWithDB(t, storeKey, transientKey)
	store := testCtx.Ctx.KVStore(storeKey)

	legacyKey := []byte{0x01, 0x01}
	legacyValue := []byte("preserved-v1-state")
	store.Set(legacyKey, legacyValue)

	require.NoError(t, migrate1To2(testCtx.Ctx))
	require.Equal(t, legacyValue, store.Get(legacyKey))

	iterator := storetypes.KVStorePrefixIterator(store, constitutiontypes.MinGasPriceKey)
	defer iterator.Close()
	require.False(t, iterator.Valid(), "the no-op v1-to-v2 migration must not synthesize a pending schedule")
}
