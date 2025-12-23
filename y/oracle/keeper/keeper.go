package keeper

import (
	"encoding/binary"
	"fmt"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// Keeper defines the oracle module keeper.
type Keeper struct {
	cdc           codec.BinaryCodec
	storeKey      storetypes.StoreKey
	authority     string
	accountKeeper types.AccountKeeper
	hooks         types.OracleHooks
}

// NewKeeper creates a new oracle keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority string,
	accountKeeper types.AccountKeeper,
) *Keeper {
	return &Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		authority:     authority,
		accountKeeper: accountKeeper,
	}
}

// GetAuthority returns the module authority address.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// SetParams sets the oracle module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	bz, err := k.cdc.Marshal(&params)
	if err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.KeyParams, bz)
	return nil
}

// GetParams returns the oracle module parameters.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyParams)
	if len(bz) == 0 {
		return types.DefaultParams()
	}
	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params
}

// SetModeratorAddress sets the moderator address.
func (k Keeper) SetModeratorAddress(ctx sdk.Context, addr string) {
	ctx.KVStore(k.storeKey).Set(types.KeyModeratorAddress, []byte(addr))
}

// GetModeratorAddress returns the moderator address.
func (k Keeper) GetModeratorAddress(ctx sdk.Context) string {
	bz := ctx.KVStore(k.storeKey).Get(types.KeyModeratorAddress)
	if len(bz) == 0 {
		return ""
	}
	return string(bz)
}

// whitelist helpers
// GetWhitelistCount returns the total number of whitelisted addresses.
func (k Keeper) GetWhitelistCount(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyWhitelistCount)
	if bz == nil {
		return 0
	}
	return sdk.BigEndianToUint64(bz)
}

func (k Keeper) setWhitelistCount(ctx sdk.Context, count uint64) {
	store := ctx.KVStore(k.storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, count)
	store.Set(types.KeyWhitelistCount, bz)
}

func (k Keeper) addToWhitelist(ctx sdk.Context, addr string) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetWhitelistKey(addr)
	if store.Has(key) {
		return
	}
	store.Set(key, []byte{1})
	k.setWhitelistCount(ctx, k.GetWhitelistCount(ctx)+1)
}

func (k Keeper) removeFromWhitelist(ctx sdk.Context, addr string) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetWhitelistKey(addr)
	if !store.Has(key) {
		return
	}
	store.Delete(key)
	cnt := k.GetWhitelistCount(ctx)
	if cnt > 0 {
		k.setWhitelistCount(ctx, cnt-1)
	}
}

// IsWhitelisted checks if an address is in the whitelist.
func (k Keeper) IsWhitelisted(ctx sdk.Context, addr string) bool {
	return ctx.KVStore(k.storeKey).Has(types.GetWhitelistKey(addr))
}

// GetWhitelist returns all whitelisted addresses.
func (k Keeper) GetWhitelist(ctx sdk.Context) []string {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, []byte(types.WhitelistKeyPrefix))
	defer iter.Close()
	var res []string
	for ; iter.Valid(); iter.Next() {
		// key suffix is original address bytes
		key := iter.Key()
		res = append(res, string(key[len(types.WhitelistKeyPrefix):]))
	}
	return res
}

// AddWhitelistAddress is an exported helper for genesis/msg usage.
func (k Keeper) AddWhitelistAddress(ctx sdk.Context, addr string) {
	k.addToWhitelist(ctx, addr)
}

// RemoveWhitelistAddress is an exported helper.
func (k Keeper) RemoveWhitelistAddress(ctx sdk.Context, addr string) {
	k.removeFromWhitelist(ctx, addr)
}

// setRequestCount sets the total request count.
func (k Keeper) setRequestCount(ctx sdk.Context, count uint64) {
	ctx.KVStore(k.storeKey).Set(types.KeyRequestCount, types.IDToBytes(count))
}

// getRequestCount returns the total request count.
func (k Keeper) getRequestCount(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyRequestCount)
	if len(bz) == 0 {
		return 0
	}
	return sdk.BigEndianToUint64(bz)
}

// SetRequestCount sets the request count (for genesis).
func (k Keeper) SetRequestCount(ctx sdk.Context, count uint64) {
	k.setRequestCount(ctx, count)
}

// NextRequestID returns the next request ID.
func (k Keeper) NextRequestID(ctx sdk.Context) uint64 {
	return k.getRequestCount(ctx) + 1
}

// IncrementRequestCount increments the request count.
func (k Keeper) IncrementRequestCount(ctx sdk.Context) {
	k.setRequestCount(ctx, k.NextRequestID(ctx))
}

// SetRequest sets an oracle request.
func (k Keeper) SetRequest(ctx sdk.Context, req types.OracleRequest) {
	ctx.KVStore(k.storeKey).Set(types.RequestKey(req.Id), k.cdc.MustMarshal(&req))
}

