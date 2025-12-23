package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

func TestQueryParamsAndModerator(t *testing.T) {
	k, ctx := setupKeeper(t)

	params := types.Params{
		Enable:                true,
		QuorumRatio:           types.DefaultQuorumRatio,
		ReportRetentionBlocks: 10,
	}
	require.NoError(t, k.SetParams(ctx, params))
	k.SetModeratorAddress(ctx, "guru1moderatorxxxxxxxxxxxxxxxxxxxxxxx0")

	res, err := k.Params(sdk.WrapSDKContext(ctx), &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, params, res.Params)

	moderator, err := k.ModeratorAddress(sdk.WrapSDKContext(ctx), &types.QueryModeratorAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, "guru1moderatorxxxxxxxxxxxxxxxxxxxxxxx0", moderator.ModeratorAddress)
}

func TestQueryRequestsAndReports(t *testing.T) {
	k, ctx := setupKeeper(t)

	req := types.OracleRequest{
		Id:       1,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "BTC",
		Count:    3,
		Period:   5,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    2,
	}
	k.SetRequest(ctx, req)

	// store reports for nonce 2
	prov := newAddr()
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: prov, RawData: "10", Nonce: 2, Signature: []byte{1}})
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: newAddr(), RawData: "12", Nonce: 2, Signature: []byte{1}})

	respReq, err := k.OracleRequest(sdk.WrapSDKContext(ctx), &types.QueryOracleRequestRequest{RequestId: 1})
	require.NoError(t, err)
	require.Equal(t, req, respReq.Request)

	respReqs, err := k.OracleRequests(sdk.WrapSDKContext(ctx), &types.QueryOracleRequestsRequest{Status: types.Status_STATUS_ACTIVE})
	require.NoError(t, err)
	require.Len(t, respReqs.Requests, 1)

	respReports, err := k.OracleReports(sdk.WrapSDKContext(ctx), &types.QueryOracleReportsRequest{
		RequestId: 1,
		Nonce:     2,
		Provider:  prov,
	})
	require.NoError(t, err)
	require.Len(t, respReports.Reports, 1)
	require.Equal(t, prov, respReports.Reports[0].Provider)
}

func TestQueryResultsAndWhitelist(t *testing.T) {
	k, ctx := setupKeeper(t)

	reqID := uint64(3)
	k.SetRequest(ctx, types.OracleRequest{
		Id:       reqID,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "SOL",
		Count:    3,
		Period:   4,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    5,
	})

	// store two results; latest pointer should point to nonce 6
	k.SetResult(ctx, types.OracleResult{RequestId: reqID, Nonce: 5, AggregatedData: "1", AggregatedHeight: 1, AggregatedTime: 1})
	k.SetResult(ctx, types.OracleResult{RequestId: reqID, Nonce: 6, AggregatedData: "2", AggregatedHeight: 2, AggregatedTime: 2})

	resLatest, err := k.OracleResult(sdk.WrapSDKContext(ctx), &types.QueryOracleResultRequest{RequestId: reqID})
	require.NoError(t, err)
	require.Equal(t, uint64(6), resLatest.Result.Nonce)

	resHistory, err := k.OracleResults(sdk.WrapSDKContext(ctx), &types.QueryOracleResultsRequest{RequestId: reqID})
	require.NoError(t, err)
	require.Len(t, resHistory.Results, 2)

	// whitelist and categories
	addr := newAddr()
	k.AddWhitelistAddress(ctx, addr)
	k.SetCategory(ctx, types.Category_CATEGORY_CRYPTO)

	whitelist, err := k.Whitelist(sdk.WrapSDKContext(ctx), &types.QueryWhitelistRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), whitelist.Count)
	require.Contains(t, whitelist.Addresses, addr)

	cats, err := k.Categories(sdk.WrapSDKContext(ctx), &types.QueryCategoriesRequest{})
	require.NoError(t, err)
	// Policy: proto-defined categories are present by default; genesis may add more.
	// At minimum, CRYPTO must be present.
	require.Contains(t, cats.Categories, types.Category_CATEGORY_CRYPTO)
}
