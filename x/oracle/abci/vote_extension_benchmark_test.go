package abci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc"
)

func BenchmarkAggregateVoteExtensionPayload(b *testing.B) {
	for _, symbolCount := range []int{1, 10, 64, 256} {
		b.Run(fmt.Sprintf("symbols=%d", symbolCount), func(b *testing.B) {
			tasks := make([]*oraclev1.OracleTask, 0, symbolCount)
			aggregates := make([]*oraclev1.AggregatedResult, 0, symbolCount)
			for symbolIndex := range symbolCount {
				symbol := fmt.Sprintf("ASSET-%03d/USD", symbolIndex)
				tasks = append(tasks, numericTask(symbol))
				aggregates = append(aggregates, &oraclev1.AggregatedResult{
					Symbol:      symbol,
					Value:       fmt.Sprintf("%d.125000000000000000", symbolIndex+1),
					SourceCount: 3,
				})
			}

			symbols, _, ok := aggregateRequestFromTasks(tasks)
			if !ok {
				b.Fatal("benchmark task set unexpectedly exceeded request limits")
			}
			request := &oraclev1.GetAggregatesRequest{Symbols: symbols}
			response := &oraclev1.GetAggregatesResponse{Results: aggregates}
			b.ReportAllocs()
			b.ReportMetric(float64(request.Size()), "request-bytes")
			b.ReportMetric(float64(response.Size()), "response-bytes")
			b.ResetTimer()

			for range b.N {
				_, tasksBySymbol, valid := aggregateRequestFromTasks(tasks)
				if !valid {
					b.Fatal("benchmark task set unexpectedly exceeded request limits")
				}
				results := validatorResultsFromAggregates(tasksBySymbol, aggregates, 3)
				if len(results) != symbolCount {
					b.Fatalf("got %d results, want %d", len(results), symbolCount)
				}
				if _, err := (&oraclev1.OracleVoteExtension{Results: results}).Marshal(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAggregateRequestRejectsOversizedTaskSet(b *testing.B) {
	tasks := make([]*oraclev1.OracleTask, 1<<16)
	for i := range maxSidecarSymbols + 1 {
		tasks[i] = numericTask(fmt.Sprintf("ASSET-%03d/USD", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		symbols, tasksBySymbol, ok := aggregateRequestFromTasks(tasks)
		if ok || symbols != nil || tasksBySymbol != nil {
			b.Fatal("oversized task set unexpectedly accepted")
		}
	}
}

func BenchmarkFetchAggregatesPersistentConnection(b *testing.B) {
	socket := shortTestUnixSocketPath(b)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		b.Fatal(err)
	}
	server := grpc.NewServer()
	oraclev1.RegisterOracleSidecarServer(server, &testSidecar{results: []*oraclev1.AggregatedResult{{
		Symbol:      "BTC/USD",
		Value:       "1.000000000000000000",
		SourceCount: 3,
	}}})
	b.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
			panic(serveErr)
		}
	}()

	handler := mustNewVoteExtensionHandler(b, nil, true, socket, time.Second)
	b.Cleanup(func() {
		if closeErr := handler.Close(); closeErr != nil {
			b.Error(closeErr)
		}
	})
	request := &oraclev1.GetAggregatesRequest{Symbols: []string{"BTC/USD"}}

	// Warm the lazy gRPC connection before measuring steady-state calls.
	if _, err := handler.fetchAggregates(context.Background(), request); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, fetchErr := handler.fetchAggregates(context.Background(), request)
		if fetchErr != nil {
			b.Fatal(fetchErr)
		}
		if len(response.GetResults()) != 1 {
			b.Fatalf("got %d results, want 1", len(response.GetResults()))
		}
	}
}
