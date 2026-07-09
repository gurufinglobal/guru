package keeper

import (
	"context"
	"fmt"
	"testing"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/stretchr/testify/require"
)

func TestTaskState(t *testing.T) {
	f := setupKeeperFixture(t)

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             " btc/usd ",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}))
	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "ETH/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            false,
		SubmissionInterval: 1,
	}))

	task, err := f.keeper.GetTask(f.ctx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, "BTC/USD", task.GetSymbol())

	active, err := f.keeper.ListTasks(f.ctx, true)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "BTC/USD", active[0].GetSymbol())

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            false,
		SubmissionInterval: 1,
	}))
	updated, err := f.keeper.GetTask(f.ctx, " BTC/USD ")
	require.NoError(t, err)
	require.False(t, updated.GetEnabled())

	require.NoError(t, f.keeper.RemoveTask(f.ctx, " BTC/USD "))
	_, err = f.keeper.GetTask(f.ctx, "BTC/USD")
	require.Error(t, err)
}

func TestTaskScheduleIndexesDueTasksByHeight(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(0)

	require.NoError(t, f.keeper.SetTask(ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 2,
	}))
	require.NoError(t, f.keeper.SetTask(ctx, &oraclev1.OracleTask{
		Symbol:             "ETH/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 3,
	}))

	due, err := f.keeper.DueTasks(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, due)

	due, err = f.keeper.DueTasks(ctx, 2)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, "BTC/USD", due[0].GetSymbol())

	due, err = f.keeper.DueTasks(ctx, 3)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, "ETH/USD", due[0].GetSymbol())

	require.NoError(t, f.keeper.AdvanceTaskSchedule(ctx, 2))
	due, err = f.keeper.DueTasks(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, due)
	due, err = f.keeper.DueTasks(ctx, 4)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, "BTC/USD", due[0].GetSymbol())

	require.NoError(t, f.keeper.SetTask(f.ctx.WithBlockHeight(4), &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            false,
		SubmissionInterval: 2,
	}))
	due, err = f.keeper.DueTasks(ctx, 4)
	require.NoError(t, err)
	require.Empty(t, due)

	require.NoError(t, f.keeper.RemoveTask(ctx, "ETH/USD"))
	due, err = f.keeper.DueTasks(ctx, 3)
	require.NoError(t, err)
	require.Empty(t, due)
}

func TestDueTasksForVoteExtensionIncludesExactAndMissedButExcludesPipelineHeight(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(0)

	for _, symbol := range []string{"BTC/USD", "ETH/USD", "ATOM/USD"} {
		require.NoError(t, f.keeper.SetTask(ctx, &oraclev1.OracleTask{
			Symbol:             symbol,
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 5,
		}))
		require.NoError(t, f.keeper.removeScheduledTask(ctx, symbol))
	}
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "BTC/USD", 3))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "BTC/USD", 10))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "ETH/USD", 9))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "ATOM/USD", 10))

	due, err := f.keeper.DueTasksForVoteExtension(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"ATOM/USD", "BTC/USD"}, taskSymbols(due))
}

func TestAdvanceTaskScheduleDedupeUsesMaxDueHeightAndKeepsScheduleWindow(t *testing.T) {
	f := setupKeeperFixture(t)
	ctx := f.ctx.WithBlockHeight(10)

	require.NoError(t, f.keeper.SetTask(ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 5,
	}))
	require.NoError(t, f.keeper.removeScheduledTask(ctx, "BTC/USD"))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "BTC/USD", 3))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "BTC/USD", 9))
	require.NoError(t, f.keeper.scheduleTaskAt(ctx, "BTC/USD", 10))

	require.NoError(t, f.keeper.AdvanceTaskSchedule(ctx, 10))

	heights, err := f.keeper.scheduledHeightsForSymbol(ctx, "BTC/USD")
	require.NoError(t, err)
	require.Equal(t, []int64{9, 15}, heights)
}

