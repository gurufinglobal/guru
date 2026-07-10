package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestKeeperDenomQuery(t *testing.T) {
	t.Run("nil request returns error", func(t *testing.T) {
		k, ctx, _, _ := setupKeeperStateTester(t)

		_, err := k.Denom(ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("invalid hash returns error", func(t *testing.T) {
		k, ctx, _, _ := setupKeeperStateTester(t)
		k.SetDenom(ctx, types.NewDenom("uatom"))

		_, err := k.Denom(ctx, &transwapv1.QueryDenomRequest{Hash: "ibc/not-a-hex"})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing denom returns not found", func(t *testing.T) {
		k, ctx, _, _ := setupKeeperStateTester(t)

		missingHash := types.DenomHash(types.NewDenom("missing")).String()
		_, err := k.Denom(ctx, &transwapv1.QueryDenomRequest{Hash: "ibc/" + missingHash})
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("found denom", func(t *testing.T) {
		k, ctx, _, _ := setupKeeperStateTester(t)
		denom := types.NewDenom("uatom", types.NewHop(types.PortID, "channel-0"))
		k.SetDenom(ctx, denom)

		resp, err := k.Denom(ctx, &transwapv1.QueryDenomRequest{Hash: "ibc/" + types.DenomHash(denom).String()})
		require.NoError(t, err)
		require.Equal(t, denom.Base, resp.Denom.Base)
		require.Len(t, resp.Denom.Trace, 1)
	})
}

func TestKeeperDenomsQuery(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	k.SetDenom(ctx, types.NewDenom("btoken"))
	k.SetDenom(ctx, types.NewDenom("atoken"))

	resp, err := k.Denoms(ctx, &transwapv1.QueryDenomsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Denoms, 2)
	require.Equal(t, "atoken", resp.Denoms[0].Base)
	require.Equal(t, "btoken", resp.Denoms[1].Base)

	firstPage, err := k.Denoms(ctx, &transwapv1.QueryDenomsRequest{
		Pagination: &queryv1beta1.PageRequest{Limit: 1, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Denoms, 1)
	require.NotNil(t, firstPage.Pagination)
	require.Equal(t, uint64(2), firstPage.Pagination.Total)
	require.NotEmpty(t, firstPage.Pagination.NextKey)

	secondPage, err := k.Denoms(ctx, &transwapv1.QueryDenomsRequest{
		Pagination: &queryv1beta1.PageRequest{Key: firstPage.Pagination.NextKey, Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Denoms, 1)
	require.Empty(t, secondPage.Pagination.NextKey)

	seen := map[string]bool{
		firstPage.Denoms[0].Base:  true,
		secondPage.Denoms[0].Base: true,
	}
	require.Equal(t, map[string]bool{"atoken": true, "btoken": true}, seen)

	// invalid stored bytes should return unmarshal error
	badStore := prefix.NewStore(runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx)), types.DenomKey)
	badStore.Set(types.DenomHash(types.NewDenom("bad-token")), []byte{0x01, 0x02})
	_, err = k.Denoms(ctx, &transwapv1.QueryDenomsRequest{Pagination: &queryv1beta1.PageRequest{}})
	require.Error(t, err)
}

func TestKeeperDenomsQueryNilRequest(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	_, err := k.Denoms(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestKeeperDenomHashQuery(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	native := types.NewDenom("uatom")
	k.SetDenom(ctx, native)
	traced := types.NewDenom("ugxusd", types.NewHop(types.PortID, "channel-0"))
	k.SetDenom(ctx, traced)

	traceResp, err := k.DenomHash(ctx, &transwapv1.QueryDenomHashRequest{Trace: "uatom"})
	require.NoError(t, err)
	require.Equal(t, types.DenomHash(native).String(), traceResp.Hash)

	traceResp, err = k.DenomHash(ctx, &transwapv1.QueryDenomHashRequest{Trace: types.DenomPath(traced)})
	require.NoError(t, err)
	require.Equal(t, types.DenomHash(traced).String(), traceResp.Hash)

	_, err = k.DenomHash(ctx, &transwapv1.QueryDenomHashRequest{Trace: ""})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = k.DenomHash(ctx, &transwapv1.QueryDenomHashRequest{Trace: "transfer/channel-0/unknown"})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestKeeperDenomHashQueryNilRequest(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	_, err := k.DenomHash(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestKeeperEscrowAddressQuery(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	k.channelKeeper = refundAccountingChannelKeeper{
		portID:    types.PortID,
		channelID: "channel-0",
	}

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := k.EscrowAddress(ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing channel returns not found", func(t *testing.T) {
		_, err := k.EscrowAddress(ctx, &transwapv1.QueryEscrowAddressRequest{
			PortId:    "wrong-port",
			ChannelId: "wrong-channel",
		})
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("valid query returns escrow address", func(t *testing.T) {
		resp, err := k.EscrowAddress(ctx, &transwapv1.QueryEscrowAddressRequest{
			PortId:    types.PortID,
			ChannelId: "channel-0",
		})
		require.NoError(t, err)
		require.Equal(t, types.GetEscrowAddress(types.PortID, "channel-0").String(), resp.EscrowAddress)
	})
}

func TestKeeperTotalEscrowForDenomQuery(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	k.SetTotalEscrowForDenom(ctx, sdk.NewCoin("uatom", sdkmath.NewInt(123)))

	t.Run("missing denom returns zero", func(t *testing.T) {
		resp, err := k.TotalEscrowForDenom(ctx, &transwapv1.QueryTotalEscrowForDenomRequest{
			Denom: "missing",
		})
		require.NoError(t, err)
		require.Equal(t, "0", resp.Amount.Amount)
		require.Equal(t, "missing", resp.Amount.Denom)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := k.TotalEscrowForDenom(ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("invalid denom returns error", func(t *testing.T) {
		_, err := k.TotalEscrowForDenom(ctx, &transwapv1.QueryTotalEscrowForDenomRequest{
			Denom: "bad denom",
		})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("found denom returns escrow", func(t *testing.T) {
		resp, err := k.TotalEscrowForDenom(ctx, &transwapv1.QueryTotalEscrowForDenomRequest{
			Denom: "uatom",
		})
		require.NoError(t, err)
		require.Equal(t, "123", resp.Amount.Amount)
		require.Equal(t, "uatom", resp.Amount.Denom)
	})

	t.Run("zeroed denom returns zero after state pruning", func(t *testing.T) {
		k.SetTotalEscrowForDenom(ctx, sdk.NewCoin("uatom", sdkmath.ZeroInt()))

		resp, err := k.TotalEscrowForDenom(ctx, &transwapv1.QueryTotalEscrowForDenomRequest{
			Denom: "uatom",
		})
		require.NoError(t, err)
		require.Equal(t, "0", resp.Amount.Amount)
		require.Equal(t, "uatom", resp.Amount.Denom)
		require.Empty(t, k.GetAllTotalEscrowed(ctx))
	})
}

func TestPulsarSDKPageRequestConverters(t *testing.T) {
	pageReq := &queryv1beta1.PageRequest{
		Key:        []byte("key"),
		Offset:     4,
		Limit:      7,
		CountTotal: true,
		Reverse:    true,
	}
	sdkReq := pulsarPageRequestToSDK(pageReq)
	require.Equal(t, pageReq.Key, sdkReq.Key)
	require.Equal(t, pageReq.Offset, sdkReq.Offset)
	require.Equal(t, pageReq.Limit, sdkReq.Limit)
	require.Equal(t, pageReq.CountTotal, sdkReq.CountTotal)
	require.Equal(t, pageReq.Reverse, sdkReq.Reverse)

	require.Nil(t, pulsarPageRequestToSDK(nil))

	sdkRes := &sdkquery.PageResponse{
		NextKey: []byte("next"),
		Total:   9,
	}
	pageRes := sdkPageResponseToPulsar(sdkRes)
	require.Equal(t, sdkRes.NextKey, pageRes.NextKey)
	require.Equal(t, sdkRes.Total, pageRes.Total)
	require.Nil(t, sdkPageResponseToPulsar(nil))
}
