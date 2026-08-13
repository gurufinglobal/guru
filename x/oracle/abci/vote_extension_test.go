package abci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAggregateRequestFromTasksFiltersNumericSortsAndDeduplicates(t *testing.T) {
	firstBTC := numericTask(" btc/usd ")
	tasks := []*oraclev1.OracleTask{
		numericTask("ZRX/USD"),
		nil,
		{Symbol: "STRING/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_STRING},
		firstBTC,
		numericTask("BTC/USD"),
		numericTask(strings.Repeat("X", maxSidecarSymbolBytes+1)),
		numericTask("ETH/USD"),
	}

	symbols, tasksBySymbol, ok := aggregateRequestFromTasks(tasks)

	require.True(t, ok)
	require.Equal(t, []string{"BTC/USD", "ETH/USD", "ZRX/USD"}, symbols)
	require.Len(t, tasksBySymbol, 3)
	require.Same(t, firstBTC, tasksBySymbol["BTC/USD"])
}

func TestAggregateRequestFromTasksRejectsMoreThanMaximumSymbols(t *testing.T) {
	t.Run("maximum accepted", func(t *testing.T) {
		tasks := make([]*oraclev1.OracleTask, 0, maxSidecarSymbols)
		for i := range maxSidecarSymbols {
			tasks = append(tasks, numericTask(fmt.Sprintf("ASSET-%03d/USD", i)))
		}

		symbols, tasksBySymbol, ok := aggregateRequestFromTasks(tasks)

		require.True(t, ok)
		require.Len(t, symbols, maxSidecarSymbols)
		require.Len(t, tasksBySymbol, maxSidecarSymbols)
		require.True(t, sort.StringsAreSorted(symbols))
	})

	t.Run("first excess distinct symbol rejected", func(t *testing.T) {
		tasks := make([]*oraclev1.OracleTask, 0, maxSidecarSymbols+1)
		for i := range maxSidecarSymbols + 1 {
			tasks = append(tasks, numericTask(fmt.Sprintf("ASSET-%03d/USD", i)))
		}

		symbols, tasksBySymbol, ok := aggregateRequestFromTasks(tasks)

		require.False(t, ok)
		require.Nil(t, symbols)
		require.Nil(t, tasksBySymbol)
	})
}

func TestNewVoteExtensionHandlerValidatesEnabledTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		socket  string
	}{
		{name: "disabled malformed target", enabled: false, socket: "%zz"},
		{name: "enabled empty target", enabled: true, socket: "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := NewVoteExtensionHandler(nil, tc.enabled, tc.socket, time.Second)

			require.NoError(t, err)
			require.NotNil(t, handler)
			require.NoError(t, handler.Close())
		})
	}

	handler, err := NewVoteExtensionHandler(nil, true, "%zz", time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create oracle sidecar client")
	require.Nil(t, handler)
}

func TestExtendVoteRequestsOnlySortedSymbolsAndReturnsCanonicalAggregates(t *testing.T) {
	sidecar := &testSidecar{results: []*oraclev1.AggregatedResult{
		{Symbol: "ETH/USD", Value: "2.000000000000000000", SourceCount: 4},
		{Symbol: "BTC/USD", Value: "1.000000000000000000", SourceCount: 3},
	}}
	socket, _ := startTestSidecar(t, sidecar)
	handler := mustNewVoteExtensionHandler(t, fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
		tasks: []*oraclev1.OracleTask{
			numericTask("ETH/USD"),
			{Symbol: "IGNORED", ValueType: oraclev1.ValueType_VALUE_TYPE_STRING},
			numericTask("BTC/USD"),
			numericTask(" btc/usd "),
		},
	}, true, socket, time.Second)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})

	resp, err := handler.ExtendVote(
		sdk.Context{}.WithContext(context.Background()),
		&abcitypes.RequestExtendVote{Height: 12},
	)
	require.NoError(t, err)

	extension := mustDecodeVoteExtension(t, resp.GetVoteExtension())
	require.Equal(t, []*oraclev1.OracleValidatorResult{
		{Symbol: "BTC/USD", Value: "1.000000000000000000", SourceCount: 3},
		{Symbol: "ETH/USD", Value: "2.000000000000000000", SourceCount: 4},
	}, extension.GetResults())
	require.Equal(t, [][]string{{"BTC/USD", "ETH/USD"}}, sidecar.Requests())
}