func TestApplyOracleValuesUpdatesLatestAndBoundsHistory(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.SetParams(f.ctx, &oraclev1.Params{
		MinValidators: 1,
		MinSources:    3,
		HistoryLimit:  2,
	}))

	values := []*oraclev1.OracleValue{
		testValue(" btc/usd ", "1.0", 10),
		testValue("BTC/USD", "2.0", 11),
		testValue("btc/usd", "3.0", 12),
	}
	require.NoError(t, f.keeper.ApplyOracleValues(f.ctx, values))

	latest, err := f.keeper.GetLatestValue(f.ctx, " btc/usd ")
	require.NoError(t, err)
	require.Equal(t, "BTC/USD", latest.GetSymbol())
	require.Equal(t, "3.0", latest.GetValue())

	history, err := f.keeper.GetHistory(f.ctx, "btc/usd")
	require.NoError(t, err)
	require.Len(t, history.GetValues(), 2)
	require.Equal(t, "BTC/USD", history.GetSymbol())
	require.Equal(t, int64(11), history.GetValues()[0].GetBlockHeight())
	require.Equal(t, int64(12), history.GetValues()[1].GetBlockHeight())
}

func TestApplyOracleValuesCallsHooksWithTaskSubmissionInterval(t *testing.T) {
	f := setupKeeperFixture(t)
	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "TRX/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 7,
	}))

	hook := &recordingOracleHook{}
	f.keeper.SetHooks(hook)

	require.NoError(t, f.keeper.ApplyOracleValues(f.ctx, []*oraclev1.OracleValue{
		testValue("trx/usd", "1.0", 10),
	}))
	require.Len(t, hook.values, 1)
	require.Equal(t, "TRX/USD", hook.values[0].GetSymbol())
	require.Equal(t, []uint32{7}, hook.sourceSubmissionIntervals)
}

func TestTaskStateRejectsInvalidTasks(t *testing.T) {
	f := setupKeeperFixture(t)

	require.Error(t, f.keeper.SetTask(f.ctx, nil))
	require.Error(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             " ",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}))
	require.Error(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED,
		Enabled:            true,
		SubmissionInterval: 1,
	}))
	require.ErrorContains(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_STRING,
		Enabled:            true,
		SubmissionInterval: 1,
	}), "non-numeric value_type is not supported")
	require.Error(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}))
	require.Error(t, f.keeper.RemoveTask(f.ctx, " "))
}

func TestApplyOracleValuesRejectsNonNumeric(t *testing.T) {
	f := setupKeeperFixture(t)

	require.ErrorContains(t, f.keeper.ApplyOracleValues(f.ctx, []*oraclev1.OracleValue{{
		Symbol:        "BTC/USD",
		ValueType:     oraclev1.ValueType_VALUE_TYPE_BOOL,
		Value:         "true",
		BlockHeight:   10,
		BlockTimeUnix: 100,
	}}), "non-numeric value_type is not supported")
}

func TestQueryServerReturnsOracleState(t *testing.T) {
	f := setupKeeperFixture(t)
	queryServer := NewQueryServer(&f.keeper)

	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}))
	require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
		Symbol:             "ETH/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            false,
		SubmissionInterval: 1,
	}))
	require.NoError(t, f.keeper.ApplyOracleValues(f.ctx, []*oraclev1.OracleValue{
		testValue("BTC/USD", "1.0", 10),
		testValue("BTC/USD", "2.0", 11),
	}))

	paramsResp, err := queryServer.Params(f.ctx, &oraclev1.QueryParamsRequest{})
	require.NoError(t, err)
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