// GetRequest returns an oracle request by ID.
func (k Keeper) GetRequest(ctx sdk.Context, id uint64) (types.OracleRequest, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.RequestKey(id))
	if len(bz) == 0 {
		return types.OracleRequest{}, false
	}
	var req types.OracleRequest
	k.cdc.MustUnmarshal(bz, &req)
	return req, true
}

// IterateRequests iterates over all oracle requests.
func (k Keeper) IterateRequests(ctx sdk.Context, fn func(req types.OracleRequest) bool) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, []byte{types.RequestKey(0)[0]})
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		var req types.OracleRequest
		k.cdc.MustUnmarshal(iter.Value(), &req)
		if stop := fn(req); stop {
			return
		}
	}
}

// SetReport sets an oracle report.
func (k Keeper) SetReport(ctx sdk.Context, report types.OracleReport) {
	ctx.KVStore(k.storeKey).Set(types.ReportKey(report.RequestId, report.Nonce, report.Provider), k.cdc.MustMarshal(&report))
	k.incrementReportCount(ctx, report.RequestId, report.Nonce)
}

// incrementReportCount increments the report count for a specific request and nonce.
func (k Keeper) incrementReportCount(ctx sdk.Context, requestID, nonce uint64) {
	store := ctx.KVStore(k.storeKey)
	key := types.ReportCountKey(requestID, nonce)
	bz := store.Get(key)
	var count uint64
	if len(bz) > 0 {
		count = types.BytesToID(bz)
	}
	store.Set(key, types.IDToBytes(count+1))
}

// GetReportCount returns the number of reports for a specific request and nonce.
func (k Keeper) GetReportCount(ctx sdk.Context, requestID, nonce uint64) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ReportCountKey(requestID, nonce))
	if len(bz) == 0 {
		return 0
	}
	return types.BytesToID(bz)
}

// GetReport returns an oracle report.
func (k Keeper) GetReport(ctx sdk.Context, requestID, nonce uint64, provider string) (types.OracleReport, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ReportKey(requestID, nonce, provider))
	if len(bz) == 0 {
		return types.OracleReport{}, false
	}
	var report types.OracleReport
	k.cdc.MustUnmarshal(bz, &report)
	return report, true
}

// IterateReports iterates over all reports for a specific request and nonce.
func (k Keeper) IterateReports(ctx sdk.Context, requestID, nonce uint64, fn func(report types.OracleReport) bool) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.ReportPrefix(requestID, nonce))
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var report types.OracleReport
		k.cdc.MustUnmarshal(iter.Value(), &report)
		if stop := fn(report); stop {
			return
		}
	}
}

// SetResult sets an oracle result.
func (k Keeper) SetResult(ctx sdk.Context, result types.OracleResult) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.ResultKey(result.RequestId, result.Nonce), k.cdc.MustMarshal(&result))
	// Update latest result pointer for O(1) lookup
	k.setLatestResultNonce(ctx, result.RequestId, result.Nonce)

	// Create expiry index
	params := k.GetParams(ctx)
	if params.ReportRetentionBlocks > 0 {
		expireHeight := result.AggregatedHeight + params.ReportRetentionBlocks
		store.Set(types.ResultExpiryKey(expireHeight, result.RequestId, result.Nonce), []byte{1})
	}
}

// GetResult returns an oracle result.
func (k Keeper) GetResult(ctx sdk.Context, requestID, nonce uint64) (types.OracleResult, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ResultKey(requestID, nonce))
	if len(bz) == 0 {
		return types.OracleResult{}, false
	}
	var res types.OracleResult
	k.cdc.MustUnmarshal(bz, &res)
	return res, true
}

// setLatestResultNonce stores the latest nonce for a request (for O(1) lookup).
func (k Keeper) setLatestResultNonce(ctx sdk.Context, requestID, nonce uint64) {
	ctx.KVStore(k.storeKey).Set(types.LatestResultKey(requestID), types.IDToBytes(nonce))
}

// getLatestResultNonce retrieves the latest nonce for a request.
func (k Keeper) getLatestResultNonce(ctx sdk.Context, requestID uint64) (uint64, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.LatestResultKey(requestID))
	if len(bz) == 0 {
		return 0, false
	}
	return types.BytesToID(bz), true
}

// GetLatestResult returns the latest result for a request in O(1) time.
func (k Keeper) GetLatestResult(ctx sdk.Context, requestID uint64) (types.OracleResult, bool) {
	nonce, found := k.getLatestResultNonce(ctx, requestID)
	if !found {
		return types.OracleResult{}, false
	}
	return k.GetResult(ctx, requestID, nonce)
}

