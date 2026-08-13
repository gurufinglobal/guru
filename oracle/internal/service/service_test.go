package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestConsumerReturnsOnlyCanonicalFreshSnapshotValues(t *testing.T) {
	t.Parallel()
	pair, catalog, latest := serviceFixture(t, time.Now().UTC())
	state := NewState(pair, catalog, latest, time.Now())
	server := NewConsumerServer(state, 64<<10, 1<<20)
	response, err := server.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Value != "10.000000000000000000" ||
		response.Results[0].SourceCount != 3 {
		t.Fatalf("response = %#v", response)
	}
	if _, err := server.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"btc/usd"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("non-canonical request error = %v", err)
	}
	if _, err := server.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD", "BTC/USD"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate request error = %v", err)
	}

	futurePair, futureCatalog, futureLatest := serviceFixture(t, time.Now().UTC().Add(time.Hour))
	futureState := NewState(futurePair, futureCatalog, futureLatest, time.Now())
	futureServer := NewConsumerServer(futureState, 64<<10, 1<<20)
	response, err = futureServer.GetAggregates(context.Background(), &oraclev1.GetAggregatesRequest{
		Symbols: []string{"BTC/USD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("clock-anomalous value was served: %#v", response.Results)
	}
}

func TestPriorGenerationLatestIsReportedButNotServed(t *testing.T) {
	t.Parallel()
	pair, catalog, latest := serviceFixture(t, time.Now().UTC())
	record := latest["BTC/USD"]
	record.ActivationGeneration = catalog.ActivationGeneration - 1
	latest["BTC/USD"] = record
	state := NewState(pair, catalog, latest, time.Now())

	if values := state.freshAt([]string{"BTC/USD"}, time.Now()); len(values) != 0 {
		t.Fatalf("prior-generation record was served: %#v", values)
	}
	status := state.statusAt(time.Now())
	if len(status.Feeds) != 1 || status.Feeds[0].Latest != nil ||
		status.Feeds[0].OmissionReason == nil || *status.Feeds[0].OmissionReason != "prior_generation" {
		t.Fatalf("prior-generation status = %#v", status.Feeds)
	}
}

func TestAdminStatusAndHistoryContracts(t *testing.T) {
	t.Parallel()
	pair, _, latest := serviceFixture(t, time.Now().UTC())
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := storage.Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()
	activated, err := store.Activate(pair.PlanDigest, pair.Feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	record := latest["BTC/USD"]
	record.ActivationGeneration = activated.ActivationGeneration
	record.FeedPlanFingerprint = pair.Feeds[0].Fingerprint
	if _, err := store.Insert(record, 30); err != nil {
		t.Fatal(err)
	}
	state := NewState(pair, activated, latest, time.Now())
	server := NewAdminServer(state, store)

	statusRecorder := httptest.NewRecorder()
	server.ServeHTTP(statusRecorder, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", statusRecorder.Code, statusRecorder.Body)
	}
	if !ValidateAdminContentType(statusRecorder.Header().Get("Content-Type")) {
		t.Fatalf("content type = %q", statusRecorder.Header().Get("Content-Type"))
	}
	var statusEnvelope SuccessEnvelope[StatusData]
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatal(err)
	}
	if statusEnvelope.Data.Feeds[0].Latest == nil || statusEnvelope.Data.Feeds[0].Health != "degraded" {
		t.Fatalf("status = %#v", statusEnvelope.Data.Feeds[0])
	}
	if strings.Contains(statusRecorder.Body.String(), `"value"`) {
		t.Fatal("status leaked aggregate value")
	}

	historyRecorder := httptest.NewRecorder()
	path := "/v1/history?symbol=" + url.QueryEscape("BTC/USD") + "&page_size=1"
	server.ServeHTTP(historyRecorder, httptest.NewRequest(http.MethodGet, path, nil))
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("history code = %d body=%s", historyRecorder.Code, historyRecorder.Body)
	}
	var historyEnvelope SuccessEnvelope[HistoryData]
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &historyEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(historyEnvelope.Data.Records) != 1 ||
		historyEnvelope.Data.Records[0].Provenance != "current" {
		t.Fatalf("history = %#v", historyEnvelope.Data)
	}

	badRecorder := httptest.NewRecorder()
	server.ServeHTTP(badRecorder, httptest.NewRequest(http.MethodPost, "/v1/status", nil))
	if badRecorder.Code != http.StatusMethodNotAllowed || badRecorder.Header().Get("Allow") != "GET" {
		t.Fatalf("method response = %d %s", badRecorder.Code, badRecorder.Body)
	}
}

