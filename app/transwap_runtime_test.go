package app

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"testing"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

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
	executeTranswapRuntimeMsg(t, testApp, ctx, "UpdateParams", &transwaptypes.MsgUpdateParams{
		Authority: testApp.TranswapKeeper.GetAuthority(),
		Params:    params,
	}, &transwaptypes.MsgUpdateParamsResponse{}, executedMsgs)
	storedParams, err := testApp.TranswapKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, params, storedParams)

	refundReceiver := sdk.AccAddress(bytes.Repeat([]byte{0x71}, 20)).String()
	retryable := transwapRuntimeRefundRecord(
		refundReceiver,
		transwaptypes.RefundStatus_REFUND_STATUS_RETRYABLE,
		params.GetMaxRefundRetries(),
		1,
	)
	retryable.NextRetryHeight = uint64(ctx.BlockHeight()) //nolint:gosec // fixed positive test height.
	require.NoError(t, testApp.TranswapKeeper.SetRefundRecord(ctx, retryable))
	retryResponse := &transwaptypes.MsgRetryRefundResponse{}
	executeTranswapRuntimeMsg(t, testApp, ctx, "RetryRefund", &transwaptypes.MsgRetryRefund{
		Signer:   refundReceiver,
		RefundId: retryable.GetId(),
	}, retryResponse, executedMsgs)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, retryResponse.GetRefund().GetStatus())
	require.Zero(t, retryResponse.GetRefund().GetNextRetryHeight())

	claimed := transwapRuntimeRefundRecord(
		refundReceiver,
		transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED,
		params.GetMaxRefundRetries(),
		2,
	)
	require.NoError(t, testApp.TranswapKeeper.SetRefundRecord(ctx, claimed))
	claimResponse := &transwaptypes.MsgClaimRefundResponse{}
	executeTranswapRuntimeMsg(t, testApp, ctx, "ClaimRefund", &transwaptypes.MsgClaimRefund{
		Signer:   refundReceiver,
		RefundId: claimed.GetId(),
	}, claimResponse, executedMsgs)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED, claimResponse.GetRefund().GetStatus())
	require.Equal(t, claimed.GetId(), claimResponse.GetRefund().GetId())
	requireTranswapServiceMethodsExecuted(t, reflect.TypeOf((*transwaptypes.MsgServer)(nil)).Elem(), executedMsgs)

	queryDenom := transwaptypes.NewDenom(
		"uquery",
		transwaptypes.NewHop(transwaptypes.PortID, "channel-0"),
	)
	testApp.TranswapKeeper.SetDenom(ctx, queryDenom)
	testApp.TranswapKeeper.SetTotalEscrowForDenom(ctx, sdk.NewInt64Coin("uescrow", 77))

	executedQueries := map[string]struct{}{}
	paramsResponse := &transwaptypes.QueryParamsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Params", &transwaptypes.QueryParamsRequest{}, paramsResponse, executedQueries)
	require.Equal(t, params, paramsResponse.GetParams())

	refundsResponse := &transwaptypes.QueryRefundsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Refunds", &transwaptypes.QueryRefundsRequest{}, refundsResponse, executedQueries)
	require.Len(t, refundsResponse.GetRefunds(), 2)

	refundResponse := &transwaptypes.QueryRefundResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Refund", &transwaptypes.QueryRefundRequest{
		RefundId: claimed.GetId(),
	}, refundResponse, executedQueries)
	require.Equal(t, claimed.GetId(), refundResponse.GetRefund().GetId())

	denomsResponse := &transwaptypes.QueryDenomsResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Denoms", &transwaptypes.QueryDenomsRequest{}, denomsResponse, executedQueries)
	require.Len(t, denomsResponse.GetDenoms(), 1)
	require.Equal(t, transwaptypes.DenomPath(queryDenom), transwaptypes.DenomPath(denomsResponse.GetDenoms()[0]))

	denomResponse := &transwaptypes.QueryDenomResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "Denom", &transwaptypes.QueryDenomRequest{
		Hash: transwaptypes.DenomHash(queryDenom).String(),
	}, denomResponse, executedQueries)
	require.NotNil(t, denomResponse.GetDenom())
	require.Equal(t, transwaptypes.DenomPath(queryDenom), transwaptypes.DenomPath(*denomResponse.GetDenom()))

	denomHashResponse := &transwaptypes.QueryDenomHashResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "DenomHash", &transwaptypes.QueryDenomHashRequest{
		Trace: transwaptypes.DenomPath(queryDenom),
	}, denomHashResponse, executedQueries)
	require.Equal(t, transwaptypes.DenomHash(queryDenom).String(), denomHashResponse.GetHash())

	escrowAddressResponse := &transwaptypes.QueryEscrowAddressResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "EscrowAddress", &transwaptypes.QueryEscrowAddressRequest{
		PortId:    transwaptypes.PortID,
		ChannelId: "channel-0",
	}, escrowAddressResponse, executedQueries)
	require.Equal(
		t,
		transwaptypes.GetEscrowAddress(transwaptypes.PortID, "channel-0").String(),
		escrowAddressResponse.GetEscrowAddress(),
	)

	totalEscrowResponse := &transwaptypes.QueryTotalEscrowForDenomResponse{}
	executeTranswapRuntimeQuery(t, testApp, ctx, "TotalEscrowForDenom", &transwaptypes.QueryTotalEscrowForDenomRequest{
		Denom: "uescrow",
	}, totalEscrowResponse, executedQueries)
	require.Equal(t, "uescrow", totalEscrowResponse.GetAmount().Denom)
	require.Equal(t, "77", totalEscrowResponse.GetAmount().Amount.String())
	requireTranswapServiceMethodsExecuted(t, reflect.TypeOf((*transwaptypes.QueryServer)(nil)).Elem(), executedQueries)
}

