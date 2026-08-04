package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInitValidateAndMachineFailureContract(t *testing.T) {
	t.Parallel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stdout.String() != fmt.Sprintf("Initialized oracled home at %s\n", home) || stderr.Len() != 0 {
		t.Fatalf("init output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "validate"}, &stdout, &stderr); code != 0 {
		t.Fatalf("validate exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "status", "--format", "json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("machine failure wrote stdout: %q", stdout.String())
	}
	var envelope service.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("machine error is invalid JSON: %v: %q", err, stderr.String())
	}
	if envelope.SchemaVersion != 1 || envelope.Command != "status" || envelope.Error.Code != "daemon_unavailable" {
		t.Fatalf("machine error = %#v", envelope)
	}
}

func TestReconcileValidatesNodeBeforeEndpointQueries(t *testing.T) {
	t.Parallel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--home", home, "reconcile", "--format", "json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("reconcile exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"invalid_arguments"`) || stdout.Len() != 0 {
		t.Fatalf("unexpected reconcile failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestReconcileCommandExitAndReadOnlyContracts(t *testing.T) {
	tests := []struct {
		name          string
		tasks         []*oraclev1.OracleTask
		statusFeeds   []service.FeedStatus
		paramsErr     error
		wantExit      int
		wantFinding   string
		wantBlocking  bool
		wantErrorCode string
		wantNodeCalls []string
	}{
		{
			name:          "ready",
			wantExit:      0,
			wantNodeCalls: []string{"Params", "ActiveTasks:"},
		},
		{
			name: "default feeds ready",
			tasks: []*oraclev1.OracleTask{
				numericTask("BTC/USD"),
				numericTask("ETH/USD"),
				numericTask("SOL/USD"),
			},
			statusFeeds: []service.FeedStatus{
				freshFeed("BTC/USD", 4, 3, domain.CycleQuorum),
				freshFeed("ETH/USD", 4, 3, domain.CycleQuorum),
				freshFeed("SOL/USD", 4, 3, domain.CycleQuorum),
			},
			wantExit:      0,
			wantNodeCalls: []string{"Params", "ActiveTasks:"},
		},
		{
			name: "informational local-only symbol",
			statusFeeds: []service.FeedStatus{
				freshFeed("ETH/USD", 3, 3, domain.CycleFull),
			},
			wantExit:      0,
			wantFinding:   "inactive_symbol",
			wantNodeCalls: []string{"Params", "ActiveTasks:"},
		},
		{
			name:          "authoritative action required",
			tasks:         []*oraclev1.OracleTask{numericTask("BTC/USD")},
			wantExit:      1,
			wantFinding:   "missing_symbol",
			wantBlocking:  true,
			wantNodeCalls: []string{"Params", "ActiveTasks:"},
		},
		{
			name:          "node failure",
			paramsErr:     status.Error(codes.Unavailable, "node unavailable"),
			wantExit:      2,
			wantErrorCode: "node_unavailable",
			wantNodeCalls: []string{"Params"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(shortAdminDirectory(t), "home")
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 0 {
				t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			pair, err := config.Load(home)
			if err != nil {
				t.Fatal(err)
			}
			before := snapshotReconcileFiles(t, pair.Paths)
			admin := startReconcileAdminServer(t, pair.Paths.AdminSocket, reconcileStatus(
				pair.Config.PublicationRevision,
				pair.Config.SourcesSHA256,
				test.statusFeeds...,
			))
			queryServer := &reconcileQueryServer{
				paramsErr: test.paramsErr,
				active: func(*oraclev1.QueryActiveTasksRequest) (*oraclev1.QueryActiveTasksResponse, error) {
					return &oraclev1.QueryActiveTasksResponse{Tasks: test.tasks}, nil
				},
			}
			nodeEndpoint := startReconcileQueryServer(t, queryServer)

			stdout.Reset()
			stderr.Reset()
			code := Run([]string{
				"--home", home,
				"reconcile",
				"--node-grpc", nodeEndpoint,
				"--format", "json",
			}, &stdout, &stderr)
			if code != test.wantExit {
				t.Fatalf("reconcile exit=%d stdout=%q stderr=%q, want %d", code, stdout.String(), stderr.String(), test.wantExit)
			}
			if after := snapshotReconcileFiles(t, pair.Paths); !reflect.DeepEqual(after, before) {
				t.Fatal("reconcile modified configuration or storage files")
			}
			if calls := admin.Calls(); !reflect.DeepEqual(calls, []string{"GET /v1/status"}) {
				t.Fatalf("admin calls = %q", calls)
			}
			if calls := queryServer.Calls(); !reflect.DeepEqual(calls, test.wantNodeCalls) {
				t.Fatalf("node calls = %q, want %q", calls, test.wantNodeCalls)
			}

			if test.wantErrorCode != "" {
				if stdout.Len() != 0 {
					t.Fatalf("failed reconcile wrote stdout: %q", stdout.String())
				}
				var envelope service.ErrorEnvelope
				if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
					t.Fatalf("error output is invalid JSON: %v: %q", err, stderr.String())
				}
				if envelope.Command != "reconcile" || envelope.Error.Code != test.wantErrorCode {
					t.Fatalf("error envelope = %#v", envelope)
				}
				return
			}
			if stderr.Len() != 0 {
				t.Fatalf("authoritative reconcile wrote stderr: %q", stderr.String())
			}
			var envelope service.SuccessEnvelope[ReconcileData]
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("success output is invalid JSON: %v: %q", err, stdout.String())
			}
			if envelope.SchemaVersion != 1 || envelope.Command != "reconcile" {
				t.Fatalf("success envelope = %#v", envelope)
			}
			if test.wantFinding == "" {
				if len(envelope.Data.Findings) != 0 {
					t.Fatalf("findings = %#v", envelope.Data.Findings)
				}
				return
			}
			if len(envelope.Data.Findings) != 1 ||
				envelope.Data.Findings[0].Code != test.wantFinding ||
				envelope.Data.Findings[0].Blocking != test.wantBlocking {
				t.Fatalf("findings = %#v", envelope.Data.Findings)
			}
		})
	}
}

type reconcileAdminRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *reconcileAdminRecorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *reconcileAdminRecorder) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func startReconcileAdminServer(
	t *testing.T,
	socket string,
	data service.StatusData,
) *reconcileAdminRecorder {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &reconcileAdminRecorder{}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorder.record(request.Method + " " + request.URL.RequestURI())
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(writer).Encode(service.SuccessEnvelope[service.StatusData]{
			SchemaVersion: 1,
			Command:       "status",
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Data:          data,
		}); err != nil {
			t.Errorf("encode admin status: %v", err)
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
			t.Error("test admin server did not stop")
		}
	})
	return recorder
}