func TestQueryServerPaginatesLargeResponses(t *testing.T) {
	f := setupKeeperFixture(t)
	queryServer := NewQueryServer(&f.keeper)

	require.NoError(t, f.keeper.SetParams(f.ctx, &oraclev1.Params{
		MinValidators: 1,
		MinSources:    3,
		HistoryLimit:  100,
	}))

	historyValues := make([]*oraclev1.OracleValue, 0, 35)
	for i := 0; i < 35; i++ {
		symbol := fmt.Sprintf("ASSET%02d/USD", i)
		require.NoError(t, f.keeper.SetTask(f.ctx, &oraclev1.OracleTask{
			Symbol:             symbol,
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		}))
		require.NoError(t, f.keeper.SetLatestValue(f.ctx, testValue(symbol, fmt.Sprintf("%d.0", i), int64(i))))
		historyValues = append(historyValues, testValue("BTC/USD", fmt.Sprintf("%d.0", i), int64(i)))
	}
	require.NoError(t, f.keeper.SetHistory(f.ctx, &oraclev1.OracleHistory{Symbol: "BTC/USD", Values: historyValues}, 100))

	tasksResp, err := queryServer.ActiveTasks(f.ctx, &oraclev1.QueryActiveTasksRequest{})
	require.NoError(t, err)
	require.Len(t, tasksResp.GetTasks(), int(DefaultQueryPageLimit))
	require.Equal(t, uint64(35), tasksResp.GetPagination().GetTotal())
	require.Equal(t, []byte("30"), tasksResp.GetPagination().GetNextKey())

	nextTasksResp, err := queryServer.ActiveTasks(f.ctx, &oraclev1.QueryActiveTasksRequest{
		Pagination: &queryv1beta1.PageRequest{
			Key:   tasksResp.GetPagination().GetNextKey(),
			Limit: 3,
		},
	})
	require.NoError(t, err)
	require.Len(t, nextTasksResp.GetTasks(), 3)
	require.Equal(t, "ASSET30/USD", nextTasksResp.GetTasks()[0].GetSymbol())
	require.Equal(t, []byte("33"), nextTasksResp.GetPagination().GetNextKey())

	latestResp, err := queryServer.LatestValues(f.ctx, &oraclev1.QueryLatestValuesRequest{
		Pagination: &queryv1beta1.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, latestResp.GetValues(), 10)
	require.Equal(t, uint64(35), latestResp.GetPagination().GetTotal())
	require.Equal(t, []byte("10"), latestResp.GetPagination().GetNextKey())

	historyResp, err := queryServer.History(f.ctx, &oraclev1.QueryHistoryRequest{Symbol: "BTC/USD"})
	require.NoError(t, err)
	require.Len(t, historyResp.GetHistory().GetValues(), int(DefaultQueryPageLimit))
	require.Equal(t, uint64(35), historyResp.GetPagination().GetTotal())
	require.Equal(t, []byte("30"), historyResp.GetPagination().GetNextKey())

	historyTailResp, err := queryServer.History(f.ctx, &oraclev1.QueryHistoryRequest{
		Symbol:     "BTC/USD",
		Pagination: &queryv1beta1.PageRequest{Offset: 30, Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, historyTailResp.GetHistory().GetValues(), 5)
	require.Empty(t, historyTailResp.GetPagination().GetNextKey())
	require.Equal(t, int64(30), historyTailResp.GetHistory().GetValues()[0].GetBlockHeight())
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

func taskSymbols(tasks []*oraclev1.OracleTask) []string {
	symbols := make([]string, 0, len(tasks))
	for _, task := range tasks {
		symbols = append(symbols, task.GetSymbol())
	}
	return symbols
}

type recordingOracleHook struct {
	values                    []*oraclev1.OracleValue
	sourceSubmissionIntervals []uint32
}

func (h *recordingOracleHook) AfterOracleValueApplied(_ context.Context, value *oraclev1.OracleValue, sourceSubmissionInterval uint32) error {
	h.values = append(h.values, value)
	h.sourceSubmissionIntervals = append(h.sourceSubmissionIntervals, sourceSubmissionInterval)
	return nil
}
