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

		// Emit single EventOracleTask event for daemon to listen
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeOracleTask,
				sdk.NewAttribute(types.AttributeKeyRequestID, strconv.FormatUint(requestID, 10)),
			),
		)

		k.Logger(ctx).Info("emitted oracle task event",
			"request_id", requestID,
			"block_height", blockHeight,
		)

		// Clean up the processed schedule
		k.DeleteScheduledTask(ctx, blockHeight, requestID)
	}
}
