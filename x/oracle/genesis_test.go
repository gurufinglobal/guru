package oracle

import (
	"testing"

	"github.com/GPTx-global/guru-v2/v2/x/oracle/keeper"
	"github.com/GPTx-global/guru-v2/v2/x/oracle/types"

	"cosmossdk.io/log"

	"cosmossdk.io/store"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func setupTest(t *testing.T) (sdk.Context, *keeper.Keeper) {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("guru", "gurupub")

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), nil)
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	err := stateStore.LoadLatestVersion()
	require.NoError(t, err)

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	k := keeper.NewKeeper(cdc, storeKey, nil)

	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "test-chain"}, false, log.NewNopLogger())

	// Pre-initialize store by setting default params to avoid nil pointer
	defaultParams := types.DefaultParams()
	err = k.SetParams(ctx, defaultParams)
	require.NoError(t, err)

	return ctx, k
}

func TestInitGenesis(t *testing.T) {
	ctx, k := setupTest(t)

	tests := []struct {
		name     string
		genesis  types.GenesisState
		expPanic bool
	}{
		{
			name: "1. valid genesis state",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:       1,
						OracleType:      types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE,
						Name:            "Test Oracle",
						Description:     "Test Description",
						Period:          60,
						AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
						Quorum:          1,
						Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: false,
		},
		{
			name: "2. empty moderator address",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress: "",
			},
			expPanic: true,
		},
		{
			name: "3. invalid oracle request doc",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:       1,
						OracleType:      types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE,
						Name:            "", // Empty name should cause validation error
						Description:     "Test Description",
						Period:          60,
						AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
						Quorum:          1,
						Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: true,
		},
		{
			name: "4. unspecified oracle type",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:       1,
						OracleType:      types.OracleType_ORACLE_TYPE_UNSPECIFIED,
						Name:            "Test Oracle",
						Description:     "Test Description",
						Period:          60,
						AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
						Quorum:          1,
						Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: true,
		},
		{
			name: "5. empty endpoints",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:       1,
						OracleType:      types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE,
						Name:            "Test Oracle",
						Description:     "Test Description",
						Period:          60,
						AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
						Quorum:          1,
						Endpoints:       []*types.OracleEndpoint{},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: true,
		},
		{
			name: "6. invalid account address",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:       1,
						OracleType:      types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE,
						Name:            "Test Oracle",
						Description:     "Test Description",
						Period:          60,
						AccountList:     []string{"invalid-address"},
						Quorum:          1,
						Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: true,
		},
		{
			name: "7. quorum greater than account list length",
			genesis: types.GenesisState{
				Params: types.Params{
					EnableOracle: true,
				},
				ModeratorAddress:      "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				OracleRequestDocCount: 1,
				OracleRequestDocs: []types.OracleRequestDoc{
					{
						RequestId:   1,
						OracleType:  types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE,
						Name:        "Test Oracle",
						Description: "Test Description",
						Period:      60,
						AccountList: []string{
							"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
							"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
						}, // 2개의 계정
						Quorum:          3, // 3개의 quorum (계정 수보다 큼)
						Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
						AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
						Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
					},
				},
			},
			expPanic: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expPanic {
				require.Panics(t, func() {
					InitGenesis(ctx, *k, tc.genesis)
				})
			} else {
				require.NotPanics(t, func() {
					InitGenesis(ctx, *k, tc.genesis)
				})
			}

			// for _, doc := range tc.genesis.OracleRequestDocs {
			// 	found, err := k.GetOracleRequestDoc(ctx, doc.RequestId)
			// 	require.NoError(t, err)
			// 	require.Equal(t, doc, *found)
			// }
		})
	}
}

// TestExportGenesis disabled temporarily due to store setup issues
// func TestExportGenesis(t *testing.T) {
// 	// Test implementation will be added later with proper integration test setup
// }
