// x/mymodule/keeper/keeper.go
package keeper

import (
	"encoding/binary"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type Keeper struct {
	cdc           codec.BinaryCodec
	storeKey      storetypes.StoreKey
	hooks         types.OracleHooks
	authority     string
	accountKeeper types.AccountKeeper
}

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

// SetHooks set the oracle hooks
func (k *Keeper) SetHooks(eh types.OracleHooks) *Keeper {
	if k.hooks != nil {
		panic("cannot set oracle hooks twice")
	}

	k.hooks = eh

	return k
}

// SetParams stores the oracle module parameters in the state store
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	store := ctx.KVStore(k.storeKey)
	bz, err := k.cdc.Marshal(&params)
	if err != nil {
		return err
	}

	store.Set(types.KeyParams, bz)

	return nil
}

// GetParams retrieves the oracle module parameters from the state store
// Returns default parameters if no parameters are found
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

func (k Keeper) GetAuthority() string {
	return k.authority
}

// SetModeratorAddress stores the moderator address in the state store
func (k Keeper) SetModeratorAddress(ctx sdk.Context, address string) error {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.KeyModeratorAddress, []byte(address))
	return nil
}

// GetModeratorAddress retrieves the moderator address from the state store
// Returns empty string if no address is found
func (k Keeper) GetModeratorAddress(ctx sdk.Context) string {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyModeratorAddress)
	if len(bz) == 0 {
		return ""
	}
	return string(bz)
}

// SetOracleRequestDocCount stores the total count of oracle request documents in the state store
// count: number of documents to store
func (k Keeper) SetOracleRequestDocCount(ctx sdk.Context, count uint64) {
	store := ctx.KVStore(k.storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, count)
	store.Set(types.KeyOracleRequestDocCount, bz)
}

// GetOracleRequestDocCount retrieves the total count of oracle request documents from the state store
// Returns: number of stored documents (0 if none exist)
func (k Keeper) GetOracleRequestDocCount(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyOracleRequestDocCount)
	if len(bz) == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

// SetOracleRequestDoc stores an oracle request document in the state store
// doc: oracle request document to store
func (k Keeper) SetOracleRequestDoc(ctx sdk.Context, doc types.OracleRequestDoc) {
	store := ctx.KVStore(k.storeKey)

	bz := k.cdc.MustMarshal(&doc)
	store.Set(types.GetOracleRequestDocKey(doc.RequestId), bz)
}

func (k Keeper) updateOracleRequestDoc(ctx sdk.Context, doc types.OracleRequestDoc) error {
	// Retrieve the existing oracle request document
	existingDoc, err := k.GetOracleRequestDoc(ctx, doc.RequestId)
	if err != nil {
		return err
	}

	// Check if the existing document status is disabled
	if existingDoc.Status == types.RequestStatus_REQUEST_STATUS_DISABLED &&
		doc.Status == types.RequestStatus_REQUEST_STATUS_UNSPECIFIED {
		return fmt.Errorf("cannot modify disabled Request Doc except status")
	}

	// Update the period if it is not empty
	if doc.Period != 0 {
		existingDoc.Period = doc.Period
	}

	// Update the status if it is not empty
	if doc.Status != types.RequestStatus_REQUEST_STATUS_UNSPECIFIED {
		existingDoc.Status = doc.Status
	}

	// Update the account list if it is not empty
	if len(doc.AccountList) != 0 {
		existingDoc.AccountList = doc.AccountList
	}

	// Update the quorum if it is not empty
	if doc.Quorum != 0 {
		existingDoc.Quorum = doc.Quorum
	}

	// Update the endpoints if they are not empty
	if len(doc.Endpoints) != 0 {
		existingDoc.Endpoints = doc.Endpoints
	}

	// Update the aggregation rule if it is not empty
	if doc.AggregationRule != types.AggregationRule_AGGREGATION_RULE_UNSPECIFIED {
		existingDoc.AggregationRule = doc.AggregationRule
	}

	// Validate the updated oracle request document with current parameters
	params := k.GetParams(ctx)
	err = existingDoc.ValidateWithParams(params)
	if err != nil {
		return fmt.Errorf("validation failed for updated document: %v", err)
	}

	// Store the updated oracle request document
	k.SetOracleRequestDoc(ctx, *existingDoc)
	return nil
}

// GetOracleRequestDoc retrieves an oracle request document by ID from the state store
// id: ID of the document to retrieve
// Returns: retrieved oracle request document and error (error if document doesn't exist)
func (k Keeper) GetOracleRequestDoc(ctx sdk.Context, id uint64) (*types.OracleRequestDoc, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetOracleRequestDocKey(id))
	if len(bz) == 0 {
		return nil, fmt.Errorf("not exist RequestDoc(req_id: %d)", id)
	}

	var doc types.OracleRequestDoc
	k.cdc.MustUnmarshal(bz, &doc)
	return &doc, nil
}

