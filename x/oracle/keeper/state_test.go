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

func testValue(symbol string, value string, height int64) *oraclev1.OracleValue {
	return &oraclev1.OracleValue{
		Symbol:        symbol,
		ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Value:         value,
		BlockHeight:   height,
		BlockTimeUnix: height * 10,
	}
}
