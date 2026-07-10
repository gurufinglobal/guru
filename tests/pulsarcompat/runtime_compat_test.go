package pulsarcompat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	legacyproto "github.com/golang/protobuf/proto" //nolint:staticcheck // This test intentionally exercises legacy protobuf wire compatibility.
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ legacyproto.Message = (*oraclev1.QueryParamsResponse)(nil)

func TestLegacyProtoV1MarshalCompatibility(t *testing.T) {
	in := &oraclev1.QueryLatestValueResponse{
		Value: &oraclev1.OracleValue{
			Symbol:        "BTC/USD",
			ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "65000.1234",
			BlockHeight:   10,
			BlockTimeUnix: 20,
		},
	}

	bz, err := legacyproto.Marshal(in)
	if err != nil {
		t.Fatalf("legacy proto marshal failed: %v", err)
	}
	if len(bz) == 0 {
		t.Fatalf("legacy proto marshal returned empty bytes")
	}

	var out oraclev1.QueryLatestValueResponse
	if err := legacyproto.Unmarshal(bz, &out); err != nil {
		t.Fatalf("legacy proto unmarshal failed: %v", err)
	}
	if out.Value == nil || out.Value.Symbol != in.Value.Symbol || out.Value.Value != in.Value.Value {
		t.Fatalf("unexpected legacy proto round-trip result: got=%+v want=%+v", out.Value, in.Value)
	}
}

type mockGatewayQueryServer struct {
	oraclev1.UnimplementedQueryServer
}

func (mockGatewayQueryServer) Params(context.Context, *oraclev1.QueryParamsRequest) (*oraclev1.QueryParamsResponse, error) {
	return &oraclev1.QueryParamsResponse{Params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100}}, nil
}

func (mockGatewayQueryServer) ActiveTasks(context.Context, *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
	return &oraclev1.QueryActiveTasksResponse{}, nil
}

