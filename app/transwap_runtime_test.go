package app

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	erc20v2 "github.com/cosmos/evm/x/erc20/v2"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	transwap "github.com/gurufinglobal/guru/v3/x/ibc/transwap"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestTranswapAllMsgAndQueryServicesExecuteThroughRuntimeRouters(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	router := testApp.IBCKeeper.PortKeeper.Router
	require.NotNil(t, router)
	require.True(t, router.Sealed())
	require.True(t, router.HasRoute(transwaptypes.ModuleName))
	ibcModule, found := router.Route(transwaptypes.ModuleName)
	require.True(t, found)
	require.IsType(t, &transwap.IBCModule{}, ibcModule)
	routerV2 := testApp.IBCKeeper.ChannelKeeperV2.Router
	require.NotNil(t, routerV2)
	require.True(t, routerV2.HasRoute(ibctransfertypes.ModuleName))
	require.IsType(t, erc20v2.IBCMiddleware{}, routerV2.Route(ibctransfertypes.ModuleName))

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  1,
		Time:    time.Unix(1_700_000_000, 0),
	})
	testApp.IBCKeeper.ChannelKeeper.SetChannel(
		ctx,
		transwaptypes.PortID,
		"channel-0",
		channeltypes.Channel{State: channeltypes.OPEN},
	)

	params := transwaptypes.DefaultParams()
	params.MaxRefundRetries = 1
	executedMsgs := map[string]struct{}{}
	executeTranswapRuntimeMsg(t, testApp, ctx, "UpdateParams", &transwapv1.MsgUpdateParams{
		Authority: testApp.TranswapKeeper.GetAuthority(),
		Params:    params,
	}, &transwapv1.MsgUpdateParamsResponse{}, executedMsgs)
	storedParams, err := testApp.TranswapKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(params, storedParams))

	refundReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x71}, 20)).String()
	retryable := transwapRuntimeRefundRecord(
		refundReceiver,
		transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE,
		params.GetMaxRefundRetries(),
		1,
	)
	retryable.NextRetryHeight = uint64(ctx.BlockHeight()) //nolint:gosec // fixed positive test height.
	require.NoError(t, testApp.TranswapKeeper.SetRefundRecord(ctx, retryable))
	retryResponse := &transwapv1.MsgRetryRefundResponse{}
	executeTranswapRuntimeMsg(t, testApp, ctx, "RetryRefund", &transwapv1.MsgRetryRefund{
		Signer:   refundReceiver,
		RefundId: retryable.GetId(),
	}, retryResponse, executedMsgs)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, retryResponse.GetRefund().GetStatus())
	require.Zero(t, retryResponse.GetRefund().GetNextRetryHeight())

	claimed := transwapRuntimeRefundRecord(
		refundReceiver,
		transwapv1.RefundStatus_REFUND_STATUS_CLAIMED,
		params.GetMaxRefundRetries(),
		2,
	)
	require.NoError(t, testApp.TranswapKeeper.SetRefundRecord(ctx, claimed))
	claimResponse := &transwapv1.MsgClaimRefundResponse{}
	executeTranswapRuntimeMsg(t, testApp, ctx, "ClaimRefund", &transwapv1.MsgClaimRefund{
		Signer:   refundReceiver,
		RefundId: claimed.GetId(),
	}, claimResponse, executedMsgs)
	require.Equal(t, transwapv1.RefundStatus_REFUND_STATUS_CLAIMED, claimResponse.GetRefund().GetStatus())
	require.Equal(t, claimed.GetId(), claimResponse.GetRefund().GetId())
	requireTranswapServiceMethodsExecuted(t, transwapv1.Msg_ServiceDesc.Methods, executedMsgs)

	queryDenom := transwaptypes.NewDenom(
		"uquery",
		transwaptypes.NewHop(transwaptypes.PortID, "channel-0"),
	)
	testApp.TranswapKeeper.SetDenom(ctx, queryDenom)
	testApp.TranswapKeeper.SetTotalEscrowForDenom(ctx, sdk.NewInt64Coin("uescrow", 77))

	executedQueries := map[string]struct{}{}
	paramsResponse := &transwapv1.QueryParamsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Params", &transwapv1.QueryParamsRequest{}, paramsResponse, executedQueries)
	require.True(t, proto.Equal(params, paramsResponse.GetParams()))

	refundsResponse := &transwapv1.QueryRefundsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Refunds", &transwapv1.QueryRefundsRequest{}, refundsResponse, executedQueries)
	require.Len(t, refundsResponse.GetRefunds(), 2)

	refundResponse := &transwapv1.QueryRefundResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Refund", &transwapv1.QueryRefundRequest{
		RefundId: claimed.GetId(),
	}, refundResponse, executedQueries)
	require.Equal(t, claimed.GetId(), refundResponse.GetRefund().GetId())

	denomsResponse := &transwapv1.QueryDenomsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Denoms", &transwapv1.QueryDenomsRequest{}, denomsResponse, executedQueries)
	require.Len(t, denomsResponse.GetDenoms(), 1)
	require.Equal(t, transwaptypes.DenomPath(queryDenom), transwaptypes.DenomPath(denomsResponse.GetDenoms()[0]))

	denomResponse := &transwapv1.QueryDenomResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Denom", &transwapv1.QueryDenomRequest{
		Hash: transwaptypes.DenomHash(queryDenom).String(),
	}, denomResponse, executedQueries)
	require.Equal(t, transwaptypes.DenomPath(queryDenom), transwaptypes.DenomPath(denomResponse.GetDenom()))

	denomHashResponse := &transwapv1.QueryDenomHashResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "DenomHash", &transwapv1.QueryDenomHashRequest{
		Trace: transwaptypes.DenomPath(queryDenom),
	}, denomHashResponse, executedQueries)
	require.Equal(t, transwaptypes.DenomHash(queryDenom).String(), denomHashResponse.GetHash())

	escrowAddressResponse := &transwapv1.QueryEscrowAddressResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "EscrowAddress", &transwapv1.QueryEscrowAddressRequest{
		PortId:    transwaptypes.PortID,
		ChannelId: "channel-0",
	}, escrowAddressResponse, executedQueries)
	require.Equal(
		t,
		transwaptypes.GetEscrowAddress(transwaptypes.PortID, "channel-0").String(),
		escrowAddressResponse.GetEscrowAddress(),
	)

	totalEscrowResponse := &transwapv1.QueryTotalEscrowForDenomResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "TotalEscrowForDenom", &transwapv1.QueryTotalEscrowForDenomRequest{
		Denom: "uescrow",
	}, totalEscrowResponse, executedQueries)
	require.Equal(t, "uescrow", totalEscrowResponse.GetAmount().GetDenom())
	require.Equal(t, "77", totalEscrowResponse.GetAmount().GetAmount())
	requireTranswapServiceMethodsExecuted(t, transwapv1.Query_ServiceDesc.Methods, executedQueries)
}

