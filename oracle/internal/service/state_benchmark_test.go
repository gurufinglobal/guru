package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

func BenchmarkStateUpdate256Feeds(b *testing.B) {
	state, symbols := benchmarkState(b, 256, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		state.CycleStarted(symbols[i%len(symbols)])
	}
}

func BenchmarkStateFresh256Feeds(b *testing.B) {
	state, symbols := benchmarkState(b, 256, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if values := state.Fresh(symbols); len(values) != len(symbols) {
			b.Fatalf("fresh values = %d, want %d", len(values), len(symbols))
		}
	}
}

func benchmarkState(tb testing.TB, feedCount, contributorCount int) (*State, []string) {
	tb.Helper()
	now := time.Now()
	feeds := make([]domain.FeedPlan, feedCount)
	symbols := make([]string, feedCount)
	latest := make(map[string]domain.Aggregate, feedCount)
	contributors := make([]string, contributorCount)
	for i := range contributors {
		contributors[i] = fmt.Sprintf("source-%03d", i)
	}
	for i := range feeds {
		symbol := fmt.Sprintf("ASSET%03d/USD", i)
		fingerprint := [32]byte{byte(i), byte(i >> 8)}
		feeds[i] = domain.FeedPlan{
			Symbol:      symbol,
			Interval:    time.Second,
			StaleAfter:  time.Minute,
			Fingerprint: fingerprint,
		}
		symbols[i] = symbol
		latest[symbol] = domain.Aggregate{
			Symbol:               symbol,
			Value:                "1.000000000000000000",
			Sequence:             1,
			ActivationGeneration: 1,
			CycleStartedAt:       now.Add(-time.Second),
			CollectedAt:          now,
			ConfiguredSources:    uint32(contributorCount),
			SuccessfulSources:    uint32(contributorCount),
			ContributorIDs:       append([]string(nil), contributors...),
			FeedPlanFingerprint:  fingerprint,
		}
	}
	pair := &config.Pair{
		Config: config.File{
			PublicationRevision: "benchmark",
			SourcesSHA256:       "benchmark",
		},
		Feeds: feeds,
	}
	return NewState(pair, storage.Catalog{ActivationGeneration: 1}, latest, now), symbols
}
