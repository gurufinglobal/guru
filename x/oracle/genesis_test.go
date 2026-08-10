package oracle

import (
	"context"
	"testing"

	"cosmossdk.io/core/appmodule"
	coregenesis "cosmossdk.io/core/genesis"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestValidateGenesisRejectsDuplicateTaskSymbols(t *testing.T) {
	err := (AppModule{}).validateGenesisState(&oracletypes.GenesisState{
		Params: oraclekeeper.DefaultParams(),
		Tasks: []*oracletypes.OracleTask{
			{
				Symbol:             "BTC/USD",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
			{
				Symbol:             " btc/usd ",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
		},
	})
	require.Error(t, err)
}

func TestInitGenesisAndExportGenesisPreservesTaskSchedule(t *testing.T) {
	ctx, keeper := setupGenesisKeeper(t)
	ctx = ctx.WithBlockHeight(100)
	module := NewAppModule(keeper)

	source := genesisSourceFromState(t, &oracletypes.GenesisState{
		Params: oraclekeeper.DefaultParams(),
		Tasks: []*oracletypes.OracleTask{
			genesisTask("BTC/USD", true, 5),
			genesisTask("ETH/USD", true, 5),
		},
		TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{
			{Symbol: "BTC/USD", Height: 8},
			{Symbol: "BTC/USD", Height: 13},
			{Symbol: "ETH/USD", Height: 10},
			{Symbol: "ETH/USD", Height: 15},
		},
		LatestValues: []*oracletypes.OracleValue{
			genesisValue("BTC/USD", "10.0", 8),
			genesisValue("ETH/USD", "20.0", 10),
		},
		History: []*oracletypes.OracleHistory{
			{Symbol: "BTC/USD", Values: []*oracletypes.OracleValue{genesisValue("BTC/USD", "10.0", 8)}},
			{Symbol: "ETH/USD", Values: []*oracletypes.OracleValue{genesisValue("ETH/USD", "20.0", 10)}},
		},
	})
	require.NoError(t, module.InitGenesis(ctx, source))

	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, module.ExportGenesis(ctx, target.Target()))
	raw, err := target.JSON()
	require.NoError(t, err)
	exportedSource, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	exported, err := readGenesisState(exportedSource, module.defaultGenesisState())
	require.NoError(t, err)

	require.Equal(t, []*oracletypes.OracleTaskScheduleEntry{
		{Symbol: "BTC/USD", Height: 8},
		{Symbol: "ETH/USD", Height: 10},
		{Symbol: "BTC/USD", Height: 13},
		{Symbol: "ETH/USD", Height: 15},
	}, exported.GetTaskSchedule())
	require.Equal(t, []*oracletypes.OracleValue{
		genesisValue("BTC/USD", "10.0", 8),
		genesisValue("ETH/USD", "20.0", 10),
	}, exported.GetLatestValues())
	require.Equal(t, []*oracletypes.OracleHistory{
		{Symbol: "BTC/USD", Values: []*oracletypes.OracleValue{genesisValue("BTC/USD", "10.0", 8)}},
		{Symbol: "ETH/USD", Values: []*oracletypes.OracleValue{genesisValue("ETH/USD", "20.0", 10)}},
	}, exported.GetHistory())
}

func TestReadGenesisStateAcceptsCanonicalProtoJSON(t *testing.T) {
	source, err := coregenesis.SourceFromRawJSON([]byte(`{
		"params":{"min_validators":1,"min_sources":3,"history_limit":100},
		"tasks":[{"symbol":"BTC/USD","value_type":"VALUE_TYPE_NUMERIC","enabled":true,"submission_interval":5}],
		"task_schedule":[{"symbol":"BTC/USD","height":"8"}],
		"latest_values":[{"symbol":"BTC/USD","value_type":"VALUE_TYPE_NUMERIC","value":"10.0","block_height":"8","block_time_unix":"80"}],
		"history":[{"symbol":"BTC/USD","values":[{"symbol":"BTC/USD","value_type":"VALUE_TYPE_NUMERIC","value":"10.0","block_height":"8","block_time_unix":"80"}]}]
	}`))
	require.NoError(t, err)

	genesis, err := readGenesisState(source, (&AppModule{}).defaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, oracletypes.ValueType_VALUE_TYPE_NUMERIC, genesis.GetTasks()[0].GetValueType())
	require.Equal(t, int64(8), genesis.GetTaskSchedule()[0].GetHeight())
	require.Equal(t, int64(80), genesis.GetLatestValues()[0].GetBlockTimeUnix())
}

func TestValidateGenesisRejectsInvalidTaskSchedule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		genesis *oracletypes.GenesisState
	}{
		{
			name: "non-positive height",
			genesis: &oracletypes.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 0}},
			},
		},
		{
			name: "duplicate normalized symbol and height",
			genesis: &oracletypes.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{
					{Symbol: "BTC/USD", Height: 8},
					{Symbol: " btc/usd ", Height: 8},
				},
			},
		},
		{
			name: "missing task",
			genesis: &oracletypes.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{{Symbol: "ETH/USD", Height: 8}},
			},
		},
		{
			name: "disabled task",
			genesis: &oracletypes.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oracletypes.OracleTask{genesisTask("BTC/USD", false, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 8}},
			},
		},
		{
			name: "enabled task missing schedule",
			genesis: &oracletypes.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
			},
		},
		{
			name: "enabled task partial schedule window",
			genesis: &oracletypes.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 8}},
			},
		},
		{
			name: "schedule phase mismatch",
			genesis: &oracletypes.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oracletypes.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oracletypes.OracleTaskScheduleEntry{
					{Symbol: "BTC/USD", Height: 8},
					{Symbol: "BTC/USD", Height: 14},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, (AppModule{}).validateGenesisState(tc.genesis))
		})
	}
}

func setupGenesisKeeper(t *testing.T) (sdk.Context, oraclekeeper.Keeper) {
	t.Helper()

	key := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_oracle_genesis_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	keeper := oraclekeeper.NewKeeper(
		runtime.NewKVStoreService(key),
		codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
		evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		genesisConstitutionKeeper{},
	)

	return testCtx.Ctx, keeper
}

func genesisSourceFromState(t *testing.T, state *oracletypes.GenesisState) appmodule.GenesisSource {
	t.Helper()

	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, writeGenesisState(target.Target(), state))
	raw, err := target.JSON()
	require.NoError(t, err)
	source, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	return source
}

func genesisTask(symbol string, enabled bool, interval uint32) *oracletypes.OracleTask {
	return &oracletypes.OracleTask{
		Symbol:             symbol,
		ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            enabled,
		SubmissionInterval: interval,
	}
}

func genesisValue(symbol, value string, height int64) *oracletypes.OracleValue {
	return &oracletypes.OracleValue{
		Symbol:      symbol,
		ValueType:   oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:       value,
		BlockHeight: height,
	}
}

type genesisConstitutionKeeper struct{}

func (genesisConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	return "", nil
}
