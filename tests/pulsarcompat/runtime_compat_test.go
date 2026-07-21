package pulsarcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	gogogateway "github.com/cosmos/gogogateway"
	legacyproto "github.com/golang/protobuf/proto" //nolint:staticcheck // This test intentionally exercises legacy protobuf wire compatibility.
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/wrapperspb"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	pulsarbexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	pulsarconstitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	pulsaroraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	pulsartranswapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ legacyproto.Message = (*pulsaroraclev1.QueryParamsResponse)(nil)

func TestLegacyProtoV1MarshalCompatibility(t *testing.T) {
	in := &pulsaroraclev1.QueryLatestValueResponse{
		Value: &pulsaroraclev1.OracleValue{
			Symbol:        "BTC/USD",
			ValueType:     pulsaroraclev1.ValueType_VALUE_TYPE_NUMERIC,
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

	var out pulsaroraclev1.QueryLatestValueResponse
	if err := legacyproto.Unmarshal(bz, &out); err != nil {
		t.Fatalf("legacy proto unmarshal failed: %v", err)
	}
	if out.Value == nil || out.Value.Symbol != in.Value.Symbol || out.Value.Value != in.Value.Value {
		t.Fatalf("unexpected legacy proto round-trip result: got=%+v want=%+v", out.Value, in.Value)
	}
}

type mockGatewayQueryServer struct {
	oracletypes.UnimplementedQueryServer
}

func (mockGatewayQueryServer) Params(context.Context, *oracletypes.QueryParamsRequest) (*oracletypes.QueryParamsResponse, error) {
	return &oracletypes.QueryParamsResponse{Params: &oracletypes.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100}}, nil
}

func (mockGatewayQueryServer) ActiveTasks(context.Context, *oracletypes.QueryActiveTasksRequest) (*oracletypes.QueryActiveTasksResponse, error) {
	return &oracletypes.QueryActiveTasksResponse{}, nil
}

func (mockGatewayQueryServer) Task(_ context.Context, req *oracletypes.QueryTaskRequest) (*oracletypes.QueryTaskResponse, error) {
	return &oracletypes.QueryTaskResponse{
		Task: &oracletypes.OracleTask{
			Symbol:             req.Symbol,
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	}, nil
}

func (mockGatewayQueryServer) LatestValue(_ context.Context, req *oracletypes.QueryLatestValueRequest) (*oracletypes.QueryLatestValueResponse, error) {
	return &oracletypes.QueryLatestValueResponse{
		Value: &oracletypes.OracleValue{
			Symbol:        req.Symbol,
			ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "42000.01",
			BlockHeight:   11,
			BlockTimeUnix: 22,
		},
	}, nil
}

func (mockGatewayQueryServer) LatestValues(_ context.Context, req *oracletypes.QueryLatestValuesRequest) (*oracletypes.QueryLatestValuesResponse, error) {
	page := req.GetPagination()
	if page != nil && (string(page.GetKey()) != string([]byte{0x01, 0x02}) || page.GetOffset() != 3 || page.GetLimit() != 4 || !page.GetCountTotal() || !page.GetReverse()) {
		return nil, status.Error(codes.InvalidArgument, "unexpected pagination request")
	}
	return &oracletypes.QueryLatestValuesResponse{
		Values: []*oracletypes.OracleValue{{
			Symbol:        "BTC/USD",
			ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "65000.25",
			BlockHeight:   101,
			BlockTimeUnix: 202,
		}},
		Pagination: &sdkquery.PageResponse{NextKey: []byte{0x03, 0x04}, Total: 9},
	}, nil
}

func (mockGatewayQueryServer) History(context.Context, *oracletypes.QueryHistoryRequest) (*oracletypes.QueryHistoryResponse, error) {
	return &oracletypes.QueryHistoryResponse{}, nil
}

type mockConstitutionQueryServer struct {
	constitutiontypes.UnimplementedQueryServer
}

func (mockConstitutionQueryServer) Params(context.Context, *constitutiontypes.QueryParamsRequest) (*constitutiontypes.QueryParamsResponse, error) {
	minBond := sdk.NewInt64Coin(appparams.BaseDenom, 1_000_000)
	return &constitutiontypes.QueryParamsResponse{
		Params: &constitutiontypes.Params{MinValidatorBondAmount: &minBond},
	}, nil
}

func TestInternalGatewayPreservesPublicPulsarJSON(t *testing.T) {
	marshaler := &gogogateway.JSONPb{EmitDefaults: true, OrigName: true}
	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler))
	if err := oracletypes.RegisterQueryHandlerServer(context.Background(), mux, mockGatewayQueryServer{}); err != nil {
		t.Fatalf("register query gateway handler: %v", err)
	}
	if err := constitutiontypes.RegisterQueryHandlerServer(context.Background(), mux, &mockConstitutionQueryServer{}); err != nil {
		t.Fatalf("register constitution query gateway handler: %v", err)
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

	paramsJSON, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /params response: %v", err)
	}
	assertPublicGatewayJSON(t, paramsJSON, &pulsaroraclev1.QueryParamsResponse{
		Params: &pulsaroraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
	})

	resp2, err := http.Get(srv.URL + "/guru/oracle/v1/values/BTC-USD")
	if err != nil {
		t.Fatalf("GET /values/{symbol} failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /values/{symbol} status: got=%d", resp2.StatusCode)
	}

	valueJSON, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read /values/{symbol} response: %v", err)
	}
	assertPublicGatewayJSON(t, valueJSON, &pulsaroraclev1.QueryLatestValueResponse{
		Value: &pulsaroraclev1.OracleValue{
			Symbol:        "BTC-USD",
			ValueType:     pulsaroraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "42000.01",
			BlockHeight:   11,
			BlockTimeUnix: 22,
		},
	})

	resp3, err := http.Get(srv.URL + "/guru/constitution/v1/params")
	if err != nil {
		t.Fatalf("GET constitution /params failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("unexpected constitution /params status: got=%d", resp3.StatusCode)
	}
	constitutionJSON, err := io.ReadAll(resp3.Body)
	if err != nil {
		t.Fatalf("read constitution /params response: %v", err)
	}
	assertPublicGatewayJSON(t, constitutionJSON, &pulsarconstitutionv1.QueryParamsResponse{
		Params: &pulsarconstitutionv1.Params{
			MinValidatorBondAmount: &basev1beta1.Coin{Denom: appparams.BaseDenom, Amount: "1000000"},
		},
	})
}

