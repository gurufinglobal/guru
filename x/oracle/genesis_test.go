package oracle

import (
	"context"
	"testing"

	"cosmossdk.io/core/appmodule"
	coregenesis "cosmossdk.io/core/genesis"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestValidateGenesisRejectsDuplicateTaskSymbols(t *testing.T) {
	err := (AppModule{}).validateGenesisState(&oraclev1.GenesisState{
		Params: oraclekeeper.DefaultParams(),
		Tasks: []*oraclev1.OracleTask{
			{
				Symbol:             "BTC/USD",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
			{
				Symbol:             " btc/usd ",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
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

	source := genesisSourceFromState(t, &oraclev1.GenesisState{
		Params: oraclekeeper.DefaultParams(),
		Tasks: []*oraclev1.OracleTask{
			genesisTask("BTC/USD", true, 5),
			genesisTask("ETH/USD", true, 5),
		},
		TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{
			{Symbol: "BTC/USD", Height: 8},
			{Symbol: "BTC/USD", Height: 13},
			{Symbol: "ETH/USD", Height: 10},
			{Symbol: "ETH/USD", Height: 15},
		},
		LatestValues: []*oraclev1.OracleValue{
			genesisValue("BTC/USD", "10.0", 8),
			genesisValue("ETH/USD", "20.0", 10),
		},
		History: []*oraclev1.OracleHistory{
			{Symbol: "BTC/USD", Values: []*oraclev1.OracleValue{genesisValue("BTC/USD", "10.0", 8)}},
			{Symbol: "ETH/USD", Values: []*oraclev1.OracleValue{genesisValue("ETH/USD", "20.0", 10)}},
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

	require.Equal(t, []*oraclev1.OracleTaskScheduleEntry{
		{Symbol: "BTC/USD", Height: 8},
		{Symbol: "ETH/USD", Height: 10},
		{Symbol: "BTC/USD", Height: 13},
		{Symbol: "ETH/USD", Height: 15},
	}, exported.GetTaskSchedule())
	require.Equal(t, []*oraclev1.OracleValue{
		genesisValue("BTC/USD", "10.0", 8),
		genesisValue("ETH/USD", "20.0", 10),
	}, exported.GetLatestValues())
	require.Equal(t, []*oraclev1.OracleHistory{
		{Symbol: "BTC/USD", Values: []*oraclev1.OracleValue{genesisValue("BTC/USD", "10.0", 8)}},
		{Symbol: "ETH/USD", Values: []*oraclev1.OracleValue{genesisValue("ETH/USD", "20.0", 10)}},
	}, exported.GetHistory())
}

func TestValidateGenesisRejectsInvalidTaskSchedule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		genesis *oraclev1.GenesisState
	}{
		{
			name: "non-positive height",
			genesis: &oraclev1.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 0}},
			},
		},
		{
			name: "duplicate normalized symbol and height",
			genesis: &oraclev1.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{
					{Symbol: "BTC/USD", Height: 8},
					{Symbol: " btc/usd ", Height: 8},
				},
			},
		},
		{
			name: "missing task",
			genesis: &oraclev1.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{{Symbol: "ETH/USD", Height: 8}},
			},
		},
		{
			name: "disabled task",
			genesis: &oraclev1.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oraclev1.OracleTask{genesisTask("BTC/USD", false, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 8}},
			},
		},
		{
			name: "enabled task missing schedule",
			genesis: &oraclev1.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
			},
		},
		{
			name: "enabled task partial schedule window",
			genesis: &oraclev1.GenesisState{
				Params:       oraclekeeper.DefaultParams(),
				Tasks:        []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{{Symbol: "BTC/USD", Height: 8}},
			},
		},
		{
			name: "schedule phase mismatch",
			genesis: &oraclev1.GenesisState{
				Params: oraclekeeper.DefaultParams(),
				Tasks:  []*oraclev1.OracleTask{genesisTask("BTC/USD", true, 5)},
				TaskSchedule: []*oraclev1.OracleTaskScheduleEntry{
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
		evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		genesisConstitutionKeeper{},
	)

	return testCtx.Ctx, keeper
}

func genesisSourceFromState(t *testing.T, state *oraclev1.GenesisState) appmodule.GenesisSource {
	t.Helper()

	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, writeGenesisState(target.Target(), state))
	raw, err := target.JSON()
	require.NoError(t, err)
	source, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	return source
}

func genesisTask(symbol string, enabled bool, interval uint32) *oraclev1.OracleTask {
	return &oraclev1.OracleTask{
		Symbol:             symbol,
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            enabled,
		SubmissionInterval: interval,
	}
}

func genesisValue(symbol, value string, height int64) *oraclev1.OracleValue {
	return &oraclev1.OracleValue{
		Symbol:      symbol,
		ValueType:   oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:       value,
		BlockHeight: height,
	}
}

type genesisConstitutionKeeper struct{}

func (genesisConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	return "", nil
}
