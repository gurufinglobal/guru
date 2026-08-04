package collector

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestExtractJSONNumericText(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		input   string
		pointer string
		want    string
		wantErr error
	}{
		{
			name:    "escaped object keys and value",
			input:   `{"a\/b":{"\u0076":"1e-18"}}`,
			pointer: "/a~1b/v",
			want:    "1e-18",
		},
		{
			name:    "array",
			input:   `{"values":[0,-2.5,3]}`,
			pointer: "/values/1",
			want:    "-2.5",
		},
		{
			name:    "last duplicate object member",
			input:   `{"v":"1","v":"2"}`,
			pointer: "/v",
			want:    "2",
		},
		{
			name:    "unresolved",
			input:   `{"v":1}`,
			pointer: "/missing",
			wantErr: errJSONPointerUnresolved,
		},
		{
			name:    "non numeric",
			input:   `{"v":true}`,
			pointer: "/v",
			wantErr: errJSONPointerNotNumeric,
		},
		{
			name:    "invalid surrogate",
			input:   `{"v":"\uD800"}`,
			pointer: "/v",
			wantErr: errInvalidSourceJSON,
		},
		{
			name:    "trailing JSON",
			input:   `{"v":1} {}`,
			pointer: "/v",
			wantErr: errInvalidSourceJSON,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := extractJSONNumericText(context.Background(), []byte(test.input), test.pointer)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractJSONNumericTextCancelsDuringParsing(t *testing.T) {
	t.Parallel()
	ctx := newCancelAfterDoneChecks(3)
	input := []byte(strings.Repeat(" ", 20<<10) + "1")
	if _, err := extractJSONNumericText(ctx, input, ""); err != context.Canceled {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestSourceJSONParserCancelsAcrossManySmallValues(t *testing.T) {
	t.Parallel()
	ctx := newCancelAfterDoneChecks(3)
	input := []byte("[" + strings.Repeat("0,", 10<<10) + "0]")
	parser := sourceJSONParser{ctx: ctx, input: input}
	if _, err := parser.parseValue(true, 0, 0); err != context.Canceled {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestSourceClientExactNumberAndRetry(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(writer, "temporary", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"price":1e-18}}`))
	}))
	defer server.Close()
	client := testSourceClient(t, server, testCollectorPolicy())
	defer client.CloseIdleConnections()
	value, err := client.Fetch(context.Background(), domain.SourcePlan{
		ID:          "a",
		URL:         server.URL,
		JSONPointer: "/data/price",
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "0.000000000000000001" {
		t.Fatalf("value = %s", value)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestSourceClientRetriesPerAttemptTimeout(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			<-request.Context().Done()
			return
		}
		_, _ = writer.Write([]byte(`{"v":2}`))
	}))
	defer server.Close()

	policy := testCollectorPolicy()
	policy.RequestTimeout = 30 * time.Millisecond
	policy.MaxAttempts = 2
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	value, err := client.Fetch(ctx, domain.SourcePlan{
		ID:          "a",
		URL:         server.URL,
		JSONPointer: "/v",
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "2.000000000000000000" || requests.Load() != 2 {
		t.Fatalf("value=%s requests=%d", value, requests.Load())
	}
}

func TestSourceClientDoesNotRetryParentDeadline(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		<-request.Context().Done()
	}))
	defer server.Close()

	policy := testCollectorPolicy()
	policy.RequestTimeout = time.Second
	policy.MaxAttempts = 3
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Fetch(ctx, domain.SourcePlan{
		ID:          "a",
		URL:         server.URL,
		JSONPointer: "/v",
	}); err == nil {
		t.Fatal("parent deadline unexpectedly succeeded")
	}
	if requests.Load() != 1 {
		t.Fatalf("parent deadline triggered %d attempts", requests.Load())
	}
}

func TestRunCycleWaitsForDeadlineAndIncludesAllSuccesses(t *testing.T) {
	t.Parallel()
	t.Run("three successes and one hang", func(t *testing.T) {
		handler := func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/hang" {
				<-request.Context().Done()
				return
			}
			values := map[string]string{"/a": "1", "/b": "2", "/c": "3"}
			_, _ = writer.Write([]byte(`{"v":` + values[request.URL.Path] + `}`))
		}
		server := httptest.NewTLSServer(http.HandlerFunc(handler))
		defer server.Close()
		runCycleCase(t, server, []string{"/a", "/b", "/c", "/hang"}, 140*time.Millisecond, "2.000000000000000000", true)
	})
	t.Run("late fourth success changes median", func(t *testing.T) {
		handler := func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/late" {
				time.Sleep(40 * time.Millisecond)
			}
			values := map[string]string{"/a": "1", "/late": "2", "/c": "100", "/d": "101"}
			_, _ = writer.Write([]byte(`{"v":` + values[request.URL.Path] + `}`))
		}
		server := httptest.NewTLSServer(http.HandlerFunc(handler))
		defer server.Close()
		runCycleCase(t, server, []string{"/a", "/late", "/c", "/d"}, 200*time.Millisecond, "51.000000000000000000", false)
	})
	t.Run("two successes and two hangs", func(t *testing.T) {
		handler := func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/h1" || request.URL.Path == "/h2" {
				<-request.Context().Done()
				return
			}
			_, _ = writer.Write([]byte(`{"v":1}`))
		}
		server := httptest.NewTLSServer(http.HandlerFunc(handler))
		defer server.Close()
		runCycleCase(t, server, []string{"/a", "/b", "/h1", "/h2"}, 100*time.Millisecond, "", true)
	})
}

func TestRunCycleExcludesSourceCompletingAfterDeadline(t *testing.T) {
	t.Parallel()

	policy := testCollectorPolicy()
	policy.MaxAttempts = 1
	policy.RequestTimeout = time.Second
	client, err := NewSourceClient(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := io.NopCloser(bytes.NewBufferString(`{"v":1}`))
		if strings.HasPrefix(request.URL.Path, "/late-") {
			body = &deadlineBody{
				context: request.Context(),
				payload: []byte(`{"v":2}`),
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       body,
			Request:    request,
		}, nil
	})

	sources := []domain.SourcePlan{
		{ID: "early", URL: "https://source.test/early", JSONPointer: "/v"},
		{ID: "late-a", URL: "https://source.test/late-a", JSONPointer: "/v"},
		{ID: "late-b", URL: "https://source.test/late-b", JSONPointer: "/v"},
	}
	feeds, _, err := domain.CanonicalPlans([]domain.FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   40 * time.Millisecond,
		StaleAfter: time.Second,
		Sources:    sources,
	}}, policy)
	if err != nil {
		t.Fatal(err)
	}
	observer := &testObserver{}
	var committed *domain.Aggregate
	engine := NewEngine(feeds, 1, 3, client, func(candidate domain.Aggregate) (domain.Aggregate, error) {
		candidate.Sequence = 1
		committed = &candidate
		return candidate, nil
	}, observer)

	nominal := time.Now()
	if err := engine.runCycle(context.Background(), feeds[0], nominal); err != nil {
		t.Fatal(err)
	}
	if committed != nil {
		t.Fatalf("post-deadline sources produced aggregate %#v", committed)
	}
	if observer.outcome != domain.CycleUnderQuorum {
		t.Fatalf("outcome = %s, want %s", observer.outcome, domain.CycleUnderQuorum)
	}
}

func TestRunCycleDoesNotStartSourceQueuedPastDeadline(t *testing.T) {
	t.Parallel()

	policy := testCollectorPolicy()
	policy.MaxAttempts = 1
	policy.RequestTimeout = 2 * time.Second
	client, err := NewSourceClient(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()

	holdStarted := make(chan struct{})
	holdRelease := make(chan struct{})
	var (
		holdStartedOnce sync.Once
		holdReleaseOnce sync.Once
		queuedRequests  atomic.Int32
	)
	releaseHold := func() { holdReleaseOnce.Do(func() { close(holdRelease) }) }
	t.Cleanup(releaseHold)
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/hold":
			holdStartedOnce.Do(func() { close(holdStarted) })
			select {
			case <-holdRelease:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		case "/queued":
			queuedRequests.Add(1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"v":1}`)),
			Request:    request,
		}, nil
	})

	observer := &testObserver{}
	engine := NewEngine(nil, 1, 1, client, func(candidate domain.Aggregate) (domain.Aggregate, error) {
		candidate.Sequence = 1
		return candidate, nil
	}, observer)
	holdFeed := domain.FeedPlan{
		Symbol:     "HOLD",
		Interval:   time.Second,
		StaleAfter: time.Second,
		Sources: []domain.SourcePlan{{
			ID:          "hold",
			URL:         "https://source.test/hold",
			JSONPointer: "/v",
		}},
	}
	holdResult := make(chan error, 1)
	go func() {
		holdResult <- engine.runCycle(context.Background(), holdFeed, time.Now())
	}()
	select {
	case <-holdStarted:
	case <-time.After(time.Second):
		t.Fatal("permit-holding source did not start")
	}

	queuedFeed := domain.FeedPlan{
		Symbol:     "QUEUED",
		Interval:   40 * time.Millisecond,
		StaleAfter: time.Second,
		Sources: []domain.SourcePlan{{
			ID:          "queued",
			URL:         "https://source.test/queued",
			JSONPointer: "/v",
		}},
	}
	if err := engine.runCycle(context.Background(), queuedFeed, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := queuedRequests.Load(); got != 0 {
		t.Fatalf("queued source started %d requests after its cycle deadline", got)
	}

	releaseHold()
	select {
	case err := <-holdResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("permit-holding cycle did not finish")
	}
}

func TestNextNominalSkipsLargeBacklogInConstantWork(t *testing.T) {
	t.Parallel()
	interval := time.Millisecond
	nominal := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := nominal.Add(20*365*24*time.Hour + 500*time.Microsecond)
	got := nextNominal(nominal, now, interval)
	want := now.Add(500 * time.Microsecond)
	if !got.Equal(want) {
		t.Fatalf("next nominal = %s, want %s", got, want)
	}
}

func runCycleCase(t *testing.T, server *httptest.Server, paths []string, interval time.Duration, want string, expectDeadline bool) {
	t.Helper()
	policy := testCollectorPolicy()
	policy.MaxAttempts = 1
	policy.RequestTimeout = time.Second
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()
	sources := make([]domain.SourcePlan, len(paths))
	for i, path := range paths {
		sources[i] = domain.SourcePlan{
			ID:          string(rune('a' + i)),
			URL:         server.URL + path,
			JSONPointer: "/v",
		}
	}
	feeds, _, err := domain.CanonicalPlans([]domain.FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   interval,
		StaleAfter: time.Second,
		Sources:    sources,
	}}, policy)
	if err != nil {
		t.Fatal(err)
	}
	observer := &testObserver{}
	var committed *domain.Aggregate
	engine := NewEngine(feeds, 1, 4, client, func(candidate domain.Aggregate) (domain.Aggregate, error) {
		candidate.Sequence = 1
		copy := candidate
		committed = &copy
		return candidate, nil
	}, observer)
	started := time.Now()
	if err := engine.runCycle(context.Background(), feeds[0], started); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if expectDeadline && elapsed < interval-20*time.Millisecond {
		t.Fatalf("cycle completed at first quorum: elapsed %s interval %s", elapsed, interval)
	}
	if want == "" {
		if committed != nil {
			t.Fatalf("under-quorum cycle committed %#v", committed)
		}
		if observer.outcome != domain.CycleUnderQuorum {
			t.Fatalf("outcome = %s", observer.outcome)
		}
		return
	}
	if committed == nil || committed.Value != want {
		t.Fatalf("committed = %#v, want value %s", committed, want)
	}
	if committed.CollectedAt == committed.CollectedAt.Round(0) { //nolint:staticcheck // Equality detects monotonic metadata removed by Round.
		t.Fatal("committed completion time lost its monotonic clock reading")
	}
	if len(committed.ContributorIDs) != len(paths) && !expectDeadline {
		t.Fatalf("contributors = %v", committed.ContributorIDs)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type deadlineBody struct {
	context context.Context
	payload []byte
	read    bool
}

func (b *deadlineBody) Read(target []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	<-b.context.Done()
	b.read = true
	return copy(target, b.payload), nil
}

func (*deadlineBody) Close() error { return nil }

type cancelAfterDoneChecksContext struct {
	after int32
	calls atomic.Int32
	done  chan struct{}
	once  sync.Once
}

func newCancelAfterDoneChecks(after int32) *cancelAfterDoneChecksContext {
	return &cancelAfterDoneChecksContext{after: after, done: make(chan struct{})}
}

func (*cancelAfterDoneChecksContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *cancelAfterDoneChecksContext) Done() <-chan struct{} {
	if c.calls.Add(1) >= c.after {
		c.once.Do(func() { close(c.done) })
	}
	return c.done
}

func (c *cancelAfterDoneChecksContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (*cancelAfterDoneChecksContext) Value(any) any { return nil }

type testObserver struct {
	mu      sync.Mutex
	outcome domain.CycleOutcome
}

func (*testObserver) CycleStarted(string) {}

func (o *testObserver) CycleFinished(_ string, outcome domain.CycleOutcome, _ uint32, _ time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outcome = outcome
}

func (o *testObserver) AggregateCommitted(_ domain.Aggregate, outcome domain.CycleOutcome) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outcome = outcome
}

func testSourceClient(t *testing.T, server *httptest.Server, policy domain.CollectorPolicy) *SourceClient {
	t.Helper()
	client, err := NewSourceClient(policy)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport := client.client.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = pool
	return client
}

func testCollectorPolicy() domain.CollectorPolicy {
	return domain.CollectorPolicy{
		MaxConcurrency:        4,
		SourceResponseBytes:   1024,
		MaxRedirects:          1,
		MaxAttempts:           2,
		RequestTimeout:        time.Second,
		ConnectTimeout:        time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		RetryInitialBackoff:   time.Millisecond,
		RetryMaxBackoff:       time.Millisecond,
	}
}
