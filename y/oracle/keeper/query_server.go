package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

var _ types.QueryServer = Keeper{}

// Params queries the module parameters.
func (k Keeper) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return &types.QueryParamsResponse{Params: k.GetParams(sdkCtx)}, nil
}

// ModeratorAddress queries the moderator address.
func (k Keeper) ModeratorAddress(ctx context.Context, _ *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return &types.QueryModeratorAddressResponse{ModeratorAddress: k.GetModeratorAddress(sdkCtx)}, nil
}

// OracleRequest queries a specific oracle request by ID.
func (k Keeper) OracleRequest(ctx context.Context, req *types.QueryOracleRequestRequest) (*types.QueryOracleRequestResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if req == nil {
		return nil, fmt.Errorf("invalid request")
	}
	request, found := k.GetRequest(sdkCtx, req.RequestId)
	if !found {
		return nil, types.ErrRequestNotFound
	}
	return &types.QueryOracleRequestResponse{Request: request}, nil
}

// OracleRequests queries all oracle requests with optional filters.
func (k Keeper) OracleRequests(ctx context.Context, req *types.QueryOracleRequestsRequest) (*types.QueryOracleRequestsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if req == nil {
		return nil, fmt.Errorf("invalid request")
	}
	store := prefix.NewStore(sdkCtx.KVStore(k.storeKey), []byte{types.RequestKey(0)[0]})

	requests := []types.OracleRequest{}
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var r types.OracleRequest
		k.cdc.MustUnmarshal(iter.Value(), &r)
		if req.Category != types.Category_CATEGORY_UNSPECIFIED && r.Category != req.Category {
			continue
		}
		if req.Status != types.Status_STATUS_UNSPECIFIED && r.Status != req.Status {
			continue
		}
		requests = append(requests, r)
	}

	return &types.QueryOracleRequestsResponse{
		Requests: requests,
	}, nil
}

// OracleReports queries reports for a specific request.
func (k Keeper) OracleReports(ctx context.Context, req *types.QueryOracleReportsRequest) (*types.QueryOracleReportsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if req == nil {
		return nil, fmt.Errorf("invalid request")
	}

	if req.Provider != "" && req.Nonce != 0 {
		if report, ok := k.GetReport(sdkCtx, req.RequestId, req.Nonce, req.Provider); ok {
			return &types.QueryOracleReportsResponse{
				Reports: []types.OracleReport{report},
			}, nil
		}
		return &types.QueryOracleReportsResponse{Reports: []types.OracleReport{}}, nil
	}

	var prefixKey []byte
	if req.Nonce != 0 {
		prefixKey = types.ReportPrefix(req.RequestId, req.Nonce)
	} else {
		prefixKey = types.ReportRequestPrefix(req.RequestId)
	}

	store := prefix.NewStore(sdkCtx.KVStore(k.storeKey), prefixKey)
	reports := []types.OracleReport{}
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var r types.OracleReport
		k.cdc.MustUnmarshal(iter.Value(), &r)
		if req.Provider != "" && r.Provider != req.Provider {
			continue
		}
		reports = append(reports, r)
	}

	return &types.QueryOracleReportsResponse{
		Reports: reports,
	}, nil
}

// OracleResult queries the result for a specific request.
func (k Keeper) OracleResult(ctx context.Context, req *types.QueryOracleResultRequest) (*types.QueryOracleResultResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if req == nil {
		return nil, fmt.Errorf("invalid request")
	}

	var (
		result types.OracleResult
		found  bool
	)

	if req.Nonce == 0 {
		result, found = k.GetLatestResult(sdkCtx, req.RequestId)
	} else {
		result, found = k.GetResult(sdkCtx, req.RequestId, req.Nonce)
	}

	if !found {
		return &types.QueryOracleResultResponse{}, nil
	}

	return &types.QueryOracleResultResponse{Result: result}, nil
}

// OracleResults queries all results for a specific request (history).
func (k Keeper) OracleResults(ctx context.Context, req *types.QueryOracleResultsRequest) (*types.QueryOracleResultsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if req == nil {
		return nil, fmt.Errorf("invalid request")
	}

	store := prefix.NewStore(sdkCtx.KVStore(k.storeKey), types.ResultPrefix(req.RequestId))
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	results := []types.OracleResult{}
	for ; iter.Valid(); iter.Next() {
		var r types.OracleResult
		k.cdc.MustUnmarshal(iter.Value(), &r)
		results = append(results, r)
	}

	return &types.QueryOracleResultsResponse{
		Results: results,
	}, nil
}

// Categories queries all enabled categories.
func (k Keeper) Categories(ctx context.Context, _ *types.QueryCategoriesRequest) (*types.QueryCategoriesResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return &types.QueryCategoriesResponse{Categories: k.GetCategories(sdkCtx)}, nil
}

// Whitelist queries the whitelist.
func (k Keeper) Whitelist(ctx context.Context, _ *types.QueryWhitelistRequest) (*types.QueryWhitelistResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	addrs := k.GetWhitelist(sdkCtx)
	return &types.QueryWhitelistResponse{
		Addresses: addrs,
		Count:     k.GetWhitelistCount(sdkCtx),
	}, nil
}
