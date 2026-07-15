package keeper

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const refundRetryHeightBytes = 8

type scheduledRefundRetry struct {
	height   uint64
	refundID string
}

func refundRetryQueueKey(height uint64, refundID string) []byte {
	key := make([]byte, len(types.RefundRetryPrefix)+refundRetryHeightBytes+len(refundID))
	copy(key, types.RefundRetryPrefix)
	binary.BigEndian.PutUint64(key[len(types.RefundRetryPrefix):], height)
	copy(key[len(types.RefundRetryPrefix)+refundRetryHeightBytes:], refundID)
	return key
}

func (k Keeper) clearRefundRetrySchedule(ctx sdk.Context, record *transwapv1.RefundRecord) {
	if record.GetNextRetryHeight() == 0 {
		return
	}
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store.Delete(refundRetryQueueKey(record.GetNextRetryHeight(), record.GetId()))
	record.NextRetryHeight = 0
}

func (k Keeper) scheduleRefundRetry(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE {
		return types.ErrInvalidRefundState.Wrap("only REFUND_RETRYABLE may be scheduled")
	}
	height := ctx.BlockHeight()
	if height < 0 || height == math.MaxInt64 {
		return types.ErrInvalidRefundState.Wrap("block height cannot schedule a refund retry")
	}

	k.clearRefundRetrySchedule(ctx, record)
	record.NextRetryHeight = uint64(height + 1) //nolint:gosec // checked non-negative and below MaxInt64.
	if err := k.SetRefundRecord(ctx, record); err != nil {
		return err
	}
	return k.restoreRefundRetrySchedule(ctx, record)
}

func (k Keeper) restoreRefundRetrySchedule(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE || record.GetNextRetryHeight() == 0 {
		return types.ErrInvalidRefundState.Wrap("retry queue entry requires a scheduled RETRYABLE record")
	}
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	key := refundRetryQueueKey(record.GetNextRetryHeight(), record.GetId())
	if store.Has(key) {
		return types.ErrRefundEscrowInvariant.Wrapf("refund %s retry queue entry already exists", record.GetId())
	}
	store.Set(key, []byte(record.GetId()))
	return nil
}

func (k Keeper) dueRefundRetries(ctx sdk.Context, limit int) ([]scheduledRefundRetry, error) {
	if limit == 0 {
		return nil, nil
	}
	blockHeight := ctx.BlockHeight()
	if blockHeight < 0 {
		return nil, types.ErrRefundEscrowInvariant.Wrap("negative block height")
	}
	currentHeight := uint64(blockHeight) //nolint:gosec // checked non-negative.
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	iterator := storetypes.KVStorePrefixIterator(store, []byte(types.RefundRetryPrefix))
	defer sdk.LogDeferred(k.Logger(ctx), func() error { return iterator.Close() })

	due := make([]scheduledRefundRetry, 0, limit)
	for ; iterator.Valid() && len(due) < limit; iterator.Next() {
		key := iterator.Key()
		if len(key) <= len(types.RefundRetryPrefix)+refundRetryHeightBytes ||
			!bytes.HasPrefix(key, []byte(types.RefundRetryPrefix)) {
			return nil, types.ErrRefundEscrowInvariant.Wrap("malformed refund retry queue key")
		}
		offset := len(types.RefundRetryPrefix)
		height := binary.BigEndian.Uint64(key[offset : offset+refundRetryHeightBytes])
		if height > currentHeight {
			break
		}
		refundID := string(key[offset+refundRetryHeightBytes:])
		if err := types.ValidateRefundID(refundID); err != nil {
			return nil, types.ErrRefundEscrowInvariant.Wrapf("invalid queued refund id: %v", err)
		}
		if string(iterator.Value()) != refundID {
			return nil, types.ErrRefundEscrowInvariant.Wrapf("refund %s queue value does not match its key", refundID)
		}
		due = append(due, scheduledRefundRetry{height: height, refundID: refundID})
	}
	return due, nil
}

// ProcessRefundRetryQueue performs at most a fixed number of persisted local
// transport retries in one block. Each failure is rescheduled for the next
// block, so one callback can never consume all attempts or exceed this bound.
func (k Keeper) ProcessRefundRetryQueue(ctx sdk.Context) error {
	due, err := k.dueRefundRetries(ctx, types.MaxRefundRetryDispatchesPerBlock)
	if err != nil {
		return err
	}
	for _, scheduled := range due {
		record, err := k.MustGetRefundRecord(ctx, scheduled.refundID)
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("queued refund %s: %v", scheduled.refundID, err)
		}
		if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE ||
			record.GetNextRetryHeight() != scheduled.height {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"refund %s queue entry does not match persisted retry state",
				scheduled.refundID,
			)
		}
		if _, err := k.RetryRefund(ctx, scheduled.refundID); err != nil {
			return err
		}
	}
	return nil
}