func transwapRuntimeRefundRecord(
	receiver string,
	status transwapv1.RefundStatus,
	retryCount uint32,
	sequence uint64,
) *transwapv1.RefundRecord {
	const denom = "urefund"
	return &transwapv1.RefundRecord{
		Id:                       transwaptypes.RefundID(transwaptypes.PortID, "channel-0", sequence),
		Status:                   status,
		RefundSourcePort:         transwaptypes.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    &transwapv1.Token{Denom: transwaptypes.NewDenom(denom), Amount: "10"},
		Receiver:                 receiver,
		ClaimAddress:             receiver,
		Memo:                     "runtime router test",
		ExchangeId:               "1",
		OriginalFee:              &basev1beta1.Coin{Denom: denom, Amount: "0"},
		OriginalTimeoutTimestamp: uint64(time.Unix(1_700_003_600, 0).UnixNano()), //nolint:gosec // fixed positive test time.
		OriginalTimeoutHeight: &transwapv1.RefundHeight{
			RevisionNumber: 1,
			RevisionHeight: 1,
		},
		OriginalOutputPort:             transwaptypes.PortID,
		OriginalOutputChannel:          "channel-0",
		OriginalOutputSequence:         sequence,
		RetryCount:                     retryCount,
		OriginalOutputPacketCommitment: make([]byte, sha256.Size),
		VolumeReservation: &bexv1.VolumeReservation{
			ExchangeId:             1,
			Direction:              bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			EpochSeconds:           bextypes.MinVolumeEpochSeconds,
			Amount:                 "1",
			VolumeWindowGeneration: 1,
		},
	}
}

func executeTranswapRuntimeMsg(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request sdk.Msg,
	response proto.Message,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "TransSwap Msg RPC executed more than once")
	handler := testApp.MsgServiceRouter().Handler(request)
	require.NotNil(t, handler, "TransSwap Msg RPC %s is not registered", method)
	result, err := handler(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MsgResponses, 1)
	require.Equal(t, "/guru.transwap.v1.Msg"+method+"Response", result.MsgResponses[0].TypeUrl)
	require.NoError(t, proto.Unmarshal(result.Data, response))
	executed[method] = struct{}{}
	t.Logf("runtime Msg/%s => %v", method, response)
}

func executeTranswapRuntimeQuery(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request proto.Message,
	response proto.Message,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "TransSwap Query RPC executed more than once")
	handler := testApp.GRPCQueryRouter().Route("/guru.transwap.v1.Query/" + method)
	require.NotNil(t, handler, "TransSwap Query RPC %s is not registered", method)
	requestBytes, err := proto.Marshal(request)
	require.NoError(t, err)
	queryResult, err := handler(ctx, &abci.RequestQuery{Data: requestBytes, Height: ctx.BlockHeight()})
	require.NoError(t, err)
	require.NotNil(t, queryResult)
	require.Equal(t, ctx.BlockHeight(), queryResult.Height)
	require.NoError(t, proto.Unmarshal(queryResult.Value, response))
	executed[method] = struct{}{}
	t.Logf("runtime Query/%s => %v", method, response)
}

func requireTranswapServiceMethodsExecuted(t *testing.T, methods []grpc.MethodDesc, executed map[string]struct{}) {
	t.Helper()
	require.Len(t, executed, len(methods))
	for _, method := range methods {
		require.Contains(t, executed, method.MethodName, "runtime RPC coverage must track the TransSwap service descriptor")
	}
}
