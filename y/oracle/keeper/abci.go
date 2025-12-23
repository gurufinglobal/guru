package keeper

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// BeginBlocker executes the begin block logic.
func (k Keeper) BeginBlocker(ctx sdk.Context) {
	params := k.GetParams(ctx)
	if !params.Enable {
		return
	}
	// no-op in begin block
}

// EndBlocker processes pending aggregations, emits scheduled events, and cleans up old reports.
func (k Keeper) EndBlocker(ctx sdk.Context) {
	params := k.GetParams(ctx)
	if !params.Enable {
		return
	}

	currentHeight := uint64(ctx.BlockHeight())

	// 1. Process aggregations
	k.ProcessOracleReportAggregation(ctx)

	// 2. Emit scheduled oracle task events
	k.processScheduledTasks(ctx, currentHeight)

	// 3. Delete old reports (based on report_retention_blocks)
	k.DeleteOldReports(ctx, currentHeight)
}

// processScheduledTasks emits EventOracleTask for all scheduled tasks at current block height.
func (k Keeper) processScheduledTasks(ctx sdk.Context, blockHeight uint64) {
	scheduledIDs := k.GetScheduledTasks(ctx, blockHeight)

	store := ctx.KVStore(k.storeKey)

	for _, requestID := range scheduledIDs {
		// Check if request is still active
		req, found := k.GetRequest(ctx, requestID)
		if !found {
			k.DeleteScheduledTask(ctx, blockHeight, requestID)
			continue
		}

		if req.Status != types.Status_STATUS_ACTIVE {
			k.DeleteScheduledTask(ctx, blockHeight, requestID)
			continue
		}

		// 이전 기간 보고서 정리(쿼럼 여부와 무관하게 fail-fast 롤오버)
		prevNonce := req.Nonce
		if prevNonce > 0 {
			k.deleteReports(ctx, req.Id, prevNonce)
			store.Delete(types.ReportCountKey(req.Id, prevNonce))
		}

		// 기간 진입: Nonce 증가 및 Count 소모(0 되면 비활성)
		req.Nonce++
		if req.Count > 0 {
			req.Count--
			if req.Count == 0 {
				req.Status = types.Status_STATUS_INACTIVE
			}
		}
		k.SetRequest(ctx, req)

		// Emit single EventOracleTask event for daemon to listen (현재 기간 nonce 포함)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeOracleTask,
				sdk.NewAttribute(types.AttributeKeyRequestID, strconv.FormatUint(requestID, 10)),
				sdk.NewAttribute(types.AttributeKeyNonce, strconv.FormatUint(req.Nonce, 10)),
			),
		)

		k.Logger(ctx).Info("emitted oracle task event",
			"request_id", requestID,
			"block_height", blockHeight,
			"nonce", req.Nonce,
		)

		// 다음 기간 스케줄 예약(집계 성공 여부와 무관)
		if req.Status == types.Status_STATUS_ACTIVE && req.Period > 0 {
			nextHeight := blockHeight + req.Period
			k.ScheduleOracleTask(ctx, nextHeight, req.Id)
		}

		// Clean up the processed schedule
		k.DeleteScheduledTask(ctx, blockHeight, requestID)
	}
}
