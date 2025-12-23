package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

func TestProcessScheduledTasks_RollOverAndReschedule(t *testing.T) {
	k, ctx := setupKeeper(t)

	req := types.OracleRequest{
		Id:       1,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "BTC",
		Count:    2,
		Period:   3,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    1,
	}
	k.SetRequest(ctx, req)

	// add a report/ count for previous nonce to ensure cleanup
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: newAddr(), RawData: "1", Nonce: 1, Signature: []byte{1}})

	// schedule current block height
	ctx = ctx.WithBlockHeight(10)
	k.ScheduleOracleTask(ctx, 10, req.Id)

	k.processScheduledTasks(ctx, 10)

	// Nonce advanced, count decreased
	updated, _ := k.GetRequest(ctx, 1)
	require.Equal(t, uint64(2), updated.Nonce)
	require.Equal(t, int64(1), updated.Count)
	require.Equal(t, types.Status_STATUS_ACTIVE, updated.Status)

	// previous reports removed
	require.Equal(t, uint64(0), k.GetReportCount(ctx, 1, 1))

	// rescheduled at next height
	next := k.GetScheduledTasks(ctx, 13)
	require.Len(t, next, 1)
	require.Equal(t, uint64(1), next[0])

	// event emitted with nonce
	evts := ctx.EventManager().Events()
	require.Len(t, evts, 1)
	attrs := evts[0].Attributes
	require.Equal(t, types.AttributeKeyRequestID, string(attrs[0].Key))
	require.Equal(t, "1", string(attrs[0].Value))
	require.Equal(t, types.AttributeKeyNonce, string(attrs[1].Key))
	require.Equal(t, "2", string(attrs[1].Value))
}

func TestProcessScheduledTasks_DeactivateWhenCountExhausted(t *testing.T) {
	k, ctx := setupKeeper(t)

	req := types.OracleRequest{
		Id:       2,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "ETH",
		Count:    1,
		Period:   4,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    5,
	}
	k.SetRequest(ctx, req)

	ctx = ctx.WithBlockHeight(20)
	k.ScheduleOracleTask(ctx, 20, req.Id)

	k.processScheduledTasks(ctx, 20)

	updated, _ := k.GetRequest(ctx, 2)
	require.Equal(t, uint64(6), updated.Nonce)
	require.Equal(t, int64(0), updated.Count)
	require.Equal(t, types.Status_STATUS_INACTIVE, updated.Status)

	// no reschedule when inactive
	next := k.GetScheduledTasks(ctx, 24)
	require.Len(t, next, 0)
}