func assertPublicGatewayJSON(t *testing.T, actual []byte, expected legacyproto.Message) {
	t.Helper()

	expectedJSON, err := (&gogogateway.JSONPb{EmitDefaults: true, OrigName: true}).Marshal(expected)
	if err != nil {
		t.Fatalf("marshal public Pulsar gateway response: %v", err)
	}
	var actualValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode internal gateway JSON %q: %v", actual, err)
	}
	var expectedValue any
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatalf("decode public Pulsar gateway JSON %q: %v", expectedJSON, err)
	}
	if !jsonValuesEqual(actualValue, expectedValue) {
		t.Fatalf("gateway JSON differs: internal=%s public=%s", actual, expectedJSON)
	}
}

func jsonValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

type mockMsgServer struct {
	oracletypes.UnimplementedMsgServer
}

type mockSidecarServer struct {
	oracletypes.UnimplementedOracleSidecarServer
}

func (mockSidecarServer) GetSamples(_ context.Context, req *oracletypes.GetSamplesRequest) (*oracletypes.GetSamplesResponse, error) {
	if req.GetHeight() != 303 || len(req.GetTasks()) != 1 || req.GetTasks()[0].GetSymbol() != "ATOM/USD" {
		return nil, status.Error(codes.InvalidArgument, "unexpected sample request")
	}
	return &oracletypes.GetSamplesResponse{Symbols: []*oracletypes.OracleSymbolSamples{{
		Symbol: "ATOM/USD",
		Samples: []*oracletypes.OracleSample{{
			Source:         "source-a",
			ValueType:      oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:          "12.34",
			SampleTimeUnix: 404,
		}},
	}}}, nil
}

