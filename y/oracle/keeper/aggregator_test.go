package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type mockHooks struct {
	called bool
	req    types.OracleRequest
	res    types.OracleResult
}

func (m *mockHooks) AfterOracleAggregation(ctx sdk.Context, request types.OracleRequest, result types.OracleResult) {
	m.called = true
	m.req = request
	m.res = result
}

func TestProcessOracleReportAggregation_AggregatesOnQuorum(t *testing.T) {
	k, ctx := setupKeeper(t)

	// whitelist three providers
	prov1, prov2, prov3 := newAddr(), newAddr(), newAddr()
	k.AddWhitelistAddress(ctx, prov1)
	k.AddWhitelistAddress(ctx, prov2)
	k.AddWhitelistAddress(ctx, prov3)

	req := types.OracleRequest{
		Id:       1,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "BTC",
		Count:    3,
		Period:   5,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    1,
	}
	k.SetRequest(ctx, req)

	// reports meet quorum (ceil(3*2/3)=2)
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: prov1, RawData: "1", Nonce: 1, Signature: []byte{1}})
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: prov2, RawData: "3", Nonce: 1, Signature: []byte{1}})
	k.SetReport(ctx, types.OracleReport{RequestId: 1, Provider: prov3, RawData: "5", Nonce: 1, Signature: []byte{1}})

	hooks := &mockHooks{}
	k.SetHooks(types.NewMultiOracleHooks(hooks))

	k.ProcessOracleReportAggregation(ctx)

	result, found := k.GetResult(ctx, 1, 1)
	require.True(t, found)
	require.Equal(t, "3", result.AggregatedData) // median of 1,3,5
	require.True(t, hooks.called)
	require.Equal(t, uint64(1), hooks.res.Nonce)

	// request nonce stays (rollover handled elsewhere)
	storedReq, _ := k.GetRequest(ctx, 1)
	require.Equal(t, uint64(1), storedReq.Nonce)
	require.Equal(t, int64(3), storedReq.Count)
}

func TestProcessOracleReportAggregation_BelowThreshold_NoResult(t *testing.T) {
	k, ctx := setupKeeper(t)

	// whitelist three providers, but only one report
	k.AddWhitelistAddress(ctx, newAddr())
	k.AddWhitelistAddress(ctx, newAddr())
	k.AddWhitelistAddress(ctx, newAddr())

	req := types.OracleRequest{
		Id:       2,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "ETH",
		Count:    2,
		Period:   5,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    1,
	}
	k.SetRequest(ctx, req)

	k.SetReport(ctx, types.OracleReport{RequestId: 2, Provider: newAddr(), RawData: "10", Nonce: 1, Signature: []byte{1}})

	k.ProcessOracleReportAggregation(ctx)

	_, found := k.GetResult(ctx, 2, 1)
	require.False(t, found)
}