func TestExtendVoteRejectsOversizedTaskSetBeforeSidecarCall(t *testing.T) {
	sidecar := &testSidecar{}
	socket, _ := startTestSidecar(t, sidecar)
	tasks := make([]*oraclev1.OracleTask, 0, maxSidecarSymbols+1)
	for i := range maxSidecarSymbols + 1 {
		tasks = append(tasks, numericTask(fmt.Sprintf("ASSET-%03d/USD", i)))
	}
	handler := mustNewVoteExtensionHandler(t, fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 1, HistoryLimit: 100},
		tasks:  tasks,
	}, true, socket, time.Second)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})

	resp, err := handler.ExtendVote(
		sdk.Context{}.WithContext(context.Background()),
		&abcitypes.RequestExtendVote{Height: 12},
	)
	require.NoError(t, err)
	require.Empty(t, resp.GetVoteExtension())
	require.Zero(t, sidecar.CallCount())

	// The independent serialized-size guard is defense in depth for future
	// schema changes. With today's 256-symbol and 128-byte-per-symbol limits,
	// the largest admissible request cannot reach the 64 KiB message cap.
	maxSymbols := make([]string, 0, maxSidecarSymbols)
	for i := range maxSidecarSymbols {
		prefix := fmt.Sprintf("%03d-", i)
		maxSymbols = append(maxSymbols, prefix+strings.Repeat("X", maxSidecarSymbolBytes-len(prefix)))
	}
	maxRequest := &oraclev1.GetAggregatesRequest{Symbols: maxSymbols}
	require.Less(t, maxRequest.Size(), maxSidecarRequestBytes)
}

func TestExtendVoteRejectsOversizedResponseAsAWhole(t *testing.T) {
	results := make([]*oraclev1.AggregatedResult, 0, maxSidecarSymbols+1)
	for i := range maxSidecarSymbols + 1 {
		results = append(results, &oraclev1.AggregatedResult{
			Symbol:      fmt.Sprintf("ASSET-%03d/USD", i),
			Value:       "1.000000000000000000",
			SourceCount: 3,
		})
	}
	sidecar := &testSidecar{results: results}
	socket, _ := startTestSidecar(t, sidecar)
	handler := mustNewVoteExtensionHandler(t, fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 1, HistoryLimit: 100},
		tasks:  []*oraclev1.OracleTask{numericTask("BTC/USD")},
	}, true, socket, time.Second)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})

	resp, err := handler.ExtendVote(
		sdk.Context{}.WithContext(context.Background()),
		&abcitypes.RequestExtendVote{Height: 12},
	)
	require.NoError(t, err)
	require.Empty(t, resp.GetVoteExtension())
	require.Equal(t, int32(1), sidecar.CallCount())
}

func TestValidatorResultsFromAggregatesPoisonsOnlyDuplicatedRequestedSymbol(t *testing.T) {
	tasksBySymbol := map[string]*oraclev1.OracleTask{
		"BTC/USD": numericTask("BTC/USD"),
		"ETH/USD": numericTask("ETH/USD"),
	}

	results := validatorResultsFromAggregates(tasksBySymbol, []*oraclev1.AggregatedResult{
		{Symbol: "DOGE/USD", Value: "9.000000000000000000", SourceCount: 3},
		{Symbol: "BTC/USD", Value: "1.000000000000000000", SourceCount: 3},
		{Symbol: "BTC/USD", Value: "2.000000000000000000", SourceCount: 3},
		nil,
		{Symbol: "ETH/USD", Value: "3.000000000000000000", SourceCount: 3},
	}, 3)

	require.Equal(t, []*oraclev1.OracleValidatorResult{{
		Symbol:      "ETH/USD",
		Value:       "3.000000000000000000",
		SourceCount: 3,
	}}, results)
}

func TestValidatorResultsFromAggregatesTreatsCanonicalAliasAsDuplicate(t *testing.T) {
	tasksBySymbol := map[string]*oraclev1.OracleTask{
		"BTC/USD": numericTask("BTC/USD"),
		"ETH/USD": numericTask("ETH/USD"),
	}

	results := validatorResultsFromAggregates(tasksBySymbol, []*oraclev1.AggregatedResult{
		{Symbol: " eth/usd ", Value: "2.000000000000000000", SourceCount: 3},
		{Symbol: "ETH/USD", Value: "2.000000000000000000", SourceCount: 3},
		{Symbol: "BTC/USD", Value: "1.000000000000000000", SourceCount: 3},
	}, 3)

	require.Equal(t, []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}, results)
}

