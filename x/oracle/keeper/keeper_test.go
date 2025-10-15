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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GPTx-global/guru-v2/v2/x/oracle/types"
)

// setupKeeper creates a new Keeper instance and context for testing
func setupKeeper(t *testing.T) (*Keeper, sdk.Context) {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("guru", "gurupub")

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	err := stateStore.LoadLatestVersion()
	require.NoError(t, err)

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	keeper := NewKeeper(cdc, storeKey, "cosmos1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft")

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	// Pre-initialize store by setting default params to avoid nil pointer
	defaultParams := types.DefaultParams()
	err = keeper.SetParams(ctx, defaultParams)
	require.NoError(t, err)

	return keeper, ctx
}

// TestSetAndGetOracleRequestDocCount tests the setting and getting of oracle request document count
func TestSetAndGetOracleRequestDocCount(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	// Initial count should be 0
	initialCount := keeper.GetOracleRequestDocCount(ctx)
	assert.Equal(t, uint64(0), initialCount)

	// Set count
	testCount := uint64(42)
	keeper.SetOracleRequestDocCount(ctx, testCount)

	// Verify the set count
	retrievedCount := keeper.GetOracleRequestDocCount(ctx)
	assert.Equal(t, testCount, retrievedCount)
}

// TestSetAndGetOracleRequestDoc tests the setting and getting of oracle request documents
func TestSetAndGetOracleRequestDoc(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	// Create test document
	doc := types.OracleRequestDoc{
		RequestId:   1,
		Name:        "Test Name",
		Description: "Test Description",
		OracleType:  types.OracleType_ORACLE_TYPE_CRYPTO,
		Status:      types.RequestStatus_REQUEST_STATUS_ENABLED,
		AccountList: []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
		Quorum:      1,
		Period:      1,
		Endpoints: []*types.OracleEndpoint{
			{Url: "https://api.coinbase.com/v2/prices/BTC-USD/spot", ParseRule: "data.amount"},
		},
		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
	}

	// Store document
	keeper.SetOracleRequestDoc(ctx, doc)

	// Retrieve document
	retrievedDoc, err := keeper.GetOracleRequestDoc(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, doc.RequestId, retrievedDoc.RequestId)
	assert.Equal(t, doc.Name, retrievedDoc.Name)
	assert.Equal(t, doc.Description, retrievedDoc.Description)
	assert.Equal(t, doc.OracleType, retrievedDoc.OracleType)
	assert.Equal(t, doc.Status, retrievedDoc.Status)
	assert.Equal(t, doc.AccountList, retrievedDoc.AccountList)
	assert.Equal(t, doc.Quorum, retrievedDoc.Quorum)
	assert.Equal(t, doc.Period, retrievedDoc.Period)
	assert.Equal(t, doc.Endpoints, retrievedDoc.Endpoints)
	assert.Equal(t, doc.AggregationRule, retrievedDoc.AggregationRule)

	// Test retrieval of non-existent document
	_, err = keeper.GetOracleRequestDoc(ctx, 999)
	assert.Error(t, err)
}

// TestSetAndGetModeratorAddress tests the setting and getting of moderator address
func TestSetAndGetModeratorAddress(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	// Initial address should be empty string
	initialAddress := keeper.GetModeratorAddress(ctx)
	assert.Equal(t, "", initialAddress)

	// Set address
	testAddress := "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"
	keeper.SetModeratorAddress(ctx, testAddress)

	// Verify the set address
	retrievedAddress := keeper.GetModeratorAddress(ctx)
	assert.Equal(t, testAddress, retrievedAddress)
}

// TestGetOracleData tests the retrieval of oracle data
func TestGetOracleData(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	doc := types.OracleRequestDoc{
		RequestId:   1,
		Name:        "Test Name",
		Description: "Test Description",
		OracleType:  types.OracleType_ORACLE_TYPE_CRYPTO,
		Status:      types.RequestStatus_REQUEST_STATUS_ENABLED,
		AccountList: []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
		Quorum:      1,
		Period:      1,
		Endpoints: []*types.OracleEndpoint{
			{Url: "https://api.coinbase.com/v2/prices/BTC-USD/spot", ParseRule: "data.amount"},
		},
		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
		Nonce:           1,
	}

	// Store document
	keeper.SetOracleRequestDoc(ctx, doc)

	dataSet := types.DataSet{
		RequestId:   1,
		Nonce:       1,
		RawData:     "100",
		BlockHeight: 1,
		BlockTime:   1,
	}

	keeper.SetDataSet(ctx, dataSet)

	query := types.QueryOracleDataRequest{
		RequestId: 1,
	}

	response, err := keeper.GetOracleData(ctx, query.RequestId)
	require.NoError(t, err)
	assert.Equal(t, dataSet.RequestId, response.DataSet.RequestId)
	assert.Equal(t, dataSet.Nonce, response.DataSet.Nonce)
	assert.Equal(t, dataSet.RawData, response.DataSet.RawData)
	assert.Equal(t, dataSet.BlockHeight, response.DataSet.BlockHeight)
	assert.Equal(t, dataSet.BlockTime, response.DataSet.BlockTime)
}
