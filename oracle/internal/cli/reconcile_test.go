package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc"
)

type reconcileQueryServer struct {
	oraclev1.UnimplementedQueryServer

	mu        sync.Mutex
	params    *oraclev1.Params
	paramsErr error
	active    func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error)
	calls     []string
}

func (s *reconcileQueryServer) Params(
	context.Context,
	*oraclev1.QueryParamsRequest,
) (*oraclev1.QueryParamsResponse, error) {
	s.recordCall("Params")
	if s.paramsErr != nil {
		return nil, s.paramsErr
	}
	params := s.params
	if params == nil {
		params = &oraclev1.Params{MinSources: 3}
	}
	return &oraclev1.QueryParamsResponse{Params: params}, nil
}

func (s *reconcileQueryServer) ActiveTasks(
	_ context.Context,
	request *oraclev1.QueryActiveTasksRequest,
) (*oraclev1.QueryActiveTasksResponse, error) {
	key := ""
	if request != nil && request.Pagination != nil {
		key = string(request.Pagination.Key)
	}
	s.recordCall("ActiveTasks:" + key)
	if s.active != nil {
		return s.active(request)
	}
	return &oraclev1.QueryActiveTasksResponse{Tasks: []*oraclev1.OracleTask{}}, nil
}

func (s *reconcileQueryServer) recordCall(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *reconcileQueryServer) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func startReconcileQueryServer(t *testing.T, queryServer *reconcileQueryServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	oraclev1.RegisterQueryServer(server, queryServer)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("test query server did not stop")
		}
	})
	return listener.Addr().String()
}

func TestReconcileClassifiesLiveContributionReadiness(t *testing.T) {
	const (
		revision = "published-revision"
		digest   = "published-digest"
	)
	tests := []struct {
		name       string
		minSources uint32
		tasks      []*oraclev1.OracleTask
		status     service.StatusData
		want       []expectedFinding
	}{
		{
			name:       "ready at chain source boundary",
			minSources: 4,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 4, 4, domain.CycleFull)),
		},
		{
			name:       "ready at fixed local source boundary",
			minSources: 1,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 3, 3, domain.CycleFull)),
		},
		{
			name:       "missing active and inactive local symbols",
			minSources: 3,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("ETH/USD", 3, 3, domain.CycleFull)),
			want: []expectedFinding{
				{code: "missing_symbol", blocking: true, symbol: "BTC/USD"},
				{code: "inactive_symbol", symbol: "ETH/USD"},
			},
		},
		{
			name:       "configured sources below fixed local minimum",
			minSources: 1,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 2, 2, domain.CycleFull)),
			want: []expectedFinding{
				{code: "configured_sources_below_minimum", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "configured sources below chain minimum",
			minSources: 4,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 3, 3, domain.CycleFull)),
			want: []expectedFinding{
				{code: "aggregate_sources_below_minimum", blocking: true, symbol: "BTC/USD"},
				{code: "configured_sources_below_minimum", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "latest aggregate below chain minimum",
			minSources: 4,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 4, 3, domain.CycleFull)),
			want: []expectedFinding{
				{code: "aggregate_sources_below_minimum", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "no current aggregate",
			minSources: 3,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, unavailableFeed("BTC/USD", domain.FreshnessNoValue)),
			want: []expectedFinding{
				{code: "no_value", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "stale aggregate",
			minSources: 3,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, unavailableFeed("BTC/USD", domain.FreshnessStale)),
			want: []expectedFinding{
				{code: "stale", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "clock anomaly",
			minSources: 3,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, unavailableFeed("BTC/USD", domain.FreshnessClockAnomaly)),
			want: []expectedFinding{
				{code: "clock_anomaly", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "older fresh aggregate after under quorum cycle",
			minSources: 3,
			tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
			status:     reconcileStatus(revision, digest, freshFeed("BTC/USD", 3, 3, domain.CycleUnderQuorum)),
			want: []expectedFinding{
				{code: "under_quorum", symbol: "BTC/USD"},
			},
		},
		{
			name:       "unsupported active task type",
			minSources: 3,
			tasks: []*oraclev1.OracleTask{{
				Symbol:    "BTC/USD",
				ValueType: oraclev1.ValueType_VALUE_TYPE_STRING,
				Enabled:   true,
			}},
			status: reconcileStatus(revision, digest, freshFeed("BTC/USD", 3, 3, domain.CycleFull)),
			want: []expectedFinding{
				{code: "unsupported_task_type", blocking: true, symbol: "BTC/USD"},
			},
		},
		{
			name:       "running configuration differs from published pair",
			minSources: 3,
			status:     reconcileStatus("running-revision", digest),
			want: []expectedFinding{
				{code: "runtime_config_mismatch", blocking: true},
			},
		},
		{
			name:       "running source digest differs from published pair",
			minSources: 3,
			status:     reconcileStatus(revision, "running-digest"),
			want: []expectedFinding{
				{code: "runtime_config_mismatch", blocking: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryServer := &reconcileQueryServer{
				params: &oraclev1.Params{MinSources: test.minSources},
				active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
					return &oraclev1.QueryActiveTasksResponse{Tasks: test.tasks}, nil
				},
			}
			data, err := reconcile(
				context.Background(),
				startReconcileQueryServer(t, queryServer),
				test.status,
				revision,
				digest,
			)
			if err != nil {
				t.Fatal(err)
			}
			assertReconcileFindings(t, data.Findings, test.want)
		})
	}
}

func TestReconcileQueriesEveryActiveTaskPage(t *testing.T) {
	queryServer := &reconcileQueryServer{
		params: &oraclev1.Params{MinSources: 3},
		active: func(request *oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
			switch string(request.GetPagination().GetKey()) {
			case "":
				return &oraclev1.QueryActiveTasksResponse{
					Tasks:      []*oraclev1.OracleTask{numericTask("BTC/USD")},
					Pagination: &sdkquery.PageResponse{NextKey: []byte("next")},
				}, nil
			case "next":
				return &oraclev1.QueryActiveTasksResponse{
					Tasks: []*oraclev1.OracleTask{numericTask("ETH/USD")},
				}, nil
			default:
				return nil, errors.New("unexpected page key")
			}
		},
	}
	data, err := reconcile(
		context.Background(),
		startReconcileQueryServer(t, queryServer),
		reconcileStatus(
			"revision",
			"digest",
			freshFeed("BTC/USD", 3, 3, domain.CycleFull),
			freshFeed("ETH/USD", 3, 3, domain.CycleFull),
		),
		"revision",
		"digest",
	)
	if err != nil {
		t.Fatal(err)
	}
	if data.ActiveTaskCount != 2 || len(data.Findings) != 0 {
		t.Fatalf("reconcile data = %#v", data)
	}
	wantCalls := []string{"Params", "ActiveTasks:", "ActiveTasks:next"}
	if calls := queryServer.Calls(); !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("query calls = %q, want %q", calls, wantCalls)
	}
}

func TestReconcileRejectsMalformedActiveTaskPages(t *testing.T) {
	tests := []struct {
		name   string
		active func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error)
	}{
		{
			name: "non canonical symbol",
			active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
				return &oraclev1.QueryActiveTasksResponse{Tasks: []*oraclev1.OracleTask{numericTask(" btc/usd ")}}, nil
			},
		},
		{
			name: "duplicate symbol",
			active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
				return &oraclev1.QueryActiveTasksResponse{Tasks: []*oraclev1.OracleTask{
					numericTask("BTC/USD"),
					numericTask("BTC/USD"),
				}}, nil
			},
		},
		{
			name: "repeated page key",
			active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
				return &oraclev1.QueryActiveTasksResponse{
					Tasks:      []*oraclev1.OracleTask{},
					Pagination: &sdkquery.PageResponse{NextKey: []byte("repeat")},
				}, nil
			},
		},
		{
			name: "active task count exceeds limit",
			active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
				tasks := make([]*oraclev1.OracleTask, 4097)
				for index := range tasks {
					tasks[index] = numericTask(fmt.Sprintf("SYMBOL-%04d", index))
				}
				return &oraclev1.QueryActiveTasksResponse{Tasks: tasks}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryServer := &reconcileQueryServer{active: test.active}
			_, err := reconcile(
				context.Background(),
				startReconcileQueryServer(t, queryServer),
				reconcileStatus("revision", "digest"),
				"revision",
				"digest",
			)
			if err == nil || !isReconcileProtocolError(err) {
				t.Fatalf("reconcile error = %v, want protocol error", err)
			}
		})
	}
}