func TestValidatorResultsFromAggregatesEnforcesCanonicalFieldsAndSourceThreshold(t *testing.T) {
	tasksBySymbol := map[string]*oraclev1.OracleTask{
		"BTC/USD": numericTask("BTC/USD"),
	}
	tests := []struct {
		name      string
		aggregate *oraclev1.AggregatedResult
	}{
		{name: "nil result", aggregate: nil},
		{name: "empty symbol", aggregate: &oraclev1.AggregatedResult{Value: "1.000000000000000000", SourceCount: 3}},
		{name: "unknown symbol", aggregate: &oraclev1.AggregatedResult{Symbol: "ETH/USD", Value: "1.000000000000000000", SourceCount: 3}},
		{name: "non-canonical case", aggregate: &oraclev1.AggregatedResult{Symbol: "btc/usd", Value: "1.000000000000000000", SourceCount: 3}},
		{name: "non-canonical whitespace", aggregate: &oraclev1.AggregatedResult{Symbol: " BTC/USD ", Value: "1.000000000000000000", SourceCount: 3}},
		{name: "empty value", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", SourceCount: 3}},
		{name: "invalid value", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", Value: "not-a-number", SourceCount: 3}},
		{name: "non-canonical value", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", Value: "1.0", SourceCount: 3}},
		{name: "oversized value", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", Value: strings.Repeat("1", maxSidecarValueBytes+1), SourceCount: 3}},
		{name: "zero sources", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", Value: "1.000000000000000000"}},
		{name: "below minimum sources", aggregate: &oraclev1.AggregatedResult{Symbol: "BTC/USD", Value: "1.000000000000000000", SourceCount: 2}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := validatorResultsFromAggregates(
				tasksBySymbol,
				[]*oraclev1.AggregatedResult{tc.aggregate},
				3,
			)
			require.Empty(t, results)
		})
	}

	results := validatorResultsFromAggregates(tasksBySymbol, []*oraclev1.AggregatedResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}, 3)
	require.Equal(t, []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}, results)
}

func TestVerifyVoteExtensionEnforcesResultMaximumAndCanonicalFields(t *testing.T) {
	valid := func() *oraclev1.OracleValidatorResult {
		return &oraclev1.OracleValidatorResult{
			Symbol:      "BTC/USD",
			Value:       "1.000000000000000000",
			SourceCount: 3,
		}
	}
	tests := []struct {
		name      string
		extension *oraclev1.OracleVoteExtension
	}{
		{name: "nil extension", extension: nil},
		{name: "nil result", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{nil}}},
		{name: "empty symbol", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Value: "1.000000000000000000", SourceCount: 3}}}},
		{name: "non-canonical symbol", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "btc/usd", Value: "1.000000000000000000", SourceCount: 3}}}},
		{name: "oversized symbol", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: strings.Repeat("X", maxSidecarSymbolBytes+1), Value: "1.000000000000000000", SourceCount: 3}}}},
		{name: "duplicate symbol", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{valid(), valid()}}},
		{name: "empty value", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "BTC/USD", SourceCount: 3}}}},
		{name: "invalid value", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "BTC/USD", Value: "invalid", SourceCount: 3}}}},
		{name: "non-canonical value", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "BTC/USD", Value: "1.0", SourceCount: 3}}}},
		{name: "oversized value", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "BTC/USD", Value: strings.Repeat("1", maxSidecarValueBytes+1), SourceCount: 3}}}},
		{name: "zero sources", extension: &oraclev1.OracleVoteExtension{Results: []*oraclev1.OracleValidatorResult{{Symbol: "BTC/USD", Value: "1.000000000000000000"}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, ValidateVoteExtension(tc.extension))
		})
	}

	maxResults := make([]*oraclev1.OracleValidatorResult, 0, maxSidecarSymbols)
	for i := range maxSidecarSymbols {
		maxResults = append(maxResults, &oraclev1.OracleValidatorResult{
			Symbol:      fmt.Sprintf("ASSET-%03d/USD", i),
			Value:       "1.000000000000000000",
			SourceCount: 3,
		})
	}
	require.NoError(t, ValidateVoteExtension(&oraclev1.OracleVoteExtension{Results: maxResults}))

	tooMany := append(append([]*oraclev1.OracleValidatorResult(nil), maxResults...), &oraclev1.OracleValidatorResult{
		Symbol:      "OVERFLOW/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	})
	require.Error(t, ValidateVoteExtension(&oraclev1.OracleVoteExtension{Results: tooMany}))
}

func TestVerifyVoteExtensionAcceptsEmptyAndRejectsMalformedOrNonCanonical(t *testing.T) {
	handler := mustNewVoteExtensionHandler(t, nil, true, "", 0)

	empty, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, empty.GetStatus())

	for _, extension := range [][]byte{
		[]byte("bad"),
		mustMarshalVoteExtension(t, []*oraclev1.OracleValidatorResult{{
			Symbol:      "BTC/USD",
			Value:       "1.0",
			SourceCount: 3,
		}}),
	} {
		resp, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{
			VoteExtension: extension,
		})
		require.NoError(t, err)
		require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, resp.GetStatus())
	}

	valid := mustMarshalVoteExtension(t, []*oraclev1.OracleValidatorResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}})
	resp, err := handler.VerifyVoteExtension(sdk.Context{}, &abcitypes.RequestVerifyVoteExtension{
		VoteExtension: valid,
	})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, resp.GetStatus())
}