// SetCategory sets an enabled category.
func (k Keeper) SetCategory(ctx sdk.Context, cat types.Category) {
	if cat == types.Category_CATEGORY_UNSPECIFIED {
		return
	}
	ctx.KVStore(k.storeKey).Set(types.CategoryKey(cat), []byte{1})
}

// GetCategories returns all enabled categories.
func (k Keeper) GetCategories(ctx sdk.Context) []types.Category {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, []byte{types.CategoryKey(types.Category_CATEGORY_UNSPECIFIED)[0]})
	defer iter.Close()

	cats := []types.Category{}
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) < 2 {
			continue
		}
		cats = append(cats, types.Category(key[1]))
	}
	return cats
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("y/%s", types.ModuleName))
}

// ScheduleOracleTask schedules an oracle task event at a specific block height.
func (k Keeper) ScheduleOracleTask(ctx sdk.Context, blockHeight, requestID uint64) {
	ctx.KVStore(k.storeKey).Set(types.TaskScheduleKey(blockHeight, requestID), []byte{1})
	k.Logger(ctx).Info("scheduled oracle task", "block_height", blockHeight, "request_id", requestID)
}

// GetScheduledTasks returns all request IDs scheduled for a specific block height.
func (k Keeper) GetScheduledTasks(ctx sdk.Context, blockHeight uint64) []uint64 {
	store := ctx.KVStore(k.storeKey)
	prefix := types.TaskSchedulePrefix(blockHeight)
	iter := storetypes.KVStorePrefixIterator(store, prefix)
	defer iter.Close()

	var requestIDs []uint64
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		// key format: prefix (1) + blockHeight (8) + requestID (8)
		if len(key) >= 17 {
			requestID := types.BytesToID(key[9:17])
			requestIDs = append(requestIDs, requestID)
		}
	}
	return requestIDs
}

// DeleteScheduledTask removes a scheduled task.
func (k Keeper) DeleteScheduledTask(ctx sdk.Context, blockHeight, requestID uint64) {
	ctx.KVStore(k.storeKey).Delete(types.TaskScheduleKey(blockHeight, requestID))
}

// DeleteOldReports deletes reports older than retention blocks using index.
func (k Keeper) DeleteOldReports(ctx sdk.Context, currentHeight uint64) {
	// Iterate over the expiry index in-order and delete all entries with expireHeight <= currentHeight.
	// This naturally "catches up" if cleanup was skipped for some heights (e.g. module disabled and later enabled).
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, []byte{types.ResultExpiryPrefix(0)[0]})
	defer iter.Close()

	type toDelete struct {
		expiryKey    []byte
		requestID    uint64
		nonce        uint64
		expireHeight uint64
	}
	var dels []toDelete

	for ; iter.Valid(); iter.Next() {
		// Key structure: prefix (1) + expireHeight (8) + requestID (8) + nonce (8)
		key := iter.Key()
		if len(key) < 25 {
			continue
		}

		expireHeight := types.BytesToID(key[1:9])
		if expireHeight > currentHeight {
			// Since keys are ordered by expireHeight, we can stop early.
			break
		}

		requestID := types.BytesToID(key[9:17])
		nonce := types.BytesToID(key[17:25])

		dels = append(dels, toDelete{
			expiryKey:    append([]byte(nil), key...),
			requestID:    requestID,
			nonce:        nonce,
			expireHeight: expireHeight,
		})
	}

	for _, d := range dels {
		// Delete associated reports
		k.deleteReports(ctx, d.requestID, d.nonce)
		// Delete count key
		store.Delete(types.ReportCountKey(d.requestID, d.nonce))
		// Delete the expiry index itself
		store.Delete(d.expiryKey)

		k.Logger(ctx).Debug("deleted old reports", "request_id", d.requestID, "nonce", d.nonce, "expire_height", d.expireHeight, "height", currentHeight)
	}
}

// deleteReports deletes all reports for a specific request and nonce.
func (k Keeper) deleteReports(ctx sdk.Context, requestID, nonce uint64) {
	store := ctx.KVStore(k.storeKey)
	iter := storetypes.KVStorePrefixIterator(store, types.ReportPrefix(requestID, nonce))
	defer iter.Close()

	// Pre-allocate based on the tracked report count (best-effort; 0 is fine).
	capHint := int(k.GetReportCount(ctx, requestID, nonce))
	if capHint < 0 {
		capHint = 0
	}
	keys := make([][]byte, 0, capHint)
	for ; iter.Valid(); iter.Next() {
		// Copy the key: iterator implementations may reuse the underlying slice buffer.
		keys = append(keys, append([]byte(nil), iter.Key()...))
	}

	for _, key := range keys {
		store.Delete(key)
	}
}

func (k *Keeper) SetHooks(hooks types.OracleHooks) *Keeper {
	if k.hooks != nil {
		panic("cannot set oracle hooks twice")
	}
	k.hooks = hooks

	return k
}
