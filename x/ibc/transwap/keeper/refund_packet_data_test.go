package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	tmdb "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"

	bextypes "github.com/gurufinglobal/guru/v2/x/bex/types"
	transtypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

type refundBexKeeperMock struct{}

func (refundBexKeeperMock) GetExchange(ctx sdk.Context, exchangeID math.Int) (*bextypes.Exchange, error) {
	return nil, nil
}

func (refundBexKeeperMock) AddCollectedFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	return nil
}

func (refundBexKeeperMock) DeductCollectedFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	return nil
}

func (refundBexKeeperMock) LockExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	return nil
}

func (refundBexKeeperMock) ReleaseExchangeFees(ctx sdk.Context, exchangeID string, fees sdk.Coins) error {
	return nil
}

func setupRefundKeeper(t *testing.T) (Keeper, sdk.Context) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(transtypes.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	k := Keeper{
		storeService: runtime.NewKVStoreService(storeKey),
		cdc:          cdc,
		BexKeeper:    refundBexKeeperMock{},
	}

	return k, ctx
}

func TestRefundPacketDataKey(t *testing.T) {
	key := RefundPacketDataKey(transtypes.PortID, "channel-7", 42)
	require.Equal(t, "refund/transwap/channel-7/42", key)
}

func TestRefundPacketData_IsolatedBySequenceForSameReceiver(t *testing.T) {
	k, ctx := setupRefundKeeper(t)

	receiver := "same-receiver"
	token := transtypes.Token{Denom: transtypes.NewDenom("uatom"), Amount: "100"}

	packet1 := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-0",
		token,
		"reserve-address-1",
		receiver,
		"memo-1",
		1,
		sdk.NewInt64Coin("uatom", 1),
		"1",
	)
	packet2 := transtypes.NewTransferPacketData(
		transtypes.PortID,
		"channel-0",
		token,
		"reserve-address-2",
		receiver,
		"memo-2",
		2,
		sdk.NewInt64Coin("uatom", 2),
		"2",
	)

	key1 := RefundPacketDataKey(transtypes.PortID, "channel-0", 1)
	key2 := RefundPacketDataKey(transtypes.PortID, "channel-0", 2)

	require.NoError(t, k.SetRefundPacketData(ctx, key1, &packet1))
	require.NoError(t, k.SetRefundPacketData(ctx, key2, &packet2))

	got1, err := k.GetRefundPacketData(ctx, key1)
	require.NoError(t, err)
	require.Equal(t, packet1.ExchangeId, got1.ExchangeId)
	require.Equal(t, packet1.Sender, got1.Sender)
	require.Equal(t, packet1.Receiver, got1.Receiver)
	require.Equal(t, packet1.Fee, got1.Fee)

	got2, err := k.GetRefundPacketData(ctx, key2)
	require.NoError(t, err)
	require.Equal(t, packet2.ExchangeId, got2.ExchangeId)
	require.Equal(t, packet2.Sender, got2.Sender)
	require.Equal(t, packet2.Receiver, got2.Receiver)
	require.Equal(t, packet2.Fee, got2.Fee)

	// deleting one sequence entry must not affect the other sequence entry
	k.DeleteRefundPacketData(ctx, key1)

	_, err = k.GetRefundPacketData(ctx, key1)
	require.Error(t, err)

	stillPresent, err := k.GetRefundPacketData(ctx, key2)
	require.NoError(t, err)
	require.Equal(t, packet2.ExchangeId, stillPresent.ExchangeId)
}