func TestExtendVoteReturnsEmptyOnSidecarFailures(t *testing.T) {
	var paramsCalls atomic.Int32
	keeper := fakeKeeper{
		params:      &oraclev1.Params{MinValidators: 1, MinSources: 1, HistoryLimit: 100},
		tasks:       []*oraclev1.OracleTask{numericTask("BTC/USD")},
		paramsCalls: &paramsCalls,
	}
	ctx := sdk.Context{}.WithContext(context.Background())

	unsetSocketHandler := mustNewVoteExtensionHandler(t, keeper, true, "", time.Millisecond)
	unsetSocketResp, err := unsetSocketHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, unsetSocketResp.GetVoteExtension())

	timeoutSidecar := &testSidecar{delay: 50 * time.Millisecond}
	timeoutSocket, _ := startTestSidecar(t, timeoutSidecar)
	timeoutHandler := mustNewVoteExtensionHandler(t, keeper, true, timeoutSocket, time.Millisecond)
	t.Cleanup(func() {
		require.NoError(t, timeoutHandler.Close())
	})
	timeoutResp, err := timeoutHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, timeoutResp.GetVoteExtension())

	errorSidecar := &testSidecar{err: errors.New("daemon error")}
	errorSocket, _ := startTestSidecar(t, errorSidecar)
	errorHandler := mustNewVoteExtensionHandler(t, keeper, true, errorSocket, time.Second)
	t.Cleanup(func() {
		require.NoError(t, errorHandler.Close())
	})
	errorResp, err := errorHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, errorResp.GetVoteExtension())

	missingSocket := filepath.Join(t.TempDir(), "missing.sock")
	missingHandler := mustNewVoteExtensionHandler(t, keeper, true, missingSocket, 10*time.Millisecond)
	t.Cleanup(func() {
		require.NoError(t, missingHandler.Close())
	})
	missingResp, err := missingHandler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Empty(t, missingResp.GetVoteExtension())
	require.Zero(t, paramsCalls.Load(), "sidecar failures should not read on-chain params")
}

func TestVoteExtensionHandlerReconnectsAfterSocketRecreation(t *testing.T) {
	socket := shortTestUnixSocketPath(t)

	firstSidecar := &testSidecar{results: []*oraclev1.AggregatedResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}}
	firstServer := startTestSidecarAt(t, socket, firstSidecar)

	handler := mustNewVoteExtensionHandler(t, fakeKeeper{
		params: &oraclev1.Params{MinValidators: 1, MinSources: 3, HistoryLimit: 100},
		tasks:  []*oraclev1.OracleTask{numericTask("BTC/USD")},
	}, true, socket, 100*time.Millisecond)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})
	ctx := sdk.Context{}.WithContext(context.Background())

	firstResp, err := handler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 12})
	require.NoError(t, err)
	require.Equal(
		t,
		"1.000000000000000000",
		mustDecodeVoteExtension(t, firstResp.GetVoteExtension()).GetResults()[0].GetValue(),
	)

	firstServer.Stop()
	unavailableResp, err := handler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 13})
	require.NoError(t, err)
	require.Empty(t, unavailableResp.GetVoteExtension())

	secondSidecar := &testSidecar{results: []*oraclev1.AggregatedResult{{
		Symbol:      "BTC/USD",
		Value:       "2.000000000000000000",
		SourceCount: 3,
	}}}
	startTestSidecarAt(t, socket, secondSidecar)

	require.Eventually(t, func() bool {
		resp, extendErr := handler.ExtendVote(ctx, &abcitypes.RequestExtendVote{Height: 14})
		if extendErr != nil || len(resp.GetVoteExtension()) == 0 {
			return false
		}
		extension, decodeErr := DecodeVoteExtension(resp.GetVoteExtension())
		return decodeErr == nil &&
			len(extension.GetResults()) == 1 &&
			extension.GetResults()[0].GetValue() == "2.000000000000000000"
	}, 5*time.Second, 50*time.Millisecond)
	require.Positive(t, secondSidecar.CallCount())
}

