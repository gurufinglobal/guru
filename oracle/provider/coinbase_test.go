package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCoinbaseProvider_Fetch_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/BTC-USD/spot") {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"amount":"123.45"}}`))
	}))
	defer srv.Close()

	p := NewCoinbaseProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL + "/"
	got, err := p.Fetch(context.Background(), "BTC/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "123.45" {
		t.Fatalf("expected 123.45, got %q", got)
	}
}

func TestCoinbaseProvider_Fetch_BadStatusIncludesBodySnippet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := NewCoinbaseProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL + "/"
	_, err := p.Fetch(context.Background(), "BTC/USD")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected error to include body snippet, got %v", err)
	}
}

func TestCoinbaseProvider_Fetch_InvalidAmount(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"amount":"not-a-number"}}`))
	}))
	defer srv.Close()

	p := NewCoinbaseProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL + "/"
	_, err := p.Fetch(context.Background(), "BTC/USD")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCoinbaseProvider_Fetch_EmptySymbol(t *testing.T) {
	t.Parallel()
	p := NewCoinbaseProvider(&http.Client{Timeout: 2 * time.Second})
	_, err := p.Fetch(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error")
	}
}
