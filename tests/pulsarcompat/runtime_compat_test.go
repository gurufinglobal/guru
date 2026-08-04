package pulsarcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
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
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
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

func (mockSidecarServer) GetAggregates(_ context.Context, req *oracletypes.GetAggregatesRequest) (*oracletypes.GetAggregatesResponse, error) {
	if len(req.GetSymbols()) != 1 || req.GetSymbols()[0] != "ATOM/USD" {
		return nil, status.Error(codes.InvalidArgument, "unexpected aggregate request")
	}
	return &oracletypes.GetAggregatesResponse{Results: []*oracletypes.AggregatedResult{{
		Symbol:      "ATOM/USD",
		Value:       "12.34",
		SourceCount: 3,
	}}}, nil
}

type mockPublicSidecarServer struct {
	pulsaroraclev1.UnimplementedOracleSidecarServer
}

func (mockPublicSidecarServer) GetAggregates(_ context.Context, req *pulsaroraclev1.GetAggregatesRequest) (*pulsaroraclev1.GetAggregatesResponse, error) {
	if len(req.GetSymbols()) != 2 || req.GetSymbols()[0] != "BTC/USD" || req.GetSymbols()[1] != "ETH/USD" {
		return nil, status.Error(codes.InvalidArgument, "unexpected aggregate request")
	}
	return &pulsaroraclev1.GetAggregatesResponse{Results: []*pulsaroraclev1.AggregatedResult{
		{Symbol: "BTC/USD", Value: "65000.25", SourceCount: 3},
		{Symbol: "ETH/USD", Value: "3500.5", SourceCount: 5},
	}}, nil
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
	aggregates, err := sidecarClient.GetAggregates(context.Background(), &pulsaroraclev1.GetAggregatesRequest{
		Symbols: []string{"ATOM/USD"},
	})
	if err != nil {
		t.Fatalf("grpc GetAggregates failed: %v", err)
	}
	if len(aggregates.GetResults()) != 1 || aggregates.GetResults()[0].GetValue() != "12.34" || aggregates.GetResults()[0].GetSourceCount() != 3 {
		t.Fatalf("unexpected GetAggregates payload: %+v", aggregates)
	}
}

