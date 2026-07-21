package app

import (
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestOracleAllMsgAndQueryServicesExecuteThroughRuntimeRouters(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		sims.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	ctx := testApp.NewNextBlockContext(cmtproto.Header{
		ChainID: appparams.SDKChainID,
		Height:  10,
		Time:    time.Unix(1_700_000_000, 0),
	})
	moderator := wiringAddress(t, 0x41)
	require.NoError(t, testApp.ConstitutionKeeper.SetModeratorAddress(ctx, moderator))

	executedMsgs := map[string]struct{}{}
	executeOracleRuntimeMsg(t, testApp, ctx, "UpdateParams", &oracletypes.MsgUpdateParams{
		Moderator: moderator,
		Params: &oracletypes.Params{
			MinValidators: 2,
			MinSources:    4,
			HistoryLimit:  5,
		},
	}, &oracletypes.MsgUpdateParamsResponse{}, executedMsgs)
	executeOracleRuntimeMsg(t, testApp, ctx, "UpsertTask", &oracletypes.MsgUpsertTask{
		Moderator: moderator,
		Task: &oracletypes.OracleTask{
			Symbol:             " btc/usd ",
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 3,
		},
	}, &oracletypes.MsgUpsertTaskResponse{}, executedMsgs)

	require.NoError(t, testApp.OracleKeeper.ApplyOracleValues(ctx, []*oracletypes.OracleValue{
		{
			Symbol:        "BTC/USD",
			ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "100.25",
			BlockHeight:   8,
			BlockTimeUnix: ctx.BlockTime().Unix() - 2,
		},
		{
			Symbol:        " btc/usd ",
			ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "101.5",
			BlockHeight:   9,
			BlockTimeUnix: ctx.BlockTime().Unix() - 1,
		},
	}))

	executedQueries := map[string]struct{}{}
	paramsResponse := &oracletypes.QueryParamsResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "Params", &oracletypes.QueryParamsRequest{}, paramsResponse, executedQueries)
	require.Equal(t, uint32(2), paramsResponse.GetParams().GetMinValidators())
	require.Equal(t, uint32(4), paramsResponse.GetParams().GetMinSources())
	require.Equal(t, uint32(5), paramsResponse.GetParams().GetHistoryLimit())

	activeTasksResponse := &oracletypes.QueryActiveTasksResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "ActiveTasks", &oracletypes.QueryActiveTasksRequest{}, activeTasksResponse, executedQueries)
	require.Len(t, activeTasksResponse.GetTasks(), 1)
	require.Equal(t, "BTC/USD", activeTasksResponse.GetTasks()[0].GetSymbol())
	require.True(t, activeTasksResponse.GetTasks()[0].GetEnabled())
	require.Equal(t, uint32(3), activeTasksResponse.GetTasks()[0].GetSubmissionInterval())
	require.Equal(t, uint64(1), activeTasksResponse.GetPagination().GetTotal())

	taskResponse := &oracletypes.QueryTaskResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "Task", &oracletypes.QueryTaskRequest{Symbol: " btc/usd "}, taskResponse, executedQueries)
	require.Equal(t, "BTC/USD", taskResponse.GetTask().GetSymbol())
	require.Equal(t, oracletypes.ValueType_VALUE_TYPE_NUMERIC, taskResponse.GetTask().GetValueType())

	latestValueResponse := &oracletypes.QueryLatestValueResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "LatestValue", &oracletypes.QueryLatestValueRequest{Symbol: "btc/usd"}, latestValueResponse, executedQueries)
	require.Equal(t, "BTC/USD", latestValueResponse.GetValue().GetSymbol())
	require.Equal(t, "101.5", latestValueResponse.GetValue().GetValue())
	require.Equal(t, int64(9), latestValueResponse.GetValue().GetBlockHeight())

	latestValuesResponse := &oracletypes.QueryLatestValuesResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "LatestValues", &oracletypes.QueryLatestValuesRequest{}, latestValuesResponse, executedQueries)
	require.Len(t, latestValuesResponse.GetValues(), 1)
	require.Equal(t, "BTC/USD", latestValuesResponse.GetValues()[0].GetSymbol())
	require.Equal(t, "101.5", latestValuesResponse.GetValues()[0].GetValue())
	require.Equal(t, uint64(1), latestValuesResponse.GetPagination().GetTotal())

	historyResponse := &oracletypes.QueryHistoryResponse{}
	executeOracleRuntimeQuery(t, testApp, ctx, "History", &oracletypes.QueryHistoryRequest{Symbol: "BTC/USD"}, historyResponse, executedQueries)
	require.Equal(t, "BTC/USD", historyResponse.GetHistory().GetSymbol())
	require.Len(t, historyResponse.GetHistory().GetValues(), 2)
	require.Equal(t, "100.25", historyResponse.GetHistory().GetValues()[0].GetValue())
	require.Equal(t, "101.5", historyResponse.GetHistory().GetValues()[1].GetValue())
	require.Equal(t, uint64(2), historyResponse.GetPagination().GetTotal())

	executeOracleRuntimeMsg(t, testApp, ctx, "RemoveTask", &oracletypes.MsgRemoveTask{
		Moderator: moderator,
		Symbol:    " btc/usd ",
	}, &oracletypes.MsgRemoveTaskResponse{}, executedMsgs)

	_, err := testApp.OracleKeeper.GetTask(ctx, "BTC/USD")
	require.Error(t, err)
	tasks, err := testApp.OracleKeeper.ListTasks(ctx, false)
	require.NoError(t, err)
	require.Empty(t, tasks)
	schedule, err := testApp.OracleKeeper.ListTaskSchedule(ctx)
	require.NoError(t, err)
	require.Empty(t, schedule)

	requireOracleRuntimeMethodsExecuted(t, oracletypes.Msg_serviceDesc.Methods, executedMsgs)
	requireOracleRuntimeMethodsExecuted(t, oracletypes.Query_serviceDesc.Methods, executedQueries)
}

func executeOracleRuntimeMsg(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request sdk.Msg,
	response oracleRuntimeWireMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "Oracle Msg RPC executed more than once")
	handler := testApp.MsgServiceRouter().Handler(request)
	require.NotNil(t, handler, "Oracle Msg RPC %s is not registered", method)
	result, err := handler(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MsgResponses, 1)
	require.Equal(t, "/guru.oracle.v1.Msg"+method+"Response", result.MsgResponses[0].TypeUrl)
	require.NoError(t, response.Unmarshal(result.Data))
	executed[method] = struct{}{}
	t.Logf("runtime Msg/%s => %v", method, response)
}

func executeOracleRuntimeQuery(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	method string,
	request oracleRuntimeWireMessage,
	response oracleRuntimeWireMessage,
	executed map[string]struct{},
) {
	t.Helper()
	require.NotContains(t, executed, method, "Oracle Query RPC executed more than once")
	handler := testApp.GRPCQueryRouter().Route("/guru.oracle.v1.Query/" + method)
	require.NotNil(t, handler, "Oracle Query RPC %s is not registered", method)
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

type oracleRuntimeWireMessage interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

func requireOracleRuntimeMethodsExecuted(t *testing.T, methods []grpc.MethodDesc, executed map[string]struct{}) {
	t.Helper()
	missing := make([]string, 0)
	for _, method := range methods {
		if _, ok := executed[method.MethodName]; !ok {
			missing = append(missing, method.MethodName)
		}
	}
	require.Empty(t, missing, "Oracle runtime RPC coverage is missing service descriptor methods")
	require.Len(t, executed, len(methods), "Oracle runtime RPC coverage has methods outside the service descriptor")
}