func mustNewVoteExtensionHandler(
	t testing.TB,
	keeper Keeper,
	enabled bool,
	socket string,
	timeout time.Duration,
) *VoteExtensionHandler {
	t.Helper()

	handler, err := NewVoteExtensionHandler(keeper, enabled, socket, timeout)
	require.NoError(t, err)
	require.NotNil(t, handler)
	return handler
}

func numericTask(symbol string) *oraclev1.OracleTask {
	return &oraclev1.OracleTask{
		Symbol:             symbol,
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}
}

func mustMarshalVoteExtension(t *testing.T, results []*oraclev1.OracleValidatorResult) []byte {
	t.Helper()

	bz, err := (&oraclev1.OracleVoteExtension{Results: results}).Marshal()
	require.NoError(t, err)
	return bz
}

func mustDecodeVoteExtension(t *testing.T, bz []byte) *oraclev1.OracleVoteExtension {
	t.Helper()

	extension, err := DecodeVoteExtension(bz)
	require.NoError(t, err)
	return extension
}

type testSidecar struct {
	oraclev1.UnimplementedOracleSidecarServer

	delay   time.Duration
	err     error
	results []*oraclev1.AggregatedResult
	calls   atomic.Int32

	mu       sync.Mutex
	requests [][]string
}

func (s *testSidecar) GetAggregates(
	ctx context.Context,
	request *oraclev1.GetAggregatesRequest,
) (*oraclev1.GetAggregatesResponse, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.requests = append(s.requests, append([]string(nil), request.GetSymbols()...))
	s.mu.Unlock()

	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, status.Error(codes.Unavailable, s.err.Error())
	}

	return &oraclev1.GetAggregatesResponse{Results: s.results}, nil
}

func (s *testSidecar) CallCount() int32 {
	return s.calls.Load()
}

func (s *testSidecar) Requests() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	requests := make([][]string, len(s.requests))
	for i := range s.requests {
		requests[i] = append([]string(nil), s.requests[i]...)
	}
	return requests
}

type runningTestSidecar struct {
	server   *grpc.Server
	listener net.Listener
	stopOnce sync.Once
}

func startTestSidecar(t *testing.T, sidecar *testSidecar) (string, *runningTestSidecar) {
	t.Helper()

	socket := shortTestUnixSocketPath(t)

	return socket, startTestSidecarAt(t, socket, sidecar)
}

func shortTestUnixSocketPath(tb testing.TB) string {
	tb.Helper()

	tmp, err := os.CreateTemp("", "o-")
	require.NoError(tb, err)
	path := tmp.Name()
	require.NoError(tb, tmp.Close())
	require.NoError(tb, os.Remove(path))
	tb.Cleanup(func() { _ = os.Remove(path) })

	return path
}

func startTestSidecarAt(t *testing.T, socket string, sidecar *testSidecar) *runningTestSidecar {
	t.Helper()

	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)

	server := grpc.NewServer()
	oraclev1.RegisterOracleSidecarServer(server, sidecar)
	running := &runningTestSidecar{server: server, listener: listener}
	t.Cleanup(running.Stop)

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
			t.Errorf("oracle sidecar test server stopped: %v", serveErr)
		}
	}()

	return running
}

func (s *runningTestSidecar) Stop() {
	s.stopOnce.Do(func() {
		s.server.Stop()
		_ = s.listener.Close()
	})
}