func TestInternalGogoClientCallsPublicPulsarSidecarServer(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	pulsaroraclev1.RegisterOracleSidecarServer(s, mockPublicSidecarServer{})
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

	client := oracletypes.NewOracleSidecarClient(conn)
	response, err := client.GetAggregates(context.Background(), &oracletypes.GetAggregatesRequest{
		Symbols: []string{"BTC/USD", "ETH/USD"},
	})
	if err != nil {
		t.Fatalf("internal gogo GetAggregates client failed against public Pulsar server: %v", err)
	}
	if len(response.GetResults()) != 2 {
		t.Fatalf("unexpected result count: got=%d want=2", len(response.GetResults()))
	}
	if got := response.GetResults()[0]; got.GetSymbol() != "BTC/USD" || got.GetValue() != "65000.25" || got.GetSourceCount() != 3 {
		t.Fatalf("unexpected first aggregate: %+v", got)
	}
	if got := response.GetResults()[1]; got.GetSymbol() != "ETH/USD" || got.GetValue() != "3500.5" || got.GetSourceCount() != 5 {
		t.Fatalf("unexpected second aggregate: %+v", got)
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

const (
	feePolicyCanonicalAmount = "25.125000000000000000"
	feePolicyMsgType         = "/cosmos.bank.v1beta1.MsgSend"
)

func TestFeePolicyInternalPBInterfaceRegistryAndTxConfigCompatibility(t *testing.T) {
	encoding := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	feepolicytypes.RegisterInterfaces(encoding.InterfaceRegistry)

	moderatorBytes := bytes.Repeat([]byte{0x41}, 20)
	moderator, err := encoding.InterfaceRegistry.SigningContext().AddressCodec().BytesToString(moderatorBytes)
	require.NoError(t, err)
	newModerator, err := encoding.InterfaceRegistry.SigningContext().AddressCodec().BytesToString(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)

	messages := []sdk.Msg{
		&feepolicytypes.MsgRegisterDiscounts{
			ModeratorAddress: moderator,
			Discounts:        []feepolicytypes.AccountDiscount{feePolicyInternalAccountDiscount()},
		},
		&feepolicytypes.MsgRemoveDiscounts{
			ModeratorAddress: moderator,
			Module:           "bank",
			MsgType:          feePolicyMsgType,
		},
		&feepolicytypes.MsgChangeModerator{
			ModeratorAddress:    moderator,
			NewModeratorAddress: newModerator,
		},
	}

	for _, msg := range messages {
		t.Run(sdk.MsgTypeURL(msg), func(t *testing.T) {
			typeURL := sdk.MsgTypeURL(msg)
			packed, err := codectypes.NewAnyWithValue(msg)
			require.NoError(t, err)
			require.Equal(t, typeURL, packed.TypeUrl)

			var unpacked sdk.Msg
			require.NoError(t, encoding.InterfaceRegistry.UnpackAny(packed, &unpacked))
			require.IsType(t, msg, unpacked)

			builder := encoding.TxConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(msg))
			encoded, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
			require.NoError(t, err)

			decoded, err := encoding.TxConfig.TxDecoder()(encoded)
			require.NoError(t, err)
			require.Len(t, decoded.GetMsgs(), 1)
			require.IsType(t, msg, decoded.GetMsgs()[0])

			msgsV2, err := decoded.GetMsgsV2()
			require.NoError(t, err)
			require.Len(t, msgsV2, 1)
			require.Equal(t, strings.TrimPrefix(typeURL, "/"), string(msgsV2[0].ProtoReflect().Descriptor().FullName()))

			sigTx, ok := decoded.(authsigning.SigVerifiableTx)
			require.True(t, ok)
			signers, err := sigTx.GetSigners()
			require.NoError(t, err)
			require.Equal(t, [][]byte{moderatorBytes}, signers)

			wrapped, err := encoding.TxConfig.WrapTxBuilder(decoded)
			require.NoError(t, err)
			reencoded, err := encoding.TxConfig.TxEncoder()(wrapped.GetTx())
			require.NoError(t, err)
			require.Equal(t, encoded, reencoded)
		})
	}

	responseAny, err := codectypes.NewAnyWithValue(&feepolicytypes.MsgRegisterDiscountsResponse{})
	require.NoError(t, err)
	var response txtypes.MsgResponse
	require.NoError(t, encoding.InterfaceRegistry.UnpackAny(responseAny, &response))
	require.IsType(t, &feepolicytypes.MsgRegisterDiscountsResponse{}, response)
}

func TestFeePolicyInternalPBRESTGatewayCompatibility(t *testing.T) {
	marshaler := &gogogateway.JSONPb{EmitDefaults: true, OrigName: true}
	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler))
	queryServer := feePolicyGatewayQueryServer{}
	msgServer := &feePolicyGatewayMsgServer{}
	require.NoError(t, feepolicytypes.RegisterQueryHandlerServer(context.Background(), mux, queryServer))
	require.NoError(t, feepolicytypes.RegisterMsgHandlerServer(context.Background(), mux, msgServer))

	moderatorResponse := serveFeePolicyGateway(t, mux, http.MethodGet, "/guru/feepolicy/v1/moderator_address", "")
	require.Equal(t, http.StatusOK, moderatorResponse.Code, moderatorResponse.Body.String())
	require.JSONEq(t, `{"moderator_address":"guru1moderator"}`, moderatorResponse.Body.String())

	discountsResponse := serveFeePolicyGateway(t, mux, http.MethodGet, "/guru/feepolicy/v1/discounts", "")
	require.Equal(t, http.StatusOK, discountsResponse.Code, discountsResponse.Body.String())

	discountResponse := serveFeePolicyGateway(t, mux, http.MethodGet, "/guru/feepolicy/v1/discounts/guru1account", "")
	require.Equal(t, http.StatusOK, discountResponse.Code, discountResponse.Body.String())
	require.JSONEq(t, `{
		"discount": {
			"address": "guru1account",
			"modules": [{
				"module": "bank",
				"discounts": [{
					"discount_type": "percent",
					"msg_type": "/cosmos.bank.v1beta1.MsgSend",
					"amount": "25.125000000000000000"
				}]
			}]
		}
	}`, discountResponse.Body.String())

	registerResponse := serveFeePolicyGateway(t, mux, http.MethodPost, "/guru/feepolicy/v1/register_discounts", `{
		"moderator_address":"guru1moderator",
		"discounts":[{
			"address":"guru1account",
			"modules":[{
				"module":"bank",
				"discounts":[{
					"discount_type":"percent",
					"msg_type":"/cosmos.bank.v1beta1.MsgSend",
					"amount":"25.125000000000000000"
				}]
			}]
		}]
	}`)
	require.Equal(t, http.StatusOK, registerResponse.Code, registerResponse.Body.String())

	removeResponse := serveFeePolicyGateway(t, mux, http.MethodPost, "/guru/feepolicy/v1/remove_discounts", `{
		"moderator_address":"guru1moderator",
		"address":"guru1account",
		"module":"bank",
		"msg_type":"/cosmos.bank.v1beta1.MsgSend"
	}`)
	require.Equal(t, http.StatusOK, removeResponse.Code, removeResponse.Body.String())

	changeResponse := serveFeePolicyGateway(t, mux, http.MethodPost, "/guru/feepolicy/v1/change_moderator", `{
		"moderator_address":"guru1moderator",
		"new_moderator_address":"guru1newmoderator"
	}`)
	require.Equal(t, http.StatusOK, changeResponse.Code, changeResponse.Body.String())
	require.Equal(t, 1, msgServer.registerCalls)
	require.Equal(t, 1, msgServer.removeCalls)
	require.Equal(t, 1, msgServer.changeCalls)
}

