package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

func TestConfiguredSymbolResolution(t *testing.T) {
	t.Parallel()
	feeds := []domain.FeedPlan{
		{Symbol: "BTC-USD"},
		{Symbol: "BTC/USD"},
		{Symbol: "ETH/USD"},
	}
	tests := []struct {
		name    string
		input   string
		feeds   []domain.FeedPlan
		want    string
		wantErr string
	}{
		{name: "exact slash", input: "BTC/USD", want: "BTC/USD"},
		{name: "exact hyphen", input: "BTC-USD", want: "BTC-USD"},
		{name: "case insensitive", input: "eth/usd", want: "ETH/USD"},
		{name: "whitespace", input: "  eth/usd  ", want: "ETH/USD"},
		{name: "underscore alias", input: "eth_usd", want: "ETH/USD"},
		{name: "ambiguous alias", input: "btc_usd", wantErr: "ambiguous"},
		{name: "unknown", input: "SOL/USD", wantErr: "not configured"},
		{name: "no configured symbols", input: "BTC/USD", feeds: []domain.FeedPlan{}, wantErr: "no symbols are configured"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testFeeds := feeds
			if test.feeds != nil {
				testFeeds = test.feeds
			}
			got, err := resolveConfiguredSymbol(test.input, testFeeds)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("resolveConfiguredSymbol(%q) error = %v", test.input, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("resolveConfiguredSymbol(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestLiveHistorySummaryUsesOneBoundedRequestPerFeed(t *testing.T) {
	t.Parallel()
	socket := shortAdminSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu       sync.Mutex
		requests []string
	)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		symbol := request.URL.Query().Get("symbol")
		mu.Lock()
		requests = append(requests, symbol+":"+request.URL.Query().Get("page_size"))
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(service.SuccessEnvelope[service.HistoryData]{
			SchemaVersion: 1,
			Command:       "history",
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Data: service.HistoryData{
				Symbol: symbol,
				Records: []service.HistoryRecord{{
					Value:                 "10.000000000000000000",
					Sequence:              "1",
					ConfiguredSourceCount: 3,
					SuccessfulSourceCount: 3,
					CollectedAt:           time.Now().UTC().Format(time.RFC3339Nano),
					Provenance:            "current",
				}},
			},
		})
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-done
	})
	pair := &config.Pair{
		Paths: config.Paths{Home: "/private/tmp/oracle", AdminSocket: socket},
		Feeds: []domain.FeedPlan{
			{Symbol: "BTC/USD", Sources: make([]domain.SourcePlan, 3)},
			{Symbol: "ETH/USD", Sources: make([]domain.SourcePlan, 3)},
		},
	}
	view, err := liveHistorySummary(t.Context(), pair)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Rows) != 2 || view.Rows[0].Value != "10.000000000000000000" {
		t.Fatalf("summary view = %#v", view)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if want := []string{"BTC/USD:1", "ETH/USD:1"}; !reflect.DeepEqual(gotRequests, want) {
		t.Fatalf("history requests = %q, want %q", gotRequests, want)
	}
}

func TestLiveHistorySummaryProbesStatusWhenNoFeedsAreConfigured(t *testing.T) {
	t.Parallel()
	pair := &config.Pair{
		Paths: config.Paths{
			Home:        "/private/tmp/oracle",
			AdminSocket: filepath.Join(shortAdminDirectory(t), "missing.sock"),
		},
	}
	if _, err := liveHistorySummary(t.Context(), pair); err == nil || !isAdminTransportError(err) {
		t.Fatalf("stopped zero-feed summary error = %v", err)
	}

	socket := shortAdminSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/status" {
			t.Errorf("probe path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(service.SuccessEnvelope[service.StatusData]{
			SchemaVersion: 1,
			Command:       "status",
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Data: service.StatusData{
				Health:               "unavailable",
				ActivationGeneration: "0",
				Feeds:                []service.FeedStatus{},
			},
		})
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-done
	})
	pair.Paths.AdminSocket = socket
	view, err := liveHistorySummary(t.Context(), pair)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "live" || len(view.Rows) != 0 {
		t.Fatalf("zero-feed view = %#v", view)
	}
}

func TestStoppedCommandsUseLockProtectedOfflineViews(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name:     "status summary",
			args:     []string{"status"},
			contains: []string{"Oracle daemon is stopped.", "Configuration  pending activation", "BTC/USD", "no records"},
		},
		{
			name:     "status alias detail",
			args:     []string{"status", "btc-usd"},
			contains: []string{"Oracle daemon is stopped.", "Stored feed", "BTC/USD"},
		},
		{
			name:     "history summary",
			args:     []string{"history"},
			contains: []string{"Stored aggregate summary.", "Mode   offline", "BTC/USD", "ETH/USD", "SOL/USD", "no records"},
		},
		{
			name:     "history alias detail",
			args:     []string{"history", "btc-usd"},
			contains: []string{"History for BTC/USD.", "Mode     offline", "No retained records."},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			args := append([]string{"--home", home}, test.args...)
			if code := Run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("success wrote stderr: %q", stderr.String())
			}
			for _, expected := range test.contains {
				if !strings.Contains(stdout.String(), expected) {
					t.Fatalf("output missing %q:\n%s", expected, stdout.String())
				}
			}
		})
	}
}

