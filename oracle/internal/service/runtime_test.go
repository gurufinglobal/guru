package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

func TestRunEmptyHomeReadinessAndGracefulShutdown(t *testing.T) {
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(tempRoot, "or-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	home := filepath.Join(base, "home")
	paths, err := config.WriteInitialFiles(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Initialize(paths.Database, paths.Marker); err != nil {
		t.Fatal(err)
	}
	pair, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(pair.Paths.Lock)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- RunLocked(ctx, pair, lock, &output, nil) }()
	waitForUnixListener(t, pair.Paths.ConsumerSocket)
	waitForUnixListener(t, pair.Paths.AdminSocket)

	secondLock, err := storage.AcquireHomeLock(pair.Paths.Lock)
	if err == nil || !errors.Is(err, storage.ErrHomeLocked) {
		if secondLock != nil {
			_ = secondLock.Close()
		}
		t.Fatalf("second lock error = %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not terminate after cancellation")
	}
	for _, path := range []string{pair.Paths.ConsumerSocket, pair.Paths.AdminSocket} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("socket %s remains after shutdown: %v", path, err)
		}
	}
	stillHeldLock, err := storage.AcquireHomeLock(pair.Paths.Lock)
	if err == nil || !errors.Is(err, storage.ErrHomeLocked) {
		if stillHeldLock != nil {
			_ = stillHeldLock.Close()
		}
		t.Fatalf("RunLocked released its caller-owned home lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("close caller-owned home lock: %v", err)
	}
	releasedLock, err := storage.AcquireHomeLock(pair.Paths.Lock)
	if err != nil {
		t.Fatalf("home lock was not released: %v", err)
	}
	if err := releasedLock.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("event=ready")) {
		t.Fatalf("readiness diagnostic missing: %q", output.String())
	}
}

func TestRunLockedRequiresOwnershipCapability(t *testing.T) {
	if err := RunLocked(context.Background(), &config.Pair{}, nil, io.Discard, nil); err == nil ||
		!strings.Contains(err.Error(), "home lock") {
		t.Fatalf("RunLocked error = %v", err)
	}
}

func TestEffectiveCollectorConcurrencyBoundsInFlightResponseBodies(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured uint32
		bodyBytes  uint32
		want       uint32
	}{
		{name: "default product", configured: 32, bodyBytes: 1 << 20, want: 32},
		{name: "maximum body", configured: 256, bodyBytes: 16 << 20, want: 2},
		{name: "configured lower", configured: 1, bodyBytes: 16 << 20, want: 1},
		{name: "configured ceiling", configured: 256, bodyBytes: 1, want: 256},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := effectiveCollectorConcurrency(domain.CollectorPolicy{
				MaxConcurrency:      test.configured,
				SourceResponseBytes: test.bodyBytes,
			})
			if got != test.want {
				t.Fatalf("effective concurrency = %d, want %d", got, test.want)
			}
			if uint64(got)*uint64(test.bodyBytes) > maxInFlightSourceBodyBytes &&
				test.bodyBytes <= maxInFlightSourceBodyBytes {
				t.Fatalf("in-flight body product = %d", uint64(got)*uint64(test.bodyBytes))
			}
		})
	}
}

func TestTrackedAdminHandlerWaitHonorsContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	requestDone := make(chan struct{})
	handler := newTrackedAdminHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	go func() {
		defer close(requestDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	<-started
	handler.Stop()

	waitContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := handler.Wait(waitContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want deadline exceeded", err)
	}

	close(release)
	drainContext, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := handler.Wait(drainContext); err != nil {
		t.Fatalf("Wait after release: %v", err)
	}
	<-requestDone
}

func TestTrackedAdminHandlerPrefersCompletedDrain(t *testing.T) {
	handler := newTrackedAdminHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := handler.Wait(ctx); err != nil {
		t.Fatalf("Wait on drained handler: %v", err)
	}
}

func TestStorageRunErrorPreservesClassificationAndCause(t *testing.T) {
	cause := errors.New("write failed")
	err := storageRunError("insert aggregate", cause)
	if !errors.Is(err, ErrStorageUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("storage error chain = %v", err)
	}
}

func waitForUnixListener(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("unix", path, 50*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Unix listener %s did not become ready", path)
}
