//go:build e2e
// +build e2e

package pulsarcompat

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type runtimeFixtureBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *runtimeFixtureBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(data)
}

func (b *runtimeFixtureBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

// TestInternalGogoClientCallsPublicPulsarSidecarProcess proves cross-runtime
// wire compatibility. Production node and sidecar code both use internal gogo.
func TestInternalGogoClientCallsPublicPulsarSidecarProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the production sidecar protocol uses Unix domain sockets")
	}

	repoRoot := projectRootFromTestFile(t)
	binDir := t.TempDir()
	serverBin := filepath.Join(binDir, "pulsar-sidecar-server")
	clientBin := filepath.Join(binDir, "gogo-node-client")
	buildRuntimeFixture(t, repoRoot, serverBin, "./tests/pulsarcompat/testdata/pulsar_sidecar_server")
	buildRuntimeFixture(t, repoRoot, clientBin, "./tests/pulsarcompat/testdata/gogo_node_client")

	socketDir, err := os.MkdirTemp(".", "guru-oracle-wire-")
	if err != nil {
		t.Fatalf("create isolated sidecar socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "wire.sock")
	socket, err = filepath.Abs(socket)
	if err != nil {
		t.Fatalf("resolve absolute sidecar socket path: %v", err)
	}
	defer func() { _ = os.Remove(socket) }()

	var serverLogs runtimeFixtureBuffer
	server := exec.Command(serverBin, socket)
	server.Stdout = &serverLogs
	server.Stderr = &serverLogs
	if err := server.Start(); err != nil {
		t.Fatalf("start isolated Pulsar sidecar server: %v", err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Wait() }()
	serverStopped := false
	defer func() {
		if serverStopped {
			return
		}
		_ = server.Process.Kill()
		<-serverDone
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-serverDone:
			serverStopped = true
			t.Fatalf("isolated Pulsar server exited before readiness: %v\n%s", err, serverLogs.String())
		default:
		}
		if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("isolated Pulsar server socket was not ready\n%s", serverLogs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := exec.Command(clientBin, socket)
	output, err := client.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated internal gogo client failed: %v\n%s", err, output)
	}

	if err := server.Process.Kill(); err != nil {
		t.Fatalf("stop isolated Pulsar server: %v", err)
	}
	<-serverDone
	serverStopped = true
}

func buildRuntimeFixture(t *testing.T, repoRoot, output, pkg string) {
	t.Helper()
	command := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-o", output, pkg)
	command.Dir = repoRoot
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build runtime fixture %s: %v\n%s", pkg, err, combined)
	}
}
