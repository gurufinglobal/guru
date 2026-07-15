package keeper

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func RefundID(outputPort, outputChannel string, outputSequence uint64) string {
	return fmt.Sprintf("%s/%s/%d", outputPort, outputChannel, outputSequence)
}

func refundPacketIndexKey(portID, channelID string, sequence uint64) string {
	return fmt.Sprintf("%s/%s/%d", portID, channelID, sequence)
}

func (k Keeper) refundRecordStore(ctx sdk.Context) prefix.Store {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(store, []byte(types.RefundRecordPrefix))
}

func (k Keeper) refundOutputStore(ctx sdk.Context) prefix.Store {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(store, []byte(types.RefundOutputPrefix))
}

func (k Keeper) refundActiveStore(ctx sdk.Context) prefix.Store {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(store, []byte(types.RefundActivePrefix))
}

func (k Keeper) CreateRefundRecord(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if err := ValidateRefundRecord(record); err != nil {
		return err
	}
	store := k.refundRecordStore(ctx)
	if store.Has([]byte(record.GetId())) {
		return types.ErrInvalidRefundState.Wrapf("refund %s already exists", record.GetId())
	}
	if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_PENDING {
		return types.ErrInvalidRefundState.Wrap("new refund must start in REFUND_PENDING")
	}
	outputKey := refundPacketIndexKey(
		record.GetOriginalOutputPort(),
		record.GetOriginalOutputChannel(),
		record.GetOriginalOutputSequence(),
	)
	outputStore := k.refundOutputStore(ctx)
	if outputStore.Has([]byte(outputKey)) {
		return types.ErrInvalidRefundState.Wrapf("output packet %s already tracks a refund", outputKey)
	}
	store.Set([]byte(record.GetId()), k.cdc.MustMarshal(record))
	outputStore.Set([]byte(outputKey), []byte(record.GetId()))
	emitRefundEvent(ctx, types.EventTypeRefundCreated, record)
	return nil
}

func (k Keeper) SetRefundRecord(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if err := ValidateRefundRecord(record); err != nil {
		return err
	}
	existing, found, err := k.GetRefundRecord(ctx, record.GetId())
	if err != nil {
		return err
	}
	if found && !immutableRefundFieldsEqual(existing, record) {
		return types.ErrInvalidRefundState.Wrapf("refund %s immutable fields changed", record.GetId())
	}
	k.refundRecordStore(ctx).Set([]byte(record.GetId()), k.cdc.MustMarshal(record))
	return nil
}

// deleteCompletedRefundRecord removes resolved transport state once every
// packet index and liability has been settled. CLAIMED records deliberately
// remain as one-time claim receipts so a repeated manual claim is idempotent.
func (k Keeper) deleteCompletedRefundRecord(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if record == nil || record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_COMPLETED {
		return types.ErrInvalidRefundState.Wrap("only completed refunds may be pruned")
	}
	if types.HasActiveRefundPacket(record) || record.GetNextRetryHeight() != 0 {
		return types.ErrInvalidRefundState.Wrap("completed refund still has live transport state")
	}
	k.refundRecordStore(ctx).Delete([]byte(record.GetId()))
	return nil
}