func (mockMsgServer) UpsertTask(_ context.Context, req *oracletypes.MsgUpsertTask) (*oracletypes.MsgUpsertTaskResponse, error) {
	if req.Moderator == "" {
		return nil, status.Error(codes.InvalidArgument, "moderator is required")
	}
	if req.Task == nil {
		return nil, status.Error(codes.InvalidArgument, "task is required")
	}
	return &oracletypes.MsgUpsertTaskResponse{}, nil
}

func TestPulsarGRPCClientCallsInternalGogoServer(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	oracletypes.RegisterMsgServer(s, &mockMsgServer{})
	oracletypes.RegisterQueryServer(s, mockGatewayQueryServer{})
	oracletypes.RegisterOracleSidecarServer(s, mockSidecarServer{})
	constitutiontypes.RegisterQueryServer(s, &mockConstitutionQueryServer{})
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

	client := pulsaroraclev1.NewMsgClient(conn)
	_, err = client.UpsertTask(context.Background(), &pulsaroraclev1.MsgUpsertTask{
		Moderator: "guru1moderator",
		Task: &pulsaroraclev1.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          pulsaroraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		},
	})
	if err != nil {
		t.Fatalf("grpc UpsertTask failed: %v", err)
	}

	queryClient := pulsaroraclev1.NewQueryClient(conn)
	values, err := queryClient.LatestValues(context.Background(), &pulsaroraclev1.QueryLatestValuesRequest{
		Pagination: &queryv1beta1.PageRequest{
			Key:        []byte{0x01, 0x02},
			Offset:     3,
			Limit:      4,
			CountTotal: true,
			Reverse:    true,
		},
	})
	if err != nil {
		t.Fatalf("grpc LatestValues failed: %v", err)
	}
	if len(values.GetValues()) != 1 || values.GetValues()[0].GetBlockHeight() != 101 || values.GetValues()[0].GetBlockTimeUnix() != 202 {
		t.Fatalf("unexpected LatestValues payload: %+v", values)
	}
	if values.GetPagination() == nil || string(values.GetPagination().GetNextKey()) != string([]byte{0x03, 0x04}) || values.GetPagination().GetTotal() != 9 {
		t.Fatalf("unexpected LatestValues pagination: %+v", values.GetPagination())
	}

	constitutionClient := pulsarconstitutionv1.NewQueryClient(conn)
	constitutionParams, err := constitutionClient.Params(context.Background(), &pulsarconstitutionv1.QueryParamsRequest{})
	if err != nil {
		t.Fatalf("grpc constitution Params failed: %v", err)
	}
	coin := constitutionParams.GetParams().GetMinValidatorBondAmount()
	if coin.GetDenom() != appparams.BaseDenom || coin.GetAmount() != "1000000" {
		t.Fatalf("unexpected constitution min bond: %+v", coin)
	}

	sidecarClient := pulsaroraclev1.NewOracleSidecarClient(conn)
	samples, err := sidecarClient.GetSamples(context.Background(), &pulsaroraclev1.GetSamplesRequest{
		Height: 303,
		Tasks: []*pulsaroraclev1.OracleTask{{
			Symbol:             "ATOM/USD",
			ValueType:          pulsaroraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 5,
		}},
	})
	if err != nil {
		t.Fatalf("grpc GetSamples failed: %v", err)
	}
	if len(samples.GetSymbols()) != 1 || len(samples.GetSymbols()[0].GetSamples()) != 1 || samples.GetSymbols()[0].GetSamples()[0].GetSampleTimeUnix() != 404 {
		t.Fatalf("unexpected GetSamples payload: %+v", samples)
	}
}

