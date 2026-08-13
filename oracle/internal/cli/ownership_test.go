package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

func TestHistoryCommandPageSizeBoundaries(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	pair, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	requests := startHistoryPageRecorder(t, pair.Paths.AdminSocket)

	for _, test := range []struct {
		name     string
		pageSize string
		want     string
	}{
		{name: "default", want: "30"},
		{name: "minimum", pageSize: "1", want: "1"},
		{name: "maximum", pageSize: "50", want: "50"},
	} {
		t.Run("online "+test.name, func(t *testing.T) {
			args := []string{"--home", home, "history", "BTC/USD", "--format", "json"}
			if test.pageSize != "" {
				args = append(args, "--page-size", test.pageSize)
			}
			stdout.Reset()
			stderr.Reset()
			if code := Run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("history exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("history success wrote stderr %q", stderr.String())
			}
			if got := <-requests; got != test.want {
				t.Fatalf("history page_size query = %q, want %q", got, test.want)
			}
		})
	}

	for _, offline := range []bool{false, true} {
		for _, test := range []struct {
			name     string
			pageSize string
		}{
			{name: "zero", pageSize: "0"},
			{name: "above maximum", pageSize: "51"},
		} {
			mode := "online"
			if offline {
				mode = "offline"
			}
			t.Run(mode+" "+test.name, func(t *testing.T) {
				args := []string{"--home", home, "history", "BTC/USD", "--format", "json", "--page-size", test.pageSize}
				if offline {
					args = append(args, "--offline")
				}
				stdout.Reset()
				stderr.Reset()
				if code := Run(args, &stdout, &stderr); code != 1 {
					t.Fatalf("history exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				if stdout.Len() != 0 {
					t.Fatalf("history failure wrote stdout %q", stdout.String())
				}
				var envelope service.ErrorEnvelope
				if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
					t.Fatalf("decode history error: %v: %q", err, stderr.String())
				}
				if envelope.Error.Code != "invalid_arguments" || envelope.Error.Message != "--page-size must be from 1 to 50" {
					t.Fatalf("history page-size error = %#v", envelope.Error)
				}
			})
		}
	}
}

func startHistoryPageRecorder(t *testing.T, socket string) <-chan string {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 3)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.URL.Query().Get("page_size")
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(writer).Encode(service.SuccessEnvelope[service.HistoryData]{
			SchemaVersion: 1,
			Command:       "history",
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Data: service.HistoryData{
				Symbol:            "BTC/USD",
				HighWaterSequence: "0",
				Records:           []service.HistoryRecord{},
			},
		}); err != nil {
			t.Errorf("encode history response: %v", err)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("history admin server did not stop")
		}
	})
	return requests
}

func TestInitRefusesHeldHomeLockBeforePublication(t *testing.T) {
	t.Parallel()
	home := filepath.Join(shortAdminDirectory(t), "home")
	absolute, err := config.PrepareInitialHome(home)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := config.CanonicalHomeLockPath(absolute)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close home lock: %v", err)
		}
	})

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 1 {
		t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "The daemon may be running, starting, or stopping.") ||
		strings.Contains(stderr.String(), storage.ErrHomeLocked.Error()) {
		t.Fatalf("held-lock init output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertInitPublicationAbsent(t, home)
}

func TestInitSequenceRevalidatesUnsafePostLockSwapBeforePublication(t *testing.T) {
	t.Parallel()
	base := shortAdminDirectory(t)
	home := filepath.Join(base, "home")
	absolute, err := config.PrepareInitialHome(home)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := config.CanonicalHomeLockPath(absolute)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close home lock: %v", err)
		}
	})

	data := filepath.Join(home, "data")
	originalData := filepath.Join(home, "data-original")
	if err := os.Rename(data, originalData); err != nil {
		t.Fatal(err)
	}
	swapTarget := filepath.Join(base, "swap-target")
	if err := os.Mkdir(swapTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(swapTarget, data); err != nil {
		t.Fatal(err)
	}

	if _, err := config.WriteInitialFiles(absolute); err == nil || !strings.Contains(err.Error(), "safe directory") {
		t.Fatalf("WriteInitialFiles after unsafe swap error = %v", err)
	}
	assertInitPublicationAbsent(t, home)
	entries, err := os.ReadDir(swapTarget)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsafe swap target received files: %v", entries)
	}
}

func assertInitPublicationAbsent(t *testing.T, home string) {
	t.Helper()
	for _, relative := range []string{
		"config.toml",
		"sources.toml",
		"data/oracle.db",
		"data/storage.meta",
		"run/oracle.sock",
		"run/admin.sock",
	} {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected init publication %q: %v", relative, err)
		}
	}
}