func (k Keeper) GetRefundRecord(ctx sdk.Context, refundID string) (*transwapv1.RefundRecord, bool, error) {
	if strings.TrimSpace(refundID) == "" {
		return nil, false, types.ErrRefundNotFound.Wrap("refund id cannot be empty")
	}
	bz := k.refundRecordStore(ctx).Get([]byte(refundID))
	if len(bz) == 0 {
		return nil, false, nil
	}
	record := &transwapv1.RefundRecord{}
	if err := k.cdc.Unmarshal(bz, record); err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (k Keeper) MustGetRefundRecord(ctx sdk.Context, refundID string) (*transwapv1.RefundRecord, error) {
	record, found, err := k.GetRefundRecord(ctx, refundID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, types.ErrRefundNotFound.Wrapf("refund %s", refundID)
	}
	return record, nil
}

func (k Keeper) GetAllRefundRecords(ctx sdk.Context) []*transwapv1.RefundRecord {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	iterator := storetypes.KVStorePrefixIterator(store, []byte(types.RefundRecordPrefix))
	defer sdk.LogDeferred(k.Logger(ctx), func() error { return iterator.Close() })

	records := make([]*transwapv1.RefundRecord, 0)
	for ; iterator.Valid(); iterator.Next() {
		record := &transwapv1.RefundRecord{}
		k.cdc.MustUnmarshal(iterator.Value(), record)
		records = append(records, record)
	}
	return records
}

func (k Keeper) assertRefundIndexes(
	ctx sdk.Context,
	expectedOutputIndexes, expectedActiveIndexes, expectedRetryIndexes map[string]string,
) error {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	indexSets := []struct {
		prefix   string
		expected map[string]string
	}{
		{prefix: types.RefundOutputPrefix, expected: expectedOutputIndexes},
		{prefix: types.RefundActivePrefix, expected: expectedActiveIndexes},
		{prefix: types.RefundRetryPrefix, expected: expectedRetryIndexes},
	}
	for _, indexSet := range indexSets {
		prefixValue := indexSet.prefix
		expected := indexSet.expected
		iterator := storetypes.KVStorePrefixIterator(store, []byte(prefixValue))
		seen := make(map[string]struct{}, len(expected))
		for ; iterator.Valid(); iterator.Next() {
			key := strings.TrimPrefix(string(iterator.Key()), prefixValue)
			refundID := string(iterator.Value())
			expectedID, found := expected[key]
			if !found || expectedID != refundID {
				_ = iterator.Close()
				return types.ErrRefundEscrowInvariant.Wrapf(
					"unexpected %s index %s -> %s",
					strings.TrimSuffix(prefixValue, "/"),
					key,
					refundID,
				)
			}
			seen[key] = struct{}{}
		}
		if err := iterator.Close(); err != nil {
			return err
		}
		if len(seen) != len(expected) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"%s index count is %d, expected %d",
				strings.TrimSuffix(prefixValue, "/"),
				len(seen),
				len(expected),
			)
		}
	}
	return nil
}

func (k Keeper) refundForOutputPacket(
	ctx sdk.Context,
	portID, channelID string,
	sequence uint64,
) (*transwapv1.RefundRecord, bool, error) {
	key := refundPacketIndexKey(portID, channelID, sequence)
	refundID := k.refundOutputStore(ctx).Get([]byte(key))
	if len(refundID) == 0 {
		return nil, false, nil
	}
	record, found, err := k.GetRefundRecord(ctx, string(refundID))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, types.ErrRefundEscrowInvariant.Wrapf("output index %s references missing refund", key)
	}
	return record, true, nil
}

func (k Keeper) refundForActivePacket(
	ctx sdk.Context,
	portID, channelID string,
	sequence uint64,
) (*transwapv1.RefundRecord, bool, error) {
	key := refundPacketIndexKey(portID, channelID, sequence)
	refundID := k.refundActiveStore(ctx).Get([]byte(key))
	if len(refundID) == 0 {
		return nil, false, nil
	}
	record, found, err := k.GetRefundRecord(ctx, string(refundID))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, types.ErrRefundEscrowInvariant.Wrapf("active index %s references missing refund", key)
	}
	return record, true, nil
}

func (k Keeper) setActiveRefundPacketIndex(ctx sdk.Context, record *transwapv1.RefundRecord) error {
	if record.GetStatus() != transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT ||
		record.GetActivePacketSequence() == 0 || record.GetActiveTimeoutTimestamp() == 0 {
		return types.ErrRefundPacketNotActive.Wrap("active index requires an in-flight packet")
	}
	key := refundPacketIndexKey(
		record.GetRefundSourcePort(),
		record.GetRefundSourceChannel(),
		record.GetActivePacketSequence(),
	)
	store := k.refundActiveStore(ctx)
	if existing := store.Get([]byte(key)); len(existing) != 0 && string(existing) != record.GetId() {
		return types.ErrRefundEscrowInvariant.Wrapf("active packet %s already belongs to another refund", key)
	}
	store.Set([]byte(key), []byte(record.GetId()))
	return nil
}

func (k Keeper) deleteOutputRefundPacketIndex(ctx sdk.Context, record *transwapv1.RefundRecord) {
	key := refundPacketIndexKey(
		record.GetOriginalOutputPort(),
		record.GetOriginalOutputChannel(),
		record.GetOriginalOutputSequence(),
	)
	k.refundOutputStore(ctx).Delete([]byte(key))
}

