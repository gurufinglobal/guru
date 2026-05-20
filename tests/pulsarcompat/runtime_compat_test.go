package pulsarcompat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	legacyproto "github.com/golang/protobuf/proto"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ legacyproto.Message = (*oraclev1.QueryParamsResponse)(nil)

func TestLegacyProtoV1MarshalCompatibility(t *testing.T) {
	in := &oraclev1.QueryOracleValueResponse{
		Result: &oraclev1.OracleResult{
			Symbol:        "BTC/USD",
			Value:         "65000.1234",
			ResolveHeight: 10,
			ResolveTime:   20,
		},
	}

	bz, err := legacyproto.Marshal(in)
	if err != nil {
		t.Fatalf("legacy proto marshal failed: %v", err)
	}
	if len(bz) == 0 {
		t.Fatalf("legacy proto marshal returned empty bytes")
	}

	var out oraclev1.QueryOracleValueResponse
	if err := legacyproto.Unmarshal(bz, &out); err != nil {
		t.Fatalf("legacy proto unmarshal failed: %v", err)
	}
	if out.Result == nil || out.Result.Symbol != in.Result.Symbol || out.Result.Value != in.Result.Value {
		t.Fatalf("unexpected legacy proto round-trip result: got=%+v want=%+v", out.Result, in.Result)
	}
}

type mockGatewayQueryServer struct {
	oraclev1.UnimplementedQueryServer
}

func (mockGatewayQueryServer) Params(context.Context, *oraclev1.QueryParamsRequest) (*oraclev1.QueryParamsResponse, error) {
	return &oraclev1.QueryParamsResponse{Params: &oraclev1.Params{Enabled: true}}, nil
}

func (mockGatewayQueryServer) ActiveTasks(context.Context, *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
	return &oraclev1.QueryActiveTasksResponse{}, nil
}

func (mockGatewayQueryServer) OracleValue(_ context.Context, req *oraclev1.QueryOracleValueRequest) (*oraclev1.QueryOracleValueResponse, error) {
	return &oraclev1.QueryOracleValueResponse{
		Result: &oraclev1.OracleResult{
			Symbol:        req.Symbol,
			Value:         "42000.01",
			ResolveHeight: 11,
			ResolveTime:   22,
		},
	}, nil
}

func (mockGatewayQueryServer) OracleValues(context.Context, *oraclev1.QueryOracleValuesRequest) (*oraclev1.QueryOracleValuesResponse, error) {
	return &oraclev1.QueryOracleValuesResponse{}, nil
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /params status: got=%d", resp.StatusCode)
	}

	var paramsResp struct {
		Params struct {
			Enabled bool `json:"enabled"`
		} `json:"params"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&paramsResp); err != nil {
		t.Fatalf("decode /params response: %v", err)
	}
	if !paramsResp.Params.Enabled {
		t.Fatalf("unexpected /params response payload: %+v", paramsResp)
	}

	resp2, err := http.Get(srv.URL + "/guru/oracle/v1/oracle_values/BTC-USD")
	if err != nil {
		t.Fatalf("GET /oracle_values/{symbol} failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /oracle_values/{symbol} status: got=%d", resp2.StatusCode)
	}

	var valueResp struct {
		Result struct {
			Symbol string `json:"symbol"`
			Value  string `json:"value"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&valueResp); err != nil {
		t.Fatalf("decode /oracle_values/{symbol} response: %v", err)
	}
	if valueResp.Result.Symbol != "BTC-USD" || valueResp.Result.Value != "42000.01" {
		t.Fatalf("unexpected /oracle_values/{symbol} payload: %+v", valueResp)
	}
}

type mockMsgServer struct {
	oraclev1.UnimplementedMsgServer
}

func (mockMsgServer) UpdatePrices(_ context.Context, req *oraclev1.MsgUpdatePrices) (*oraclev1.MsgUpdatePricesResponse, error) {
	if req.Authority == "" {
		return nil, status.Error(codes.InvalidArgument, "authority is required")
	}
	if len(req.Rates) == 0 {
		return nil, status.Error(codes.InvalidArgument, "rates are required")
	}
	return &oraclev1.MsgUpdatePricesResponse{}, nil
}

func TestGRPCMsgClientServerWithPulsarMessages(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	oraclev1.RegisterMsgServer(s, mockMsgServer{})
	defer s.Stop()

	go func() {
		_ = s.Serve(lis)
	}()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	defer conn.Close()

	client := oraclev1.NewMsgClient(conn)
	_, err = client.UpdatePrices(context.Background(), &oraclev1.MsgUpdatePrices{
		Authority: "guru1authority",
		Rates: []*oraclev1.OracleResult{
			{
				Symbol:        "BTC/USD",
				Value:         "60000.0",
				ResolveHeight: 1,
				ResolveTime:   2,
			},
		},
	})
	if err != nil {
		t.Fatalf("grpc UpdatePrices failed: %v", err)
	}
}

func TestInterfaceRegistryCanUnpackPulsarMsgAndResponse(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	oracletypes.RegisterInterfaces(registry)

	msgIn := &oraclev1.MsgUpdatePrices{
		Authority: "guru1authority",
		Rates: []*oraclev1.OracleResult{
			{
				Symbol: "BTC/USD",
				Value:  "60000.0",
			},
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
	unpackedMsg, ok := msgOut.(*oraclev1.MsgUpdatePrices)
	if !ok {
		t.Fatalf("unexpected unpacked sdk.Msg type: %T", msgOut)
	}
	if unpackedMsg.Authority != msgIn.Authority {
		t.Fatalf("unexpected unpacked authority: got=%s want=%s", unpackedMsg.Authority, msgIn.Authority)
	}

	respAny, err := codectypes.NewAnyWithValue(&oraclev1.MsgUpdatePricesResponse{})
	if err != nil {
		t.Fatalf("pack tx.MsgResponse into Any failed: %v", err)
	}

	var respOut txtypes.MsgResponse
	if err := registry.UnpackAny(respAny, &respOut); err != nil {
		t.Fatalf("unpack tx.MsgResponse from Any failed: %v", err)
	}
	if _, ok := respOut.(*oraclev1.MsgUpdatePricesResponse); !ok {
		t.Fatalf("unexpected unpacked tx.MsgResponse type: %T", respOut)
	}
}