func TestAdminStorageFailureSignalsFatalOnce(t *testing.T) {
	pair, catalog, latest := serviceFixture(t, time.Now())
	state := NewState(pair, catalog, latest, time.Now())
	fatal := make(chan error, 2)
	server := newAdminServerWithFatal(state, failingHistoryStore{catalog: catalog}, func(err error) {
		fatal <- err
	})

	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodGet, "/v1/history?symbol=BTC%2FUSD&page_size=1", nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusConflict ||
			!strings.Contains(response.Body.String(), `"code":"storage_unavailable"`) {
			t.Fatalf("history response = %d %s", response.Code, response.Body.String())
		}
	}
	select {
	case err := <-fatal:
		if !strings.Contains(err.Error(), "storage frame corrupted") {
			t.Fatalf("fatal error = %v", err)
		}
	default:
		t.Fatal("storage failure did not signal fatal shutdown")
	}
	select {
	case err := <-fatal:
		t.Fatalf("storage failure signaled more than once: %v", err)
	default:
	}
}

func TestAdminStoragePanicFailsClosedAndSignalsFatal(t *testing.T) {
	pair, catalog, latest := serviceFixture(t, time.Now())
	state := NewState(pair, catalog, latest, time.Now())
	fatal := make(chan error, 1)
	server := newAdminServerWithFatal(state, panickingHistoryStore{}, func(err error) {
		fatal <- err
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/history?symbol=BTC%2FUSD&page_size=1", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"code":"storage_unavailable"`) {
		t.Fatalf("history response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-source-body") {
		t.Fatalf("panic value leaked in response: %s", response.Body.String())
	}
	select {
	case err := <-fatal:
		if !strings.Contains(err.Error(), "history storage panicked") ||
			strings.Contains(err.Error(), "secret-source-body") {
			t.Fatalf("fatal error = %v", err)
		}
	default:
		t.Fatal("storage panic did not signal fatal shutdown")
	}
}

type failingHistoryStore struct {
	catalog storage.Catalog
}

func (s failingHistoryStore) History(string, uint32, []byte) (storage.HistoryPage, error) {
	return storage.HistoryPage{}, errors.New("storage frame corrupted")
}

func (s failingHistoryStore) Catalog() storage.Catalog {
	return s.catalog
}

type panickingHistoryStore struct{}

func (panickingHistoryStore) History(string, uint32, []byte) (storage.HistoryPage, error) {
	panic("secret-source-body")
}

func (panickingHistoryStore) Catalog() storage.Catalog {
	return storage.Catalog{}
}

func serviceFixture(t *testing.T, collectedAt time.Time) (*config.Pair, storage.Catalog, map[string]domain.Aggregate) {
	t.Helper()
	feeds, digest, err := domain.CanonicalPlans([]domain.FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   time.Second,
		StaleAfter: 5 * time.Second,
		Sources: []domain.SourcePlan{
			{ID: "a", URL: "https://a.example", JSONPointer: "/v"},
			{ID: "b", URL: "https://b.example", JSONPointer: "/v"},
			{ID: "c", URL: "https://c.example", JSONPointer: "/v"},
		},
	}}, domain.CollectorPolicy{
		MaxConcurrency:        3,
		SourceResponseBytes:   1024,
		MaxRedirects:          1,
		MaxAttempts:           1,
		RequestTimeout:        time.Second,
		ConnectTimeout:        time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		RetryInitialBackoff:   time.Millisecond,
		RetryMaxBackoff:       time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := &config.Pair{
		Config: config.File{
			PublicationRevision: "revision",
			SourcesSHA256:       strings.Repeat("a", 64),
		},
		Feeds:      feeds,
		PlanDigest: digest,
	}
	catalog := storage.Catalog{
		ActivationGeneration: 1,
		PlanDigest:           digest,
		Feeds: []storage.CatalogFeed{{
			Symbol:      feeds[0].Symbol,
			Fingerprint: feeds[0].Fingerprint,
		}},
	}
	record := domain.Aggregate{
		Symbol:               "BTC/USD",
		Value:                "10.000000000000000000",
		Sequence:             1,
		ActivationGeneration: 1,
		CycleStartedAt:       collectedAt.Add(-time.Millisecond),
		CollectedAt:          collectedAt,
		ConfiguredSources:    3,
		SuccessfulSources:    3,
		ContributorIDs:       []string{"a", "b", "c"},
		FeedPlanFingerprint:  feeds[0].Fingerprint,
	}
	return pair, catalog, map[string]domain.Aggregate{"BTC/USD": record}
}
