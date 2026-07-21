package oracle

import (
	"context"
	"net"
	"strconv"
	"testing"

	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestEnsureNodeTasksConfiguredRequiresMatchingSource(t *testing.T) {
	address, stop := startOracleQueryServer(t, testOracleParams(), []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
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
	address, stop := startOracleQueryServer(t, testOracleParams(), []*oraclev1.OracleTask{{
		Symbol:             "btc/usd",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 2,
	}})
	defer stop()

	tasks, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources:          btcSources(),
	})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "BTC/USD", tasks[0].GetSymbol())
	require.Equal(t, uint32(2), tasks[0].GetSubmissionInterval())
}

func TestEnsureNodeTasksConfiguredRequiresMinSources(t *testing.T) {
	address, stop := startOracleQueryServer(t, testOracleParams(), []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
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
	require.ErrorContains(t, err, "min_sources=3")
}

func TestEnsureNodeTasksConfiguredRejectsNonNumericNodeTasks(t *testing.T) {
	address, stop := startOracleQueryServer(t, testOracleParams(), []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_STRING,
		Enabled:            true,
		SubmissionInterval: 1,
	}})
	defer stop()

	_, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources:          btcSources(),
	})
	require.ErrorContains(t, err, "no active numeric tasks")
}

func TestEnsureNodeTasksConfiguredRejectsMissingParams(t *testing.T) {
	address, stop := startOracleQueryServer(t, nil, []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
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
	require.ErrorContains(t, err, "oracle params are not initialized")
}

func TestEnsureNodeTasksConfiguredFollowsNodeTaskPagination(t *testing.T) {
	nodeTasks := make([]*oraclev1.OracleTask, 0, 125)
	for i := 0; i < 124; i++ {
		nodeTasks = append(nodeTasks, &oraclev1.OracleTask{
			Symbol:             "IGNORED" + strconv.Itoa(i) + "/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 1,
		})
	}
	nodeTasks = append(nodeTasks, &oraclev1.OracleTask{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	})

	address, stop := startOracleQueryServer(t, testOracleParams(), nodeTasks)
	defer stop()

	tasks, err := EnsureNodeTasksConfigured(context.Background(), Config{
		Socket:           "/tmp/oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         address,
		NodeQueryTimeout: "1s",
		Sources:          btcSources(),
	})
	require.NoError(t, err)
	require.Len(t, tasks, 125)
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
			{Symbol: "btc/usd", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1},
			{Symbol: "BTC/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1},
		},
	)

	require.Len(t, matches, 2)
	require.Equal(t, "a", matches[0].Name)
	require.Equal(t, "b", matches[1].Name)
}

func testOracleParams() *oraclev1.Params {
	return &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100}
}

func btcSources() []SourceConfig {
	return []SourceConfig{
		{Name: "btc-a", Symbol: "BTC/USD", URL: "http://example.invalid/btc-a", ResponsePath: "price"},
		{Name: "btc-b", Symbol: "BTC/USD", URL: "http://example.invalid/btc-b", ResponsePath: "price"},
		{Name: "btc-c", Symbol: "BTC/USD", URL: "http://example.invalid/btc-c", ResponsePath: "price"},
	}
}

func startOracleQueryServer(t *testing.T, params *oraclev1.Params, tasks []*oraclev1.OracleTask) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	oraclev1.RegisterQueryServer(server, &fakeOracleQueryServer{
		params: params,
		tasks:  tasks,
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

	params *oraclev1.Params
	tasks  []*oraclev1.OracleTask
}

func (f fakeOracleQueryServer) Params(context.Context, *oraclev1.QueryParamsRequest) (*oraclev1.QueryParamsResponse, error) {
	return &oraclev1.QueryParamsResponse{
		Params: f.params,
	}, nil
}

func (f fakeOracleQueryServer) ActiveTasks(_ context.Context, req *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
	offset := uint64(0)
	limit := uint64(30)
	if pagination := req.GetPagination(); pagination != nil {
		if key := pagination.GetKey(); len(key) > 0 {
			parsed, err := strconv.ParseUint(string(key), 10, 64)
			if err != nil {
				return nil, err
			}
			offset = parsed
		} else {
			offset = pagination.GetOffset()
		}
		if pagination.GetLimit() != 0 {
			limit = pagination.GetLimit()
		}
	}

	total := uint64(len(f.tasks))
	if offset >= total {
		return &oraclev1.QueryActiveTasksResponse{
			Pagination: &querytypes.PageResponse{Total: total},
		}, nil
	}

	end := offset + limit
	if end < offset || end > total {
		end = total
	}

	var nextKey []byte
	if end < total {
		nextKey = []byte(strconv.FormatUint(end, 10))
	}

	return &oraclev1.QueryActiveTasksResponse{
		Tasks: f.tasks[offset:end],
		Pagination: &querytypes.PageResponse{
			NextKey: nextKey,
			Total:   total,
		},
	}, nil
}
