package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestSourceClientHTTPStatusRetryPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		status       int
		wantAttempts int32
	}{
		{name: "request timeout", status: http.StatusRequestTimeout, wantAttempts: 2},
		{name: "rate limit", status: http.StatusTooManyRequests, wantAttempts: 2},
		{name: "server error", status: http.StatusInternalServerError, wantAttempts: 2},
		{name: "upper server error", status: 599, wantAttempts: 2},
		{name: "bad request", status: http.StatusBadRequest, wantAttempts: 1},
		{name: "not found", status: http.StatusNotFound, wantAttempts: 1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(writer, "rejected", test.status)
			}))
			defer server.Close()
			policy := testCollectorPolicy()
			policy.MaxAttempts = 2
			client := testSourceClient(t, server, policy)
			defer client.CloseIdleConnections()

			if _, err := client.Fetch(context.Background(), testSource(server.URL, "/v")); err == nil {
				t.Fatal("status rejection unexpectedly succeeded")
			}
			if requests.Load() != test.wantAttempts {
				t.Fatalf("requests = %d, want %d", requests.Load(), test.wantAttempts)
			}
		})
	}
}

func TestSourceClientRejectsTerminalResponseContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		pointer  string
		limit    uint32
		wantText string
	}{
		{
			name:     "oversized body",
			body:     `{"v":123456789}`,
			pointer:  "/v",
			limit:    8,
			wantText: "response body exceeds limit",
		},
		{
			name:     "invalid JSON",
			body:     `{"v":`,
			pointer:  "/v",
			limit:    128,
			wantText: "invalid JSON response",
		},
		{
			name:     "invalid UTF-8",
			body:     string([]byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'}),
			pointer:  "/v",
			limit:    128,
			wantText: "response body is not valid UTF-8",
		},
		{
			name:     "unresolved pointer",
			body:     `{"v":1}`,
			pointer:  "/missing",
			limit:    128,
			wantText: "JSON Pointer did not resolve",
		},
		{
			name:     "invalid numeric value",
			body:     `{"v":"NaN"}`,
			pointer:  "/v",
			limit:    128,
			wantText: "numeric value rejected",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			policy := testCollectorPolicy()
			policy.MaxAttempts = 2
			policy.SourceResponseBytes = test.limit
			client := testSourceClient(t, server, policy)
			defer client.CloseIdleConnections()

			_, err := client.Fetch(context.Background(), testSource(server.URL, test.pointer))
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Fetch error = %v, want %q", err, test.wantText)
			}
			if requests.Load() != 1 {
				t.Fatalf("terminal response triggered %d attempts", requests.Load())
			}
		})
	}
}

func TestSourceClientRejectsHTTPSDowngradeRedirect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Redirect(writer, request, "http://127.0.0.1/downgrade", http.StatusFound)
	}))
	defer server.Close()
	policy := testCollectorPolicy()
	policy.MaxAttempts = 2
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()

	_, err := client.Fetch(context.Background(), testSource(server.URL, "/v"))
	if err == nil || !strings.Contains(err.Error(), "redirect URL rejected") {
		t.Fatalf("Fetch error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("downgrade redirect triggered %d attempts", requests.Load())
	}
}

func TestSourceClientRejectsOversizedRedirectURL(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Location", "https://example.com/"+strings.Repeat("x", domain.MaxURLBytes))
		writer.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	policy := testCollectorPolicy()
	policy.MaxAttempts = 2
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()

	_, err := client.Fetch(context.Background(), testSource(server.URL, "/v"))
	if err == nil || !strings.Contains(err.Error(), "redirect URL rejected") {
		t.Fatalf("Fetch error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("oversized redirect triggered %d attempts", requests.Load())
	}
}

func TestSourceClientParentCancellationIsTerminal(t *testing.T) {
	t.Parallel()
	var (
		requests atomic.Int32
		once     sync.Once
	)
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		once.Do(func() { close(started) })
		<-request.Context().Done()
	}))
	defer server.Close()
	policy := testCollectorPolicy()
	policy.MaxAttempts = 3
	client := testSourceClient(t, server, policy)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Fetch(ctx, testSource(server.URL, "/v"))
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("source request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled source request unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled source request did not return")
	}
	if requests.Load() != 1 {
		t.Fatalf("parent cancellation triggered %d attempts", requests.Load())
	}
}

func testSource(url, pointer string) domain.SourcePlan {
	return domain.SourcePlan{ID: "a", URL: url, JSONPointer: pointer}
}