func (mockGatewayQueryServer) Task(_ context.Context, req *oraclev1.QueryTaskRequest) (*oraclev1.QueryTaskResponse, error) {
	return &oraclev1.QueryTaskResponse{
		Task: &oraclev1.OracleTask{
			Symbol:             req.Symbol,
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	}, nil
}

func (mockGatewayQueryServer) LatestValue(_ context.Context, req *oraclev1.QueryLatestValueRequest) (*oraclev1.QueryLatestValueResponse, error) {
	return &oraclev1.QueryLatestValueResponse{
		Value: &oraclev1.OracleValue{
			Symbol:        req.Symbol,
			ValueType:     oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "42000.01",
			BlockHeight:   11,
			BlockTimeUnix: 22,
		},
	}, nil
}

func (mockGatewayQueryServer) LatestValues(context.Context, *oraclev1.QueryLatestValuesRequest) (*oraclev1.QueryLatestValuesResponse, error) {
	return &oraclev1.QueryLatestValuesResponse{}, nil
}

func (mockGatewayQueryServer) History(context.Context, *oraclev1.QueryHistoryRequest) (*oraclev1.QueryHistoryResponse, error) {
	return &oraclev1.QueryHistoryResponse{}, nil
}

func TestGRPCGatewayHandlesPulsarQueryMessages(t *testing.T) {
	mux := runtime.NewServeMux()
	if err := oraclev1.RegisterQueryHandlerServer(context.Background(), mux, mockGatewayQueryServer{}); err != nil {
		t.Fatalf("register query gateway handler: %v", err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/guru/oracle/v1/params")
	if err != nil {
		t.Fatalf("GET /params failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /params status: got=%d", resp.StatusCode)
	}

	var paramsResp struct {
		Params struct {
			MinValidators uint32 `json:"min_validators"`
			MinSources    uint32 `json:"min_sources"`
			HistoryLimit  uint32 `json:"history_limit"`
		} `json:"params"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&paramsResp); err != nil {
		t.Fatalf("decode /params response: %v", err)
	}
	if paramsResp.Params.MinValidators != 1 || paramsResp.Params.MinSources != 3 || paramsResp.Params.HistoryLimit != 100 {
		t.Fatalf("unexpected /params response payload: %+v", paramsResp)
	}

	resp2, err := http.Get(srv.URL + "/guru/oracle/v1/values/BTC-USD")
	if err != nil {
		t.Fatalf("GET /values/{symbol} failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /values/{symbol} status: got=%d", resp2.StatusCode)
	}

	var valueResp struct {
		Value struct {
			Symbol string `json:"symbol"`
			Value  string `json:"value"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&valueResp); err != nil {
		t.Fatalf("decode /values/{symbol} response: %v", err)
	}
	if valueResp.Value.Symbol != "BTC-USD" || valueResp.Value.Value != "42000.01" {
		t.Fatalf("unexpected /values/{symbol} payload: %+v", valueResp)
	}
}

type mockMsgServer struct {
	oraclev1.UnimplementedMsgServer
}

func (mockMsgServer) UpsertTask(_ context.Context, req *oraclev1.MsgUpsertTask) (*oraclev1.MsgUpsertTaskResponse, error) {
	if req.Moderator == "" {
		return nil, status.Error(codes.InvalidArgument, "moderator is required")
	}
	if req.Task == nil {
		return nil, status.Error(codes.InvalidArgument, "task is required")
	}
	return &oraclev1.MsgUpsertTaskResponse{}, nil
}

func TestGRPCMsgClientServerWithPulsarMessages(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	oraclev1.RegisterMsgServer(s, mockMsgServer{})
	defer s.Stop()

	go func() {
		_ = s.Serve(lis)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := oraclev1.NewMsgClient(conn)
	_, err = client.UpsertTask(context.Background(), &oraclev1.MsgUpsertTask{
		Moderator: "guru1moderator",
		Task: &oraclev1.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	})
	if err != nil {
		t.Fatalf("grpc UpsertTask failed: %v", err)
	}
}

func TestInterfaceRegistryCanUnpackPulsarMsgAndResponse(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	oracletypes.RegisterInterfaces(registry)

	msgIn := &oraclev1.MsgUpsertTask{
		Moderator: "guru1moderator",
		Task: &oraclev1.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	}

	msgAny, err := codectypes.NewAnyWithValue(msgIn)
	if err != nil {
		t.Fatalf("pack sdk.Msg into Any failed: %v", err)
	}

	var msgOut sdk.Msg
	if err := registry.UnpackAny(msgAny, &msgOut); err != nil {
		t.Fatalf("unpack sdk.Msg from Any failed: %v", err)
	}
	unpackedMsg, ok := msgOut.(*oraclev1.MsgUpsertTask)
	if !ok {
		t.Fatalf("unexpected unpacked sdk.Msg type: %T", msgOut)
	}
	if unpackedMsg.Moderator != msgIn.Moderator {
		t.Fatalf("unexpected unpacked moderator: got=%s want=%s", unpackedMsg.Moderator, msgIn.Moderator)
	}

	respAny, err := codectypes.NewAnyWithValue(&oraclev1.MsgUpsertTaskResponse{})
	if err != nil {
		t.Fatalf("pack tx.MsgResponse into Any failed: %v", err)
	}

	var respOut txtypes.MsgResponse
	if err := registry.UnpackAny(respAny, &respOut); err != nil {
		t.Fatalf("unpack tx.MsgResponse from Any failed: %v", err)
	}
	if _, ok := respOut.(*oraclev1.MsgUpsertTaskResponse); !ok {
		t.Fatalf("unexpected unpacked tx.MsgResponse type: %T", respOut)
	}
}

func TestTxConfigDecodesPulsarMessagesWithNestedFields(t *testing.T) {
	encodingConfig := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	banktypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	constitutiontypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	addrBytes := make([]byte, 20)
	for i := range addrBytes {
		addrBytes[i] = byte(i + 1)
	}
	moderator, err := encodingConfig.InterfaceRegistry.SigningContext().AddressCodec().BytesToString(addrBytes)
	if err != nil {
		t.Fatalf("build moderator address: %v", err)
	}
	receiverBytes := make([]byte, 20)
	for i := range receiverBytes {
		receiverBytes[i] = byte(i + 51)
	}
	receiver, err := encodingConfig.InterfaceRegistry.SigningContext().AddressCodec().BytesToString(receiverBytes)
	if err != nil {
		t.Fatalf("build receiver address: %v", err)
	}

	for _, name := range []protoreflect.FullName{
		"guru.oracle.v1.MsgUpsertTask",
		"guru.oracle.v1.OracleTask",
		"guru.oracle.v1.Params",
		"guru.constitution.v1.MsgUpdateSeparationRatio",
		"guru.constitution.v1.SeparationRatio",
	} {
		if _, err := protoregistry.GlobalFiles.FindDescriptorByName(name); err != nil {
			t.Fatalf("global descriptor %s not registered: %v", name, err)
		}
	}

	messages := []sdk.Msg{
		&banktypes.MsgSend{
			FromAddress: moderator,
			ToAddress:   receiver,
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("agxn", 1)),
		},
		&oraclev1.MsgUpsertTask{
			Moderator: moderator,
			Task: &oraclev1.OracleTask{
				Symbol:             "BTC/USD",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 5,
			},
		},
		&oraclev1.MsgUpdateParams{
			Moderator: moderator,
			Params: &oraclev1.Params{
				MinValidators: 1,
				MinSources:    3,
				HistoryLimit:  100,
			},
		},
		&constitutionv1.MsgUpdateSeparationRatio{
			Moderator: moderator,
			SeparationRatio: &constitutionv1.SeparationRatio{
				BasePpm:       100000,
				BurnPpm:       200000,
				ValidatorsPpm: 700000,
			},
		},
	}

	for _, msg := range messages {
		builder := encodingConfig.TxConfig.NewTxBuilder()
		if err := builder.SetMsgs(msg); err != nil {
			t.Fatalf("set msg %T: %v", msg, err)
		}
		txBytes, err := encodingConfig.TxConfig.TxEncoder()(builder.GetTx())
		if err != nil {
			t.Fatalf("encode tx %T: %v", msg, err)
		}
		decodedTx, err := encodingConfig.TxConfig.TxDecoder()(txBytes)
		if err != nil {
			t.Fatalf("decode tx %T: %v", msg, err)
		}
		decodedMsgs := decodedTx.GetMsgs()
		if len(decodedMsgs) != 1 {
			t.Fatalf("decode tx %T message count: got=%d", msg, len(decodedMsgs))
		}
		intoAny, ok := decodedTx.(interface{ AsAny() *codectypes.Any })
		if !ok {
			t.Fatalf("decoded tx %T does not implement the SDK query tx Any bridge", decodedTx)
		}
		packedTx := intoAny.AsAny()
		if packedTx == nil || packedTx.TypeUrl != "/cosmos.tx.v1beta1.Tx" {
			t.Fatalf("decoded tx %T produced an invalid Any: %+v", decodedTx, packedTx)
		}
		sigTx, ok := decodedTx.(authsigning.SigVerifiableTx)
		if !ok {
			t.Fatalf("decoded tx %T does not implement SigVerifiableTx", decodedTx)
		}
		signers, err := sigTx.GetSigners()
		if err != nil {
			t.Fatalf("get signers for %T: %v", msg, err)
		}
		if len(signers) == 0 {
			t.Fatalf("get signers for %T returned no signers", msg)
		}
		if _, err := decodedTx.GetMsgsV2(); err != nil {
			t.Fatalf("get msgs v2 for %T: %v", msg, err)
		}
		wrapped, err := encodingConfig.TxConfig.WrapTxBuilder(decodedTx)
		if err != nil {
			t.Fatalf("wrap decoded tx %T: %v", decodedTx, err)
		}
		if _, err := encodingConfig.TxConfig.TxEncoder()(wrapped.GetTx()); err != nil {
			t.Fatalf("encode wrapped tx %T: %v", msg, err)
		}
	}
}
