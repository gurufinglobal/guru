package oracle

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNodeTaskPreflightServesWhileDegradedThenStartsPollers(t *testing.T) {
	var sourceRequests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sourceRequests.Add(1)
		_, _ = w.Write([]byte(`{"price":"10.5"}`))
	}))
	defer httpServer.Close()

	config := Config{
		Socket:           shortTestSocketPath(t),
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         "127.0.0.1:1",
		NodeQueryTimeout: "20ms",
		Sources: []SourceConfig{{
			Name:         "btc",
			Symbol:       "BTC/USD",
			URL:          httpServer.URL,
			ResponsePath: "price",
			Interval:     "20ms",
		}},
	}
	sidecar, err := NewSidecar(config)
	require.NoError(t, err)

	var attempts atomic.Int32
	var nodeReady atomic.Bool
	preflight := &nodeTaskPreflight{
		config:  config,
		sidecar: sidecar,
		ensure: func(context.Context, Config) ([]*oraclev1.OracleTask, error) {
			attempts.Add(1)
			if !nodeReady.Load() {
				return nil, errors.New("node unavailable")
			}

			return testActiveTasks(), nil
		},
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     10 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Start(ctx, sidecar, preflight)
	}()
	waitForSocket(t, config.Socket)

	client, closeClient := newSidecarTestClient(t, config.Socket)
	defer closeClient()
	response, err := client.GetSamples(context.Background(), &oraclev1.GetSamplesRequest{
		Tasks:  testActiveTasks(),
		Height: 10,
	})
	require.NoError(t, err)
	require.Len(t, response.GetSymbols(), 1)
	require.Equal(t, int32(1), sourceRequests.Load())

	nodeReady.Store(true)
	require.Eventually(t, func() bool {
		sidecar.tasksMu.Lock()
		defer sidecar.tasksMu.Unlock()

		return sidecar.tasksConfigured
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		return sourceRequests.Load() >= 2
	}, time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
	require.ErrorContains(t, sidecar.ConfigureActiveTasks(testActiveTasks()), "already configured")

	cancel()
	require.NoError(t, <-done)
}

func TestNodeTaskPreflightUsesBoundedBackoffAndCancels(t *testing.T) {
	sidecar, err := NewSidecar(Config{Socket: filepath.Join(t.TempDir(), "oracle.sock")})
	require.NoError(t, err)

	var attempts atomic.Int32
	preflight := &nodeTaskPreflight{
		config:  sidecar.config,
		sidecar: sidecar,
		ensure: func(context.Context, Config) ([]*oraclev1.OracleTask, error) {
			attempts.Add(1)
			return nil, errors.New("node unavailable")
		},
		initialBackoff: 20 * time.Millisecond,
		maxBackoff:     40 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	require.NoError(t, preflight.Run(ctx))
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
	require.LessOrEqual(t, attempts.Load(), int32(7))
}

func TestStartCancelsNodeTaskPreflightWhenSocketBindFails(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "not-a-socket")
	require.NoError(t, os.WriteFile(socketPath, []byte("occupied"), 0o600))

	sidecar, err := NewSidecar(Config{Socket: socketPath})
	require.NoError(t, err)

	var attempts atomic.Int32
	preflight := &nodeTaskPreflight{
		config:  sidecar.config,
		sidecar: sidecar,
		ensure: func(ctx context.Context, _ Config) ([]*oraclev1.OracleTask, error) {
			attempts.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     10 * time.Millisecond,
	}

	err = Start(context.Background(), sidecar, preflight)
	require.ErrorContains(t, err, "refusing to remove non-socket file")
	require.Equal(t, int32(1), attempts.Load())
}

func TestSidecarRemainsAvailableAfterNodeQueryServerStops(t *testing.T) {
	queryAddress, stopQuery := startOracleQueryServer(t, &oraclev1.Params{
		MinValidators: 1,
		MinSources:    1,
		HistoryLimit:  100,
	}, testActiveTasks())
	defer func() {
		if stopQuery != nil {
			stopQuery()
		}
	}()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"price":"10.5"}`))
	}))
	defer httpServer.Close()

	config := Config{
		Socket:           shortTestSocketPath(t),
		RequestTimeout:   "1s",
		SourceTimeout:    "1s",
		NodeGRPC:         queryAddress,
		NodeQueryTimeout: "1s",
		Sources: []SourceConfig{{
			Name:         "btc",
			Symbol:       "BTC/USD",
			URL:          httpServer.URL,
			ResponsePath: "price",
		}},
	}
	sidecar, err := NewSidecar(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Start(ctx, sidecar, NewNodeTaskPreflight(config, sidecar))
	}()
	waitForSocket(t, config.Socket)
	require.Eventually(t, func() bool {
		sidecar.tasksMu.Lock()
		defer sidecar.tasksMu.Unlock()

		return sidecar.tasksConfigured
	}, time.Second, 10*time.Millisecond)

	stopQuery()
	stopQuery = nil

	client, closeClient := newSidecarTestClient(t, config.Socket)
	defer closeClient()
	response, err := client.GetSamples(context.Background(), &oraclev1.GetSamplesRequest{
		Tasks:  testActiveTasks(),
		Height: 11,
	})
	require.NoError(t, err)
	require.Len(t, response.GetSymbols(), 1)
	require.Equal(t, "10.5", response.GetSymbols()[0].GetSamples()[0].GetValue())

	cancel()
	require.NoError(t, <-done)
}

func TestNodeTaskPreflightLogsDegradedAndReadyTransitions(t *testing.T) {
	sidecar, err := NewSidecar(Config{Socket: filepath.Join(t.TempDir(), "oracle.sock")})
	require.NoError(t, err)

	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousOutput) })

	var attempts atomic.Int32
	preflight := &nodeTaskPreflight{
		config:  sidecar.config,
		sidecar: sidecar,
		ensure: func(context.Context, Config) ([]*oraclev1.OracleTask, error) {
			if attempts.Add(1) == 1 {
				return nil, errors.New("node unavailable")
			}

			return testActiveTasks(), nil
		},
		initialBackoff: time.Millisecond,
		maxBackoff:     time.Millisecond,
	}

	require.NoError(t, preflight.Run(context.Background()))
	require.Contains(t, output.String(), "node_preflight=degraded")
	require.Contains(t, output.String(), "retry_in=1ms")
	require.Contains(t, output.String(), "node_preflight=ready")
}

func testActiveTasks() []*oraclev1.OracleTask {
	return []*oraclev1.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}}
}

func newSidecarTestClient(t *testing.T, socketPath string) (oraclev1.OracleSidecarClient, func()) {
	t.Helper()

	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	return oraclev1.NewOracleSidecarClient(conn), func() {
		require.NoError(t, conn.Close())
	}
}

func shortTestSocketPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join("/tmp", "guru-oracle-lifecycle-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() { _ = os.Remove(path) })

	return path
}