func snapshotReconcileFiles(t *testing.T, paths config.Paths) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, 4)
	for _, path := range []string{paths.ConfigFile, paths.SourcesFile, paths.Database, paths.Marker} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[path] = string(content)
	}
	return snapshot
}

func TestFrozenCommandSurfaceDisablesCompletion(t *testing.T) {
	t.Parallel()
	exec := &execution{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, home: "/nonexistent", format: "text"}
	root := exec.rootCommand()
	for _, command := range root.Commands() {
		if command.Name() == "completion" {
			t.Fatal("completion command is exposed")
		}
	}
}

func TestInitRefusesInsecureExistingHomeWithoutChangingPermissions(t *testing.T) {
	t.Parallel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "shared")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--home", home, "init"}, &stdout, &stderr); code != 1 {
		t.Fatalf("init exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	info, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing home permissions changed to %o", info.Mode().Perm())
	}
	if _, err := os.Lstat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("init wrote into rejected home: %v", err)
	}
}

func TestCommandErrorsPreserveTypedCauses(t *testing.T) {
	t.Parallel()
	locked := fmt.Errorf("acquire: %w", storage.ErrHomeLocked)
	if err := fail(1, "home_locked", locked); !errors.Is(err, storage.ErrHomeLocked) {
		t.Fatalf("command error does not unwrap home lock: %v", err)
	}
	if code := startFailureCode(locked); code != "home_locked" {
		t.Fatalf("home lock code = %q", code)
	}
	if code := homeLockFailureCode(locked); code != "home_locked" {
		t.Fatalf("acquire contention code = %q", code)
	}
	if code := homeLockFailureCode(errors.New("permission denied")); code != "storage_error" {
		t.Fatalf("acquire failure code = %q", code)
	}
	storageFailure := fmt.Errorf("runtime: %w", service.ErrStorageUnavailable)
	if code := startFailureCode(storageFailure); code != "storage_error" {
		t.Fatalf("storage code = %q", code)
	}
	if code := startFailureCode(errors.New("other")); code != "internal" {
		t.Fatalf("fallback code = %q", code)
	}
}