func TestMachineModesDoNotGainAutomaticOfflineSchemas(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "status", "--format", "json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope service.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("decode status error: %v: %q", err, stderr.String())
	}
	if envelope.Error.Code != "daemon_unavailable" {
		t.Fatalf("status error = %#v", envelope.Error)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "history", "--format", "json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("history exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("decode history error: %v: %q", err, stderr.String())
	}
	if envelope.Error.Code != "invalid_arguments" {
		t.Fatalf("history error = %#v", envelope.Error)
	}
}

func TestOfflineFallbackRefusesHeldHomeLock(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	lockPath, err := config.CanonicalHomeLockPath(home)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close lock: %v", err)
		}
	})
	for _, args := range [][]string{
		{"--home", home, "status"},
		{"--home", home, "history"},
		{"--home", home, "history", "btc-usd"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(args, &stdout, &stderr); code != 1 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		if stdout.Len() != 0 ||
			!strings.Contains(stderr.String(), "The daemon may be running, starting, or stopping.") ||
			strings.Contains(stderr.String(), "Oracle daemon is stopped.") {
			t.Fatalf("%v held-lock fallback output stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestAdminProtocolFailureNeverFallsBackOffline(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	pair, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", pair.Paths.AdminSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("{}"))
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-done
	})
	for _, args := range [][]string{
		{"--home", home, "status"},
		{"--home", home, "history"},
		{"--home", home, "history", "btc-usd"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(args, &stdout, &stderr); code != 1 {
			t.Fatalf("%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		if stdout.Len() != 0 ||
			!strings.Contains(stderr.String(), "incompatible or invalid admin response") ||
			strings.Contains(stderr.String(), "Oracle daemon is stopped.") {
			t.Fatalf("%v protocol failure output stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestOfflineFallbackReloadsConfigurationAfterLock(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	pair, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", pair.Paths.AdminSocket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		writeErr := os.WriteFile(pair.Paths.SourcesFile, []byte("invalid publication"), 0o600)
		closeErr := connection.Close()
		done <- errors.Join(writeErr, closeErr)
	}()

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"--home", home, "status"}, &stdout, &stderr)
	_ = listener.Close()
	if serverErr := <-done; serverErr != nil {
		t.Fatalf("raw admin server: %v", serverErr)
	}
	if code != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "published configuration is invalid") ||
		strings.Contains(stderr.String(), "Oracle daemon is stopped.") {
		t.Fatalf("post-lock reload exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestOfflineViewsUseLatestStoredAggregateWithoutStatusValueLeak(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	seedOfflineAggregate(t, home)

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "history"}, &stdout, &stderr); code != 0 {
		t.Fatalf("history exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "63699.135") ||
		strings.Contains(stdout.String(), "63699.135000000000000000") {
		t.Fatalf("history value is not human formatted:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "63699.135") ||
		!strings.Contains(stdout.String(), "Configuration  activated") ||
		!strings.Contains(stdout.String(), "stored") {
		t.Fatalf("offline status leaked a value or omitted metadata:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{
		"--home", home,
		"history", "btc-usd",
		"--offline",
		"--format", "json",
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("offline JSON history exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope service.SuccessEnvelope[service.HistoryData]
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode offline history: %v", err)
	}
	if envelope.Data.Symbol != "BTC/USD" ||
		len(envelope.Data.Records) != 1 ||
		envelope.Data.Records[0].Value != "63699.135000000000000000" {
		t.Fatalf("offline history envelope = %#v", envelope.Data)
	}
}

func seedOfflineAggregate(t *testing.T, home string) {
	t.Helper()
	lockPath, err := config.CanonicalHomeLockPath(home)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close home lock: %v", err)
		}
	}()
	pair, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(pair.Paths.Database, pair.Paths.Marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()
	catalog, err := store.Activate(
		pair.PlanDigest,
		pair.Feeds,
		pair.Config.Storage.HistoryRetention,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	feed := pair.Feeds[0]
	contributors := make([]string, 3)
	for i := range contributors {
		contributors[i] = feed.Sources[i].ID
	}
	now := time.Date(2026, 8, 4, 3, 0, 0, 0, time.UTC)
	_, err = store.Insert(domain.Aggregate{
		Symbol:               feed.Symbol,
		Value:                "63699.135000000000000000",
		ActivationGeneration: catalog.ActivationGeneration,
		CycleStartedAt:       now.Add(-time.Second),
		CollectedAt:          now,
		ConfiguredSources:    uint32(len(feed.Sources)),
		SuccessfulSources:    uint32(len(contributors)),
		ContributorIDs:       contributors,
		FeedPlanFingerprint:  feed.Fingerprint,
	}, pair.Config.Storage.HistoryRetention)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOfflineStoreHoldsCanonicalLockDuringRead(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	lockPath, err := config.CanonicalHomeLockPath(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := withOfflineStore(home, func(pair *config.Pair, store *storage.Store) error {
		if pair.Paths.Home != home {
			t.Fatalf("post-lock pair home = %q, want %q", pair.Paths.Home, home)
		}
		if _, err := store.LatestRecords(); err != nil {
			t.Fatalf("read offline store: %v", err)
		}
		second, err := storage.AcquireHomeLock(lockPath)
		if !errors.Is(err, storage.ErrHomeLocked) {
			if second != nil {
				_ = second.Close()
			}
			t.Fatalf("offline callback did not retain home lock: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatalf("offline helper did not release home lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateProvenanceRequiresPublishedPlan(t *testing.T) {
	t.Parallel()
	feed := domain.FeedPlan{Symbol: "BTC/USD", Fingerprint: [32]byte{1}}
	pair := &config.Pair{PlanDigest: [32]byte{2}}
	catalog := storage.Catalog{
		ActivationGeneration: 3,
		PlanDigest:           pair.PlanDigest,
	}
	record := domain.Aggregate{
		ActivationGeneration: 3,
		FeedPlanFingerprint:  feed.Fingerprint,
	}
	if got := aggregateProvenance(pair, catalog, feed, record); got != "current" {
		t.Fatalf("matching provenance = %q", got)
	}
	catalog.PlanDigest = [32]byte{4}
	if got := aggregateProvenance(pair, catalog, feed, record); got != "prior" {
		t.Fatalf("pending-plan provenance = %q", got)
	}
}

func TestHumanAdminDataValidationRejectsUnsafeSemantics(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	validStatus := service.StatusData{
		Health:               "healthy",
		ActivationGeneration: "1",
		Feeds: []service.FeedStatus{{
			Symbol:                "BTC/USD",
			ConfiguredSourceCount: 3,
			IntervalNanos:         "10000000000",
			StaleAfterNanos:       "20000000000",
			Freshness:             "fresh",
			Health:                "healthy",
			Latest: &service.LatestStatus{
				Sequence:              "1",
				CollectedAt:           now,
				SuccessfulSourceCount: 3,
			},
			Cycle: service.CycleStatus{
				Activity:              "idle",
				LastOutcome:           "full",
				SuccessfulSourceCount: 3,
			},
		}},
	}
	if err := validateHumanStatusData(validStatus); err != nil {
		t.Fatalf("valid status: %v", err)
	}
	invalidStatus := validStatus
	invalidStatus.Feeds = append([]service.FeedStatus(nil), validStatus.Feeds...)
	invalidStatus.Feeds[0].Freshness = "future"
	if err := validateHumanStatusData(invalidStatus); err == nil {
		t.Fatal("invalid freshness unexpectedly passed")
	}
	invalidStatus = validStatus
	invalidStatus.Feeds = append([]service.FeedStatus(nil), validStatus.Feeds...)
	invalidStatus.Feeds[0].Latest = nil
	if err := validateHumanStatusData(invalidStatus); err == nil {
		t.Fatal("fresh status without aggregate metadata unexpectedly passed")
	}

	validHistory := service.HistoryData{Records: []service.HistoryRecord{{
		Value:                 "10.000000000000000000",
		Sequence:              "1",
		CollectedAt:           now,
		ConfiguredSourceCount: 3,
		SuccessfulSourceCount: 3,
		Provenance:            "current",
	}}}
	if err := validateHumanHistoryData(validHistory); err != nil {
		t.Fatalf("valid history: %v", err)
	}
	invalidHistory := validHistory
	invalidHistory.Records = append([]service.HistoryRecord(nil), validHistory.Records...)
	invalidHistory.Records[0].Value = "10.0"
	if err := validateHumanHistoryData(invalidHistory); err == nil {
		t.Fatal("non-canonical history decimal unexpectedly passed")
	}
	invalidHistory = validHistory
	invalidHistory.Records = append([]service.HistoryRecord(nil), validHistory.Records...)
	invalidHistory.Records[0].Sequence = "01"
	if err := validateHumanHistoryData(invalidHistory); err == nil {
		t.Fatal("non-canonical history sequence unexpectedly passed")
	}
	invalidKey := "not+a+page+key"
	invalidHistory = validHistory
	invalidHistory.NextPageKey = &invalidKey
	if err := validateHumanHistoryData(invalidHistory); err == nil {
		t.Fatal("invalid history page key unexpectedly passed")
	}
}
