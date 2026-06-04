package oracle

import (
	"context"
	"net"
	"testing"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestEnsureNodeTasksConfiguredRequiresMatchingSource(t *testing.T) {
	address, stop := startOracleQueryServer(t, true, []*oraclev1.OracleTask{{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}})
	defer stop()

	_, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources: []SourceConfig{{
			Name:         "eth",
			Symbol:       "ETH/USD",
			URL:          "http://example.invalid/eth",
			ResponsePath: "price",
		}},
	})
	require.ErrorContains(t, err, "no configured oracle sources match")
}

func TestEnsureNodeTasksConfiguredReturnsNodeTasks(t *testing.T) {
	address, stop := startOracleQueryServer(t, true, []*oraclev1.OracleTask{{
		Symbol:    "btc/usd",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}})
	defer stop()

	tasks, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources: []SourceConfig{{
			Name:         "btc",
			Symbol:       "BTC/USD",
			URL:          "http://example.invalid/btc",
			ResponsePath: "price",
		}},
	})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "BTC/USD", tasks[0].GetSymbol())
}

func TestEnsureNodeTasksConfiguredRejectsDisabledModule(t *testing.T) {
	address, stop := startOracleQueryServer(t, false, []*oraclev1.OracleTask{{
		Symbol:    "BTC/USD",
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}})
	defer stop()

	_, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources: []SourceConfig{{
			Name:         "btc",
			Symbol:       "BTC/USD",
			URL:          "http://example.invalid/btc",
			ResponsePath: "price",
		}},
	})
	require.ErrorContains(t, err, "oracle module is disabled")
}

func TestMatchingSourcesForTasksDeduplicatesAndSorts(t *testing.T) {
	matches := MatchingSourcesForTasks(
		[]SourceConfig{
			{Name: "b", Symbol: "BTC/USD", URL: "http://example.invalid/b", ResponsePath: "price"},
			{Name: "a", Symbol: "btc/usd", URL: "http://example.invalid/a", ResponsePath: "price"},
			{Name: "a", Symbol: "BTC/USD", URL: "http://example.invalid/a2", ResponsePath: "price"},
			{Name: "eth", Symbol: "ETH/USD", URL: "http://example.invalid/eth", ResponsePath: "price"},
		},
		[]*oraclev1.OracleTask{
			{Symbol: "btc/usd", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true},
			{Symbol: "BTC/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true},
		},
	)

	require.Len(t, matches, 2)
	require.Equal(t, "a", matches[0].Name)
	require.Equal(t, "b", matches[1].Name)
}

func startOracleQueryServer(t *testing.T, enabled bool, tasks []*oraclev1.OracleTask) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	oraclev1.RegisterQueryServer(server, fakeOracleQueryServer{
		enabled: enabled,
		tasks:   tasks,
	})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.GracefulStop()
		require.NoError(t, <-done)
	}
}

type fakeOracleQueryServer struct {
	oraclev1.UnimplementedQueryServer

	enabled bool
	tasks   []*oraclev1.OracleTask
}

func (f fakeOracleQueryServer) Params(context.Context, *oraclev1.QueryParamsRequest) (*oraclev1.QueryParamsResponse, error) {
	return &oraclev1.QueryParamsResponse{
		Params: &oraclev1.Params{Enabled: f.enabled},
	}, nil
}

func (f fakeOracleQueryServer) ActiveTasks(context.Context, *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
	return &oraclev1.QueryActiveTasksResponse{Tasks: f.tasks}, nil
}