func (k Keeper) deleteActiveRefundPacketIndex(ctx sdk.Context, record *transwapv1.RefundRecord) {
	if record.GetActivePacketSequence() == 0 {
		return
	}
	key := refundPacketIndexKey(
		record.GetRefundSourcePort(),
		record.GetRefundSourceChannel(),
		record.GetActivePacketSequence(),
	)
	k.refundActiveStore(ctx).Delete([]byte(key))
}

func ValidateRefundRecord(record *transwapv1.RefundRecord) error {
	return types.ValidateRefundRecord(record)
}

func immutableRefundFieldsEqual(a, b *transwapv1.RefundRecord) bool {
	return a.GetId() == b.GetId() &&
		a.GetRefundSourcePort() == b.GetRefundSourcePort() &&
		a.GetRefundSourceChannel() == b.GetRefundSourceChannel() &&
		proto.Equal(a.GetToken(), b.GetToken()) &&
		a.GetReceiver() == b.GetReceiver() &&
		a.GetClaimAddress() == b.GetClaimAddress() &&
		a.GetMemo() == b.GetMemo() &&
		a.GetExchangeId() == b.GetExchangeId() &&
		proto.Equal(a.GetOriginalFee(), b.GetOriginalFee()) &&
		a.GetOriginalTimeoutTimestamp() == b.GetOriginalTimeoutTimestamp() &&
		proto.Equal(a.GetOriginalTimeoutHeight(), b.GetOriginalTimeoutHeight()) &&
		a.GetOriginalOutputPort() == b.GetOriginalOutputPort() &&
		a.GetOriginalOutputChannel() == b.GetOriginalOutputChannel() &&
		a.GetOriginalOutputSequence() == b.GetOriginalOutputSequence() &&
		bytes.Equal(a.GetOriginalOutputPacketCommitment(), b.GetOriginalOutputPacketCommitment()) &&
		proto.Equal(a.GetVolumeReservation(), b.GetVolumeReservation())
}

func transitionRefundStatus(record *transwapv1.RefundRecord, next transwapv1.RefundStatus) error {
	allowed := false
	switch record.GetStatus() {
	case transwapv1.RefundStatus_REFUND_STATUS_PENDING:
		allowed = next == transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE || next == transwapv1.RefundStatus_REFUND_STATUS_COMPLETED
	case transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE:
		allowed = next == transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT || next == transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE
	case transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT:
		allowed = next == transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE ||
			next == transwapv1.RefundStatus_REFUND_STATUS_COMPLETED ||
			next == transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE
	case transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE:
		allowed = next == transwapv1.RefundStatus_REFUND_STATUS_CLAIMED
	case transwapv1.RefundStatus_REFUND_STATUS_COMPLETED, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED:
		allowed = false
	}
	if !allowed {
		return types.ErrInvalidRefundState.Wrapf("%s -> %s", record.GetStatus(), next)
	}
	record.Status = next
	return nil
}

func emitRefundEvent(ctx sdk.Context, eventType string, record *transwapv1.RefundRecord, extra ...sdk.Attribute) {
	attributes := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyRefundID, record.GetId()),
		sdk.NewAttribute(types.AttributeKeyRefundStatus, record.GetStatus().String()),
		sdk.NewAttribute(types.AttributeKeyRetryCount, strconv.FormatUint(uint64(record.GetRetryCount()), 10)),
		sdk.NewAttribute(types.AttributeKeyPacketSequence, strconv.FormatUint(record.GetActivePacketSequence(), 10)),
		sdk.NewAttribute(types.AttributeKeyPacketTimeout, strconv.FormatUint(record.GetActiveTimeoutTimestamp(), 10)),
		sdk.NewAttribute(types.AttributeKeyClaimAddress, record.GetClaimAddress()),
		sdk.NewAttribute(types.AttributeKeyNextRetryHeight, strconv.FormatUint(record.GetNextRetryHeight(), 10)),
	}
	attributes = append(attributes, extra...)
	ctx.EventManager().EmitEvent(sdk.NewEvent(eventType, attributes...))
}
