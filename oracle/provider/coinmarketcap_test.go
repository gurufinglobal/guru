package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCoinMarketCapProvider_Fetch_Success(t *testing.T) {
	t.Parallel()

	const apiKey = "cmc-test-key"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/tools/price-conversion" {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-CMC_PRO_API_KEY") != apiKey {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("amount") != "1" {
			http.Error(w, "bad amount", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("symbol") != "BTC" || r.URL.Query().Get("convert") != "USD" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":{"error_code":0,"error_message":null},
			"data":{"BTC":[{"quote":{"USD":{"price":0.107439747239410490763297}}}]}
		}`))
	}))
	defer srv.Close()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, apiKey)
	p.baseURL = srv.URL + "/v2/tools/price-conversion"

	got, err := p.Fetch(context.Background(), "BTC/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "0.10743974723941049" {
		t.Fatalf("expected 0.10743974723941049, got %q", got)
	}
}

func TestCoinMarketCapProvider_Fetch_APIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":{"error_code":1002,"error_message":"invalid API key"},
			"data":{}
		}`))
	}))
	defer srv.Close()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, "bad-key")
	p.baseURL = srv.URL

	_, err := p.Fetch(context.Background(), "BTC/USD")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "cmc api error") {
		t.Fatalf("expected cmc api error, got %v", err)
	}
}

func TestCoinMarketCapProvider_Fetch_InvalidSymbol(t *testing.T) {
	t.Parallel()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, "test")
	_, err := p.Fetch(context.Background(), "BTCUSD")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCoinMarketCapProvider_Fetch_ArrayResponse_FiatPair(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":{"error_code":0,"error_message":null},
			"data":[
				{
					"symbol":"USD",
					"quote":{"PHP":{"price":59.70000200}}
				}
			]
		}`))
	}))
	defer srv.Close()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, "test-key")
	p.baseURL = srv.URL

	got, err := p.Fetch(context.Background(), "USD/PHP")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "59.700002" {
		t.Fatalf("expected 59.700002, got %q", got)
	}
}

func TestCoinMarketCapProvider_Fetch_ArrayResponse_SelectMostRecentCandidate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":{"error_code":0,"error_message":null},
			"data":[
				{
					"symbol":"BTC",
					"quote":{"USD":{"price":71569.78649716,"last_updated":"2026-03-13T07:45:05.000Z"}}
				},
				{
					"symbol":"BTC",
					"quote":{"USD":{"price":70000.0,"last_updated":"2026-03-13T07:46:05.000Z"}}
				},
				{
					"symbol":"NOTBTC",
					"quote":{"USD":{"price":999999.0,"last_updated":"2026-03-13T07:50:05.000Z"}}
				}
			]
		}`))
	}))
	defer srv.Close()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, "test-key")
	p.baseURL = srv.URL

	got, err := p.Fetch(context.Background(), "BTC/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "70000" {
		t.Fatalf("expected 70000, got %q", got)
	}
}

func TestCoinMarketCapProvider_Fetch_ArrayResponse_SelectMostRecentByConversionTimestamp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":{"error_code":0,"error_message":null},
			"data":[
				{
					"symbol":"BTC",
					"last_updated":"2026-03-13T07:45:05.000Z",
					"quote":{"USD":{"price":64000.0}}
				},
				{
					"symbol":"BTC",
					"last_updated":"2026-03-13T07:46:05.000Z",
					"quote":{"USD":{"price":63000.0}}
				}
			]
		}`))
	}))
	defer srv.Close()

	p := NewCoinMarketCapProvider(&http.Client{Timeout: 2 * time.Second}, "test-key")
	p.baseURL = srv.URL

	got, err := p.Fetch(context.Background(), "BTC/USD")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got != "63000" {
		t.Fatalf("expected 63000, got %q", got)
	}
}
