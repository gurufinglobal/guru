package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

func TestRegisterOracleRequest_SchedulesAndStartsAtNonceOne(t *testing.T) {
	k, ctx := setupKeeper(t)

	moderator := newAddr()
	k.SetModeratorAddress(ctx, moderator)

	msg := &types.MsgRegisterOracleRequest{
		ModeratorAddress: moderator,
		Category:         types.Category_CATEGORY_CRYPTO,
		Symbol:           "BTC",
		Count:            3,
		Period:           5,
	}

	resp, err := k.RegisterOracleRequest(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	req, found := k.GetRequest(ctx, resp.RequestId)
	require.True(t, found)
	require.Equal(t, uint64(1), req.Nonce)
	require.Equal(t, types.Status_STATUS_ACTIVE, req.Status)

	// schedule at period height
	scheduled := k.GetScheduledTasks(ctx, uint64(ctx.BlockHeight())+req.Period)
	require.Len(t, scheduled, 1)
	require.Equal(t, resp.RequestId, scheduled[0])

	// event contains request id and nonce
	evts := ctx.EventManager().Events()
	require.Len(t, evts, 1)
	require.Equal(t, types.EventTypeOracleTask, evts[0].Type)
	require.Len(t, evts[0].Attributes, 2)
	require.Equal(t, types.AttributeKeyRequestID, string(evts[0].Attributes[0].Key))
	require.Equal(t, types.AttributeKeyNonce, string(evts[0].Attributes[1].Key))
}

func TestRegisterOracleRequest_UnauthorizedModerator(t *testing.T) {
	k, ctx := setupKeeper(t)
	k.SetModeratorAddress(ctx, newAddr())

	msg := &types.MsgRegisterOracleRequest{
		ModeratorAddress: newAddr(), // mismatch
		Category:         types.Category_CATEGORY_CRYPTO,
		Symbol:           "BTC",
		Count:            1,
		Period:           5,
	}

	_, err := k.RegisterOracleRequest(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
}

func TestUpdateOracleRequest(t *testing.T) {
	k, ctx := setupKeeper(t)
	moderator := newAddr()
	k.SetModeratorAddress(ctx, moderator)

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

	msg := &types.MsgUpdateOracleRequest{
		ModeratorAddress: moderator,
		RequestId:        1,
		Count:            10,
		Period:           7,
		Status:           types.Status_STATUS_INACTIVE,
	}

	_, err := k.UpdateOracleRequest(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	updated, _ := k.GetRequest(ctx, 1)
	require.Equal(t, int64(10), updated.Count)
	require.Equal(t, uint64(7), updated.Period)
	require.Equal(t, types.Status_STATUS_INACTIVE, updated.Status)
}

func TestSubmitOracleReport_InvalidNonce(t *testing.T) {
	k, ctx := setupKeeper(t)

	moderator := newAddr()
	k.SetModeratorAddress(ctx, moderator)
	provider := newAddr()
	k.AddWhitelistAddress(ctx, provider)

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

	msg := &types.MsgSubmitOracleReport{
		ProviderAddress: provider,
		RequestId:       1,
		Nonce:           2, // expect 1
		RawData:         "1.23",
		Signature:       []byte{1},
	}

	_, err := k.SubmitOracleReport(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)

	// nothing stored
	_, found := k.GetReport(ctx, 1, 2, provider)
	require.False(t, found)
}

func TestSubmitOracleReport_NotWhitelisted(t *testing.T) {
	k, ctx := setupKeeper(t)

	moderator := newAddr()
	k.SetModeratorAddress(ctx, moderator)

	req := types.OracleRequest{
		Id:       2,
		Category: types.Category_CATEGORY_CRYPTO,
		Symbol:   "ETH",
		Count:    3,
		Period:   5,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    1,
	}
	k.SetRequest(ctx, req)

	provider := newAddr() // not added to whitelist

	msg := &types.MsgSubmitOracleReport{
		ProviderAddress: provider,
		RequestId:       2,
		Nonce:           1,
		RawData:         "2.34",
		Signature:       []byte{1},
	}

	_, err := k.SubmitOracleReport(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
}

