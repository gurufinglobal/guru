package oracle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestSidecarServesConfiguredSourcesOverUnixSocket(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte(`{"data":{"price":"10.5"}}`))
		case "/b":
			_, _ = w.Write([]byte(`{"data":{"price":11}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer httpServer.Close()

	socketPath := filepath.Join("/tmp", "guru-oracle-test-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	sidecar, err := NewSidecar(Config{
		Socket:         socketPath,
		RequestTimeout: "2s",
		SourceTimeout:  "1s",
		Sources: []SourceConfig{
			{Name: "a", Symbol: "BTC/USD", URL: httpServer.URL + "/a", ResponsePath: "data.price"},
			{Name: "b", Symbol: "BTC/USD", URL: httpServer.URL + "/b", ResponsePath: "data.price"},
			{Name: "eth", Symbol: "ETH/USD", URL: httpServer.URL + "/a", ResponsePath: "data.price"},
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sidecar.Run(ctx)
	}()
	waitForSocket(t, socketPath)

	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := oraclev1.NewOracleSidecarClient(conn)
	response, err := client.GetSamples(context.Background(), &oraclev1.GetSamplesRequest{
		Tasks: []*oraclev1.OracleTask{
			{Symbol: "BTC/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1},
			{Symbol: "btc/usd", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1},
		},
		Height: 12,
	})
	require.NoError(t, err)
	require.Len(t, response.GetSymbols(), 1)
	require.Equal(t, "BTC/USD", response.GetSymbols()[0].GetSymbol())
	require.Len(t, response.GetSymbols()[0].GetSamples(), 2)
	require.Equal(t, "a", response.GetSymbols()[0].GetSamples()[0].GetSource())
	require.Equal(t, "10.5", response.GetSymbols()[0].GetSamples()[0].GetValue())
	require.Equal(t, "b", response.GetSymbols()[0].GetSamples()[1].GetSource())
	require.Equal(t, "11", response.GetSymbols()[0].GetSamples()[1].GetValue())

	cancel()
	err = <-done
	require.NoError(t, err)
}

func TestSidecarReusesFreshIntervalCache(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{"price":"10.5"}}`))
	}))
	defer httpServer.Close()

	sidecar, err := NewSidecar(Config{
		Socket:         "/tmp/guru-oracle-test.sock",
		RequestTimeout: "2s",
		SourceTimeout:  "1s",
		Sources: []SourceConfig{{
			Name:         "a",
			Symbol:       "BTC/USD",
			URL:          httpServer.URL,
			ResponsePath: "data.price",
			Interval:     "1h",
		}},
	})
	require.NoError(t, err)

	source := sidecar.config.Sources[0]
	task := &oraclev1.OracleTask{Symbol: "BTC/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1}
	first, err := sidecar.sampleForSource(context.Background(), source, task, time.Second)
	require.NoError(t, err)
	second, err := sidecar.sampleForSource(context.Background(), source, task, time.Second)
	require.NoError(t, err)

	require.Equal(t, first.GetValue(), second.GetValue())
	require.Equal(t, int32(1), requests.Load())
}

func TestSidecarCacheReturnsClonedSamples(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{"price":"10.5"}}`))
	}))
	defer httpServer.Close()

	sidecar, err := NewSidecar(Config{
		Socket:         "/tmp/guru-oracle-test.sock",
		RequestTimeout: "2s",
		SourceTimeout:  "1s",
		Sources: []SourceConfig{{
			Name:         "a",
			Symbol:       "BTC/USD",
			URL:          httpServer.URL,
			ResponsePath: "data.price",
			Interval:     "1h",
		}},
	})
	require.NoError(t, err)

	source := sidecar.config.Sources[0]
	task := &oraclev1.OracleTask{Symbol: "BTC/USD", ValueType: oraclev1.ValueType_VALUE_TYPE_NUMERIC, Enabled: true, SubmissionInterval: 1}
	first, err := sidecar.sampleForSource(context.Background(), source, task, time.Second)
	require.NoError(t, err)
	first.Value = "mutated"
	first.Source = "mutated-source"

	second, err := sidecar.sampleForSource(context.Background(), source, task, time.Second)
	require.NoError(t, err)

	require.Equal(t, "10.5", second.GetValue())
	require.Equal(t, "a", second.GetSource())
	require.Equal(t, int32(1), requests.Load())
}

func TestSidecarPollsIntervalSourcesWithoutRequests(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{"price":"10.5"}}`))
	}))
	defer httpServer.Close()

	socketPath := filepath.Join("/tmp", "guru-oracle-test-poll-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	sidecar, err := NewSidecar(Config{
		Socket:         socketPath,
		RequestTimeout: "2s",
		SourceTimeout:  "1s",
		Sources: []SourceConfig{{
			Name:         "a",
			Symbol:       "BTC/USD",
			URL:          httpServer.URL,
			ResponsePath: "data.price",
			Interval:     "20ms",
		}},
	}, []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sidecar.Run(ctx)
	}()
	waitForSocket(t, socketPath)
	require.Eventually(t, func() bool {
		return requests.Load() >= 2
	}, time.Second, 10*time.Millisecond)

	cancel()
	err = <-done
	require.NoError(t, err)
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Stat(path)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for socket %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