func transwapRuntimeRefundRecord(
	receiver string,
	status transwaptypes.RefundStatus,
	retryCount uint32,
	sequence uint64,
) *transwaptypes.RefundRecord {
	const denom = "urefund"
	return &transwaptypes.RefundRecord{
		Id:                       transwaptypes.RefundID(transwaptypes.PortID, "channel-0", sequence),
		Status:                   status,
		RefundSourcePort:         transwaptypes.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    transwaptypes.Token{Denom: transwaptypes.NewDenom(denom), Amount: "10"},
		Receiver:                 receiver,
		ClaimAddress:             receiver,
		Memo:                     "runtime router test",
		ExchangeId:               "1",
		OriginalFee:              sdk.NewInt64Coin(denom, 0),
		OriginalTimeoutTimestamp: uint64(time.Unix(1_700_003_600, 0).UnixNano()), //nolint:gosec // fixed positive test time.
		OriginalTimeoutHeight: &transwaptypes.RefundHeight{
			RevisionNumber: 1,
			RevisionHeight: 1,
		},
		OriginalOutputPort:             transwaptypes.PortID,
		OriginalOutputChannel:          "channel-0",
		OriginalOutputSequence:         sequence,
		RetryCount:                     retryCount,
		OriginalOutputPacketCommitment: make([]byte, sha256.Size),
		VolumeReservation: &bextypes.VolumeReservation{
			ExchangeId:             1,
			Direction:              bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
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
	response gogoRuntimeMessage,
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
	require.NoError(t, response.Unmarshal(result.Data))
	executed[method] = struct{}{}
	t.Logf("runtime Msg/%s => %v", method, response)
}

func executeTranswapRuntimeQuery(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request gogoRuntimeMessage,
	response gogoRuntimeMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "TransSwap Query RPC executed more than once")
	handler := testApp.GRPCQueryRouter().Route("/guru.transwap.v1.Query/" + method)
	require.NotNil(t, handler, "TransSwap Query RPC %s is not registered", method)
	requestBytes, err := request.Marshal()
	require.NoError(t, err)
	queryResult, err := handler(ctx, &abci.RequestQuery{Data: requestBytes, Height: ctx.BlockHeight()})
	require.NoError(t, err)
	require.NotNil(t, queryResult)
	require.Equal(t, ctx.BlockHeight(), queryResult.Height)
	require.NoError(t, response.Unmarshal(queryResult.Value))
	executed[method] = struct{}{}
	t.Logf("runtime Query/%s => %v", method, response)
}

func requireTranswapServiceMethodsExecuted(t *testing.T, service reflect.Type, executed map[string]struct{}) {
	t.Helper()
	require.Len(t, executed, service.NumMethod())
	for i := 0; i < service.NumMethod(); i++ {
		require.Contains(t, executed, service.Method(i).Name, "runtime RPC coverage must track the generated TransSwap service interface")
	}
}