type expectedFinding struct {
	code     string
	blocking bool
	symbol   string
}

func assertReconcileFindings(t *testing.T, got []Finding, want []expectedFinding) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("findings = %#v, want %#v", got, want)
	}
	for index := range want {
		symbol := ""
		if got[index].Symbol != nil {
			symbol = *got[index].Symbol
		}
		if got[index].Code != want[index].code ||
			got[index].Blocking != want[index].blocking ||
			symbol != want[index].symbol {
			t.Fatalf("finding[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func numericTask(symbol string) *oraclev1.OracleTask {
	return &oraclev1.OracleTask{
		Symbol:    symbol,
		ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:   true,
	}
}

func reconcileStatus(revision, digest string, feeds ...service.FeedStatus) service.StatusData {
	return service.StatusData{
		PublicationRevision: revision,
		SourcesSHA256:       digest,
		Feeds:               feeds,
	}
}

func freshFeed(
	symbol string,
	configuredSources uint32,
	successfulSources uint32,
	lastOutcome domain.CycleOutcome,
) service.FeedStatus {
	return service.FeedStatus{
		Symbol:                symbol,
		ConfiguredSourceCount: configuredSources,
		Freshness:             string(domain.FreshnessFresh),
		Latest: &service.LatestStatus{
			SuccessfulSourceCount: successfulSources,
		},
		Cycle: service.CycleStatus{LastOutcome: string(lastOutcome)},
	}
}

func unavailableFeed(symbol string, freshness domain.Freshness) service.FeedStatus {
	return service.FeedStatus{
		Symbol:                symbol,
		ConfiguredSourceCount: 3,
		Freshness:             string(freshness),
		Cycle:                 service.CycleStatus{LastOutcome: string(domain.CycleUnderQuorum)},
	}
}