func TestInterfaceRegistryCanUnpackInternalGogoMsgAndResponse(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	oracletypes.RegisterInterfaces(registry)

	msgIn := &oracletypes.MsgUpsertTask{
		Moderator: "guru1moderator",
		Task: &oracletypes.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
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
	unpackedMsg, ok := msgOut.(*oracletypes.MsgUpsertTask)
	if !ok {
		t.Fatalf("unexpected unpacked sdk.Msg type: %T", msgOut)
	}
	if unpackedMsg.Moderator != msgIn.Moderator {
		t.Fatalf("unexpected unpacked moderator: got=%s want=%s", unpackedMsg.Moderator, msgIn.Moderator)
	}

	respAny, err := codectypes.NewAnyWithValue(&oracletypes.MsgUpsertTaskResponse{})
	if err != nil {
		t.Fatalf("pack tx.MsgResponse into Any failed: %v", err)
	}

	var respOut txtypes.MsgResponse
	if err := registry.UnpackAny(respAny, &respOut); err != nil {
		t.Fatalf("unpack tx.MsgResponse from Any failed: %v", err)
	}
	if _, ok := respOut.(*oracletypes.MsgUpsertTaskResponse); !ok {
		t.Fatalf("unexpected unpacked tx.MsgResponse type: %T", respOut)
	}
}

func TestStandardTxConfigDecodesInternalGogoMessagesWithNestedFields(t *testing.T) {
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
		&oracletypes.MsgUpsertTask{
			Moderator: moderator,
			Task: &oracletypes.OracleTask{
				Symbol:             "BTC/USD",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 5,
			},
		},
		&oracletypes.MsgUpdateParams{
			Moderator: moderator,
			Params: &oracletypes.Params{
				MinValidators: 1,
				MinSources:    3,
				HistoryLimit:  100,
			},
		},
		&constitutiontypes.MsgUpdateSeparationRatio{
			Moderator: moderator,
			SeparationRatio: &constitutiontypes.SeparationRatio{
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

func TestStandardTxConfigDecodesPublicPulsarBexAndTransSwapTransactions(t *testing.T) {
	encodingConfig := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	bextypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	transwaptypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	authorityBytes := bytes.Repeat([]byte{0x61}, 20)
	feePayerBytes := bytes.Repeat([]byte{0x62}, 20)
	addressCodec := encodingConfig.InterfaceRegistry.SigningContext().AddressCodec()
	authority, err := addressCodec.BytesToString(authorityBytes)
	if err != nil {
		t.Fatalf("encode authority address: %v", err)
	}
	feePayer, err := addressCodec.BytesToString(feePayerBytes)
	if err != nil {
		t.Fatalf("encode fee payer address: %v", err)
	}

	tests := []struct {
		name       string
		message    protov2.Message
		fullName   protoreflect.FullName
		assertGogo func(*testing.T, sdk.Msg)
	}{
		{
			name: "BEX map and wrapper fields",
			message: &pulsarbexv1.MsgUpdateExchange{
				AdminAddress:     authority,
				ExchangeId:       7,
				ExpectedRevision: 3,
				Patch: &pulsarbexv1.ExchangeUpdatePatch{
					DenomA:   wrapperspb.String("uatom"),
					Metadata: map[string]string{"network": "test", "tier": "one"},
				},
			},
			fullName: "guru.bex.v1.MsgUpdateExchange",
			assertGogo: func(t *testing.T, message sdk.Msg) {
				t.Helper()
				msg, ok := message.(*bextypes.MsgUpdateExchange)
				if !ok {
					t.Fatalf("decoded BEX message type = %T, want *types.MsgUpdateExchange", message)
				}
				if msg.GetPatch().GetDenomA().GetValue() != "uatom" ||
					msg.GetPatch().GetMetadata()["network"] != "test" ||
					msg.GetPatch().GetMetadata()["tier"] != "one" {
					t.Fatalf("decoded BEX message lost public nested fields: %+v", msg.GetPatch())
				}
			},
		},
		{
			name: "TransSwap nested params",
			message: &pulsartranswapv1.MsgUpdateParams{
				Authority: authority,
				Params: &pulsartranswapv1.Params{
					MaxRefundRetries:     3,
					RefundTimeoutWindow:  60,
					MinRelaySafetyMargin: 5,
				},
			},
			fullName: "guru.transwap.v1.MsgUpdateParams",
			assertGogo: func(t *testing.T, message sdk.Msg) {
				t.Helper()
				msg, ok := message.(*transwaptypes.MsgUpdateParams)
				if !ok {
					t.Fatalf("decoded TransSwap message type = %T, want *types.MsgUpdateParams", message)
				}
				if msg.GetParams().GetMaxRefundRetries() != 3 ||
					msg.GetParams().GetRefundTimeoutWindow() != 60 ||
					msg.GetParams().GetMinRelaySafetyMargin() != 5 {
					t.Fatalf("decoded TransSwap message lost public nested params: %+v", msg.GetParams())
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			txBytes := marshalPublicPulsarTx(t, test.message, feePayer)
			decoded, err := encodingConfig.TxConfig.TxDecoder()(txBytes)
			if err != nil {
				t.Fatalf("decode public Pulsar transaction: %v", err)
			}
			messages := decoded.GetMsgs()
			if len(messages) != 1 {
				t.Fatalf("decoded message count = %d, want 1", len(messages))
			}
			test.assertGogo(t, messages[0])

			messagesV2, err := decoded.GetMsgsV2()
			if err != nil {
				t.Fatalf("adapt decoded public message to protobuf v2: %v", err)
			}
			if len(messagesV2) != 1 || messagesV2[0].ProtoReflect().Descriptor().FullName() != test.fullName {
				t.Fatalf("decoded protobuf v2 messages = %+v, want %s", messagesV2, test.fullName)
			}

			sigTx, ok := decoded.(authsigning.SigVerifiableTx)
			if !ok {
				t.Fatalf("decoded transaction %T does not implement SigVerifiableTx", decoded)
			}
			signers, err := sigTx.GetSigners()
			if err != nil {
				t.Fatalf("extract decoded transaction signers: %v", err)
			}
			if len(signers) != 2 || !bytes.Equal(signers[0], authorityBytes) || !bytes.Equal(signers[1], feePayerBytes) {
				t.Fatalf("decoded signers = %X, want authority=%X fee_payer=%X", signers, authorityBytes, feePayerBytes)
			}

			wrapped, err := encodingConfig.TxConfig.WrapTxBuilder(decoded)
			if err != nil {
				t.Fatalf("wrap decoded standard transaction: %v", err)
			}
			reencoded, err := encodingConfig.TxConfig.TxEncoder()(wrapped.GetTx())
			if err != nil {
				t.Fatalf("re-encode decoded standard transaction: %v", err)
			}
			if !bytes.Equal(reencoded, txBytes) {
				t.Fatalf("standard re-encoding changed public transaction bytes: got=%X want=%X", reencoded, txBytes)
			}
		})
	}
}

func marshalPublicPulsarTx(t *testing.T, message protov2.Message, feePayer string) []byte {
	t.Helper()

	messageBytes, err := (protov2.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		t.Fatalf("marshal public Pulsar message: %v", err)
	}
	bodyBytes, err := (&txtypes.TxBody{Messages: []*codectypes.Any{{
		TypeUrl: "/" + string(message.ProtoReflect().Descriptor().FullName()),
		Value:   messageBytes,
	}}}).Marshal()
	if err != nil {
		t.Fatalf("marshal public transaction body: %v", err)
	}
	authInfoBytes, err := (&txtypes.AuthInfo{Fee: &txtypes.Fee{Payer: feePayer}}).Marshal()
	if err != nil {
		t.Fatalf("marshal public transaction auth info: %v", err)
	}
	txBytes, err := (&txtypes.TxRaw{BodyBytes: bodyBytes, AuthInfoBytes: authInfoBytes}).Marshal()
	if err != nil {
		t.Fatalf("marshal public TxRaw: %v", err)
	}
	return txBytes
}