func feePolicyInternalAccountDiscount() feepolicytypes.AccountDiscount {
	return feepolicytypes.AccountDiscount{
		Address: "guru1account",
		Modules: []feepolicytypes.ModuleDiscount{{
			Module: "bank",
			Discounts: []feepolicytypes.Discount{{
				DiscountType: "percent",
				MsgType:      feePolicyMsgType,
				Amount:       sdkmath.LegacyMustNewDecFromStr(feePolicyCanonicalAmount),
			}},
		}},
	}
}

type feePolicyGatewayQueryServer struct {
	feepolicytypes.UnimplementedQueryServer
}

func (feePolicyGatewayQueryServer) ModeratorAddress(context.Context, *feepolicytypes.QueryModeratorAddressRequest) (*feepolicytypes.QueryModeratorAddressResponse, error) {
	return &feepolicytypes.QueryModeratorAddressResponse{ModeratorAddress: "guru1moderator"}, nil
}

func (feePolicyGatewayQueryServer) Discounts(context.Context, *feepolicytypes.QueryDiscountsRequest) (*feepolicytypes.QueryDiscountsResponse, error) {
	return &feepolicytypes.QueryDiscountsResponse{
		Discounts: []feepolicytypes.AccountDiscount{feePolicyInternalAccountDiscount()},
	}, nil
}

func (feePolicyGatewayQueryServer) Discount(_ context.Context, request *feepolicytypes.QueryDiscountRequest) (*feepolicytypes.QueryDiscountResponse, error) {
	if request.GetAddress() != "guru1account" {
		return nil, fmt.Errorf("unexpected discount address %q", request.GetAddress())
	}
	return &feepolicytypes.QueryDiscountResponse{Discount: feePolicyInternalAccountDiscount()}, nil
}

type feePolicyGatewayMsgServer struct {
	feepolicytypes.UnimplementedMsgServer
	registerCalls int
	removeCalls   int
	changeCalls   int
}

func (server *feePolicyGatewayMsgServer) RegisterDiscounts(_ context.Context, request *feepolicytypes.MsgRegisterDiscounts) (*feepolicytypes.MsgRegisterDiscountsResponse, error) {
	if request.ModeratorAddress != "guru1moderator" || len(request.Discounts) != 1 || len(request.Discounts[0].Modules) != 1 || len(request.Discounts[0].Modules[0].Discounts) != 1 {
		return nil, fmt.Errorf("unexpected register request: %+v", request)
	}
	if !request.Discounts[0].Modules[0].Discounts[0].Amount.Equal(sdkmath.LegacyMustNewDecFromStr(feePolicyCanonicalAmount)) {
		return nil, fmt.Errorf("unexpected LegacyDec amount: %s", request.Discounts[0].Modules[0].Discounts[0].Amount)
	}
	server.registerCalls++
	return &feepolicytypes.MsgRegisterDiscountsResponse{}, nil
}

func (server *feePolicyGatewayMsgServer) RemoveDiscounts(_ context.Context, request *feepolicytypes.MsgRemoveDiscounts) (*feepolicytypes.MsgRemoveDiscountsResponse, error) {
	if request.ModeratorAddress != "guru1moderator" || request.Address != "guru1account" || request.Module != "bank" || request.MsgType != feePolicyMsgType {
		return nil, fmt.Errorf("unexpected remove request: %+v", request)
	}
	server.removeCalls++
	return &feepolicytypes.MsgRemoveDiscountsResponse{}, nil
}

func (server *feePolicyGatewayMsgServer) ChangeModerator(_ context.Context, request *feepolicytypes.MsgChangeModerator) (*feepolicytypes.MsgChangeModeratorResponse, error) {
	if request.ModeratorAddress != "guru1moderator" || request.NewModeratorAddress != "guru1newmoderator" {
		return nil, fmt.Errorf("unexpected change-moderator request: %+v", request)
	}
	server.changeCalls++
	return &feepolicytypes.MsgChangeModeratorResponse{}, nil
}

func serveFeePolicyGateway(t *testing.T, mux *runtime.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}
