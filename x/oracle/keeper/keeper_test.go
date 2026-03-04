package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	tmdb "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

// setupKeeper builds an in-memory keeper/context for unit tests.
func setupKeeper(t *testing.T) (*Keeper, sdk.Context) {
	t.Helper()

	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("guru", "gurupub")

	storeKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	// minimal account keeper stub: we only need GetAccount for signature paths,
	// but unit tests here avoid signature verification, so pass nil safely.
	var accountKeeper oracletypes.AccountKeeper

	k := NewKeeper(cdc, storeKey, "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft", accountKeeper)

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	// preset params to avoid nil defaults
	require.NoError(t, k.SetParams(ctx, oracletypes.DefaultParams()))

	// Chain behavior: proto-defined categories are always present at init.
	for _, cat := range oracletypes.ProtoDefinedCategories() {
		k.SetCategory(ctx, cat)
	}

	return k, ctx
}

// newAddr returns a random bech32 address using the configured prefix.
func newAddr() string {
	priv := secp256k1.GenPrivKey()
	return sdk.AccAddress(priv.PubKey().Address()).String()
}
