package keeper

import (
	"testing"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/stretchr/testify/require"
)

func TestTaskState(t *testing.T) {
	f := setupKeeperFixture(t)

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    " BTC/USD ",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}))
	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "ETH/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   false,
	}))

	task, err := f.keeper.GetTask(f.ctx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "BTC/USD", task.GetSymbol())

	active, err := f.keeper.ListTasks(f.ctx, true)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "BTC/USD", active[0].GetSymbol())

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   false,
	}))
	updated, err := f.keeper.GetTask(f.ctx, " BTC/USD ")
	require.NoError(t, err)
	require.False(t, updated.GetEnabled())

	require.NoError(t, f.keeper.RemoveTask(f.ctx, " BTC/USD "))
	_, err = f.keeper.GetTask(f.ctx, "BTC/USD")
	require.Error(t, err)
}

func TestApplyOracleValuesUpdatesLatestAndBoundsHistory(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.SetParams(f.ctx, &oraclev1.Params{
		Enabled:       true,
		MinValidators: 1,
		MinSources:    3,
		HistoryLimit:  2,
	}))

	values := []*oraclev1.OracleValue{
		testValue("BTC/USD", "1.0", 10),
		testValue("BTC/USD", "2.0", 11),
		testValue("BTC/USD", "3.0", 12),
	}
	require.NoError(t, f.keeper.ApplyOracleValues(f.ctx, values))

	latest, err := f.keeper.GetLatestValue(f.ctx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "3.0", latest.GetValue())

	history, err := f.keeper.GetHistory(f.ctx, "BTC/USD")
	require.NoError(t, err)
	require.Len(t, history.GetValues(), 2)
	require.Equal(t, int64(11), history.GetValues()[0].GetBlockHeight())
	require.Equal(t, int64(12), history.GetValues()[1].GetBlockHeight())
}

func TestTaskStateRejectsInvalidTasks(t *testing.T) {
	f := setupKeeperFixture(t)

	require.Error(t, f.keeper.SetTask(f.ctx, nil))
	require.Error(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    " ",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}))
	require.Error(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED,
		Enabled:   true,
	}))
	require.Error(t, f.keeper.RemoveTask(f.ctx, " "))
}

func TestQueryServerReturnsOracleState(t *testing.T) {
	f := setupKeeperFixture(t)
	queryServer := NewQueryServer(&f.keeper)

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}))
	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "ETH/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   false,
	}))
	require.NoError(t, f.keeper.ApplyOracleValues(f.ctx, []*oraclev1.OracleValue{
		testValue("BTC/USD", "1.0", 10),
		testValue("BTC/USD", "2.0", 11),
	}))

	paramsResp, err := queryServer.Params(f.ctx, &oraclev1.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, DefaultParams().GetEnabled(), paramsResp.GetParams().GetEnabled())
	require.Equal(t, DefaultParams().GetMinValidators(), paramsResp.GetParams().GetMinValidators())
	require.Equal(t, DefaultParams().GetMinSources(), paramsResp.GetParams().GetMinSources())
	require.Equal(t, DefaultParams().GetHistoryLimit(), paramsResp.GetParams().GetHistoryLimit())

	tasksResp, err := queryServer.ActiveTasks(f.ctx, &oraclev1.QueryActiveTasksRequest{})
	require.NoError(t, err)
	require.Len(t, tasksResp.GetTasks(), 1)
	require.Equal(t, "BTC/USD", tasksResp.GetTasks()[0].GetSymbol())

	taskResp, err := queryServer.Task(f.ctx, &oraclev1.QueryTaskRequest{Symbol: " BTC/USD "})
	require.NoError(t, err)
	require.Equal(t, "BTC/USD", taskResp.GetTask().GetSymbol())

	latestResp, err := queryServer.LatestValue(f.ctx, &oraclev1.QueryLatestValueRequest{Symbol: "BTC/USD"})
	require.NoError(t, err)
	require.Equal(t, "2.0", latestResp.GetValue().GetValue())

	allLatestResp, err := queryServer.LatestValues(f.ctx, &oraclev1.QueryLatestValuesRequest{})
	require.NoError(t, err)
	require.Len(t, allLatestResp.GetValues(), 1)
	require.Equal(t, "BTC/USD", allLatestResp.GetValues()[0].GetSymbol())

	historyResp, err := queryServer.History(f.ctx, &oraclev1.QueryHistoryRequest{Symbol: "BTC/USD"})
	require.NoError(t, err)
	require.Len(t, historyResp.GetHistory().GetValues(), 2)
	require.Equal(t, int64(10), historyResp.GetHistory().GetValues()[0].GetBlockHeight())
	require.Equal(t, int64(11), historyResp.GetHistory().GetValues()[1].GetBlockHeight())
}

func testValue(symbol string, value string, height int64) *oraclev1.OracleValue {
	return &oraclev1.OracleValue{
		Symbol:        symbol,
		ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:         value,
		BlockHeight:   height,
		BlockTimeUnix: height * 10,
	}
}
