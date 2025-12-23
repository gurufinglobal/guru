package keeper

import (
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"

	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
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
	var accountKeeper oracletypes.AccountKeeper = nil

	k := NewKeeper(cdc, storeKey, "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft", accountKeeper)

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	// preset params to avoid nil defaults
	require.NoError(t, k.SetParams(ctx, oracletypes.DefaultParams()))

	return k, ctx
}

// newAddr returns a random bech32 address using the configured prefix.
func newAddr() string {
	priv := secp256k1.GenPrivKey()
	return sdk.AccAddress(priv.PubKey().Address()).String()
}