func (k Keeper) GetOracleRequestDocs(ctx sdk.Context) []*types.OracleRequestDoc {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.KeyOracleRequestDoc)
	defer iterator.Close()

	var docs []*types.OracleRequestDoc
	for ; iterator.Valid(); iterator.Next() {
		var doc types.OracleRequestDoc
		k.cdc.MustUnmarshal(iterator.Value(), &doc)
		docs = append(docs, &doc)
	}
	return docs
}

func (k Keeper) GetOracleRequestDocsByStatus(ctx sdk.Context, status types.RequestStatus) []*types.OracleRequestDoc {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.KeyOracleRequestDoc)
	defer iterator.Close()

	var docs []*types.OracleRequestDoc
	for ; iterator.Valid(); iterator.Next() {
		var doc types.OracleRequestDoc
		k.cdc.MustUnmarshal(iterator.Value(), &doc)
		if doc.Status == status {
			docs = append(docs, &doc)
		}
	}
	return docs
}

func (k Keeper) SetSubmitData(ctx sdk.Context, data types.SubmitDataSet) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&data)
	key := types.GetSubmitDataKeyByProvider(data.RequestId, data.Nonce, data.Provider)
	store.Set(key, bz)
}

func (k Keeper) GetSubmitData(ctx sdk.Context, requestID uint64, nonce uint64, provider string) ([]*types.SubmitDataSet, error) {
	store := ctx.KVStore(k.storeKey)
	var datas []*types.SubmitDataSet
	if provider == "" {
		datas, err := k.GetSubmitDatas(ctx, requestID, nonce)
		if err != nil {
			return nil, err
		}
		return datas, nil
	}

	key := types.GetSubmitDataKeyByProvider(requestID, nonce, provider)
	bz := store.Get(key)
	if len(bz) == 0 {
		return nil, fmt.Errorf("not exist SubmitData(req_id: %d, nonce: %d, provider: %s)", requestID, nonce, provider)
	}

	var data types.SubmitDataSet
	k.cdc.MustUnmarshal(bz, &data)
	datas = append(datas, &data)
	return datas, nil
}

func (k Keeper) GetSubmitDatas(ctx sdk.Context, requestID uint64, nonce uint64) ([]*types.SubmitDataSet, error) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.GetSubmitDataKey(requestID, nonce))
	defer iterator.Close()

	var datas []*types.SubmitDataSet
	for ; iterator.Valid(); iterator.Next() {
		var data types.SubmitDataSet
		if err := k.cdc.Unmarshal(iterator.Value(), &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal submit data: %w", err)
		}
		datas = append(datas, &data)
	}
	return datas, nil
}

// SetDataSet stores the aggregated oracle data
func (k Keeper) SetDataSet(ctx sdk.Context, dataSet types.DataSet) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&dataSet)
	store.Set(types.GetDataSetKey(dataSet.RequestId, dataSet.Nonce), bz)
}

func (k Keeper) GetDataSet(ctx sdk.Context, requestID uint64, nonce uint64) (*types.DataSet, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetDataSetKey(requestID, nonce))
	if len(bz) == 0 {
		return nil, fmt.Errorf("not exist DataSet(req_id: %d, nonce: %d)", requestID, nonce)
	}

	var dataSet types.DataSet
	k.cdc.MustUnmarshal(bz, &dataSet)
	return &dataSet, nil
}

// Logger returns a logger instance with the module name prefixed
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func (k Keeper) checkAccountAuthorized(accountList []string, fromAddress string) bool {
	for _, account := range accountList {
		if account == fromAddress {
			return true
		}
	}
	return false
}

func (k Keeper) validateSubmitData(data types.SubmitDataSet) error {
	if data.RequestId == 0 {
		return errorsmod.Wrapf(types.ErrInvalidRequestID, "request id is 0")
	}
	if data.Nonce == 0 {
		return errorsmod.Wrapf(types.ErrInvalidNonce, "nonce is 0")
	}
	if data.Provider == "" {
		return errorsmod.Wrapf(types.ErrInvalidProvider, "provider is empty")
	}
	if data.RawData == "" {
		return errorsmod.Wrapf(types.ErrInvalidRawData, "raw data is empty")
	}
	return nil
}

// GetOracleData retrieves the oracle data by request ID
func (k Keeper) GetOracleData(ctx sdk.Context, requestID uint64) (*types.QueryOracleDataResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	doc, err := k.GetOracleRequestDoc(sdkCtx, requestID)
	if err != nil {
		return nil, err
	}

	dataset, err := k.GetDataSet(sdkCtx, doc.RequestId, doc.Nonce)
	if err != nil {
		return nil, err
	}

	return &types.QueryOracleDataResponse{
		DataSet: dataset,
	}, nil
}
