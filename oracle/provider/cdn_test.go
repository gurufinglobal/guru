package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCDNProvider_Fetch_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/v1/currencies/krw.json" {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"date":"2024-01-01",
			"krw":{"usd":0.107439747239410490763297,"jpy":0.11}
		}`))
	}))
	defer srv.Close()

	p := NewCDNProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL + "/latest/v1/currencies"

	got, err := p.Fetch(context.Background(), "KRW/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "0.10743974723941049" {
		t.Fatalf("expected 0.10743974723941049, got %q", got)
	}
}

func TestCDNProvider_Fetch_StringValue(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"date":"2024-01-01",
			"krw":{"usd":"123.4500"}
		}`))
	}))
	defer srv.Close()

	p := NewCDNProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL

	got, err := p.Fetch(context.Background(), "KRW/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "123.45" {
		t.Fatalf("expected 123.45, got %q", got)
	}
}

func TestCDNProvider_Fetch_MissingPair(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"date":"2024-01-01",
			"krw":{"jpy":0.11}
		}`))
	}))
	defer srv.Close()

	p := NewCDNProvider(&http.Client{Timeout: 2 * time.Second})
	p.baseURL = srv.URL

	_, err := p.Fetch(context.Background(), "KRW/USD")
	if err == nil {
		t.Fatalf("expected error")
	}
}
