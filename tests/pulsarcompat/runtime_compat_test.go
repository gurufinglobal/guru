package pulsarcompat

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	pulsarconstitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	pulsaroraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
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

func (mockGatewayQueryServer) History(_ context.Context, req *oracletypes.QueryHistoryRequest) (*oracletypes.QueryHistoryResponse, error) {
	if req.GetSymbol() == "PAGINATION/TEST" && (req.GetPagination() == nil || req.GetPagination().GetOffset() != 3 || req.GetPagination().GetLimit() != 4) {
		return nil, status.Error(codes.InvalidArgument, "unexpected pagination request")
	}
	return &oracletypes.QueryHistoryResponse{
		History: &oracletypes.OracleHistory{Symbol: req.GetSymbol()},
	}, nil
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

	resp2, err := http.Get(srv.URL + "/guru/oracle/v1/value?symbol=BTC-USD")
	if err != nil {
		t.Fatalf("GET /value?symbol= failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /value?symbol= status: got=%d", resp2.StatusCode)
	}

	valueJSON, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read /value?symbol= response: %v", err)
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

func TestOracleGatewaySymbolQueryParameter(t *testing.T) {
	marshaler := &gogogateway.JSONPb{EmitDefaults: true, OrigName: true}
	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler))
	if err := oracletypes.RegisterQueryHandlerServer(context.Background(), mux, mockGatewayQueryServer{}); err != nil {
		t.Fatalf("register query gateway handler: %v", err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	routes := []struct {
		name          string
		path          string
		responseField string
	}{
		{name: "task", path: "/guru/oracle/v1/task", responseField: "task"},
		{name: "latest value", path: "/guru/oracle/v1/value", responseField: "value"},
		{name: "history", path: "/guru/oracle/v1/history", responseField: "history"},
	}

	// The mock echoes symbols so this test isolates HTTP query decoding from keeper lookup semantics.
	for _, route := range routes {
		for _, symbolCase := range []struct {
			name   string
			symbol string
		}{
			{name: "slash", symbol: "BTC/USD"},
			{name: "percent", symbol: "PCT%USD"},
			{name: "unicode", symbol: "한글/원"},
			{name: "empty", symbol: ""},
			{name: "unknown", symbol: "UNKNOWN/SYMBOL"},
		} {
			t.Run(route.name+"/"+symbolCase.name, func(t *testing.T) {
				query := url.Values{}
				query.Set("symbol", symbolCase.symbol)
				resp, err := http.Get(srv.URL + route.path + "?" + query.Encode())
				if err != nil {
					t.Fatalf("GET %s failed: %v", route.path, err)
				}
				body, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if readErr != nil {
					t.Fatalf("read %s response: %v", route.path, readErr)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected %s status: got=%d body=%s", route.path, resp.StatusCode, body)
				}

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("decode %s response %q: %v", route.path, body, err)
				}
				message, ok := payload[route.responseField].(map[string]any)
				if !ok || message["symbol"] != symbolCase.symbol {
					t.Fatalf("unexpected %s symbol: got=%v want=%q", route.path, message["symbol"], symbolCase.symbol)
				}
			})
		}

		t.Run(route.name+"/missing", func(t *testing.T) {
			resp, err := http.Get(srv.URL + route.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", route.path, err)
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatalf("read %s response: %v", route.path, readErr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("unexpected %s status: got=%d body=%s", route.path, resp.StatusCode, body)
			}

			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode %s response %q: %v", route.path, body, err)
			}
			message, ok := payload[route.responseField].(map[string]any)
			if !ok || message["symbol"] != "" {
				t.Fatalf("unexpected missing %s symbol: got=%v", route.path, message["symbol"])
			}
		})
	}

	paginationQuery := url.Values{}
	paginationQuery.Set("symbol", "PAGINATION/TEST")
	paginationQuery.Set("pagination.offset", "3")
	paginationQuery.Set("pagination.limit", "4")
	resp, err := http.Get(srv.URL + "/guru/oracle/v1/history?" + paginationQuery.Encode())
	if err != nil {
		t.Fatalf("GET history with pagination failed: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read history pagination response: %v", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected history pagination status: got=%d body=%s", resp.StatusCode, body)
	}
	var paginationPayload map[string]any
	if err := json.Unmarshal(body, &paginationPayload); err != nil {
		t.Fatalf("decode history pagination response %q: %v", body, err)
	}
	history, ok := paginationPayload["history"].(map[string]any)
	if !ok || history["symbol"] != "PAGINATION/TEST" {
		t.Fatalf("unexpected history pagination symbol: got=%v", history["symbol"])
	}

	for _, oldPath := range []string{
		"/guru/oracle/v1/tasks/BTC-USD",
		"/guru/oracle/v1/values/BTC-USD",
		"/guru/oracle/v1/history/BTC-USD",
	} {
		resp, err := http.Get(srv.URL + oldPath)
		if err != nil {
			t.Fatalf("GET legacy path %s failed: %v", oldPath, err)
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read legacy path %s response: %v", oldPath, readErr)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unexpected legacy path %s status: got=%d want=%d", oldPath, resp.StatusCode, http.StatusNotFound)
		}
	}
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
	for _, symbol := range []string{"BTC/USD", "PCT%USD", "한글/원"} {
		task, err := queryClient.Task(context.Background(), &pulsaroraclev1.QueryTaskRequest{Symbol: symbol})
		if err != nil {
			t.Fatalf("grpc Task failed for %q: %v", symbol, err)
		}
		if task.GetTask().GetSymbol() != symbol {
			t.Fatalf("unexpected grpc Task symbol: got=%q want=%q", task.GetTask().GetSymbol(), symbol)
		}

		latest, err := queryClient.LatestValue(context.Background(), &pulsaroraclev1.QueryLatestValueRequest{Symbol: symbol})
		if err != nil {
			t.Fatalf("grpc LatestValue failed for %q: %v", symbol, err)
		}
		if latest.GetValue().GetSymbol() != symbol {
			t.Fatalf("unexpected grpc LatestValue symbol: got=%q want=%q", latest.GetValue().GetSymbol(), symbol)
		}

		history, err := queryClient.History(context.Background(), &pulsaroraclev1.QueryHistoryRequest{Symbol: symbol})
		if err != nil {
			t.Fatalf("grpc History failed for %q: %v", symbol, err)
		}
		if history.GetHistory().GetSymbol() != symbol {
			t.Fatalf("unexpected grpc History symbol: got=%q want=%q", history.GetHistory().GetSymbol(), symbol)
		}
	}

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
