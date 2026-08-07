//go:build e2e || soak
// +build e2e soak

package pulsarcompat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const envOracleRestart = "GURU_E2E_ORACLE_RESTART"

// TestE2ESingleValidatorOracleRestartReproduction preserves the validator home
// across an active-oracle node and sidecar restart. Keep this focused
// reproduction separate from the longer restart matrix so it can be run while
// diagnosing a failed matrix case.
func TestE2ESingleValidatorOracleRestartReproduction(t *testing.T) {
	if os.Getenv(envOracleRestart) != "1" {
		t.Skipf("set %s=1 to run the single-validator oracle restart reproduction", envOracleRestart)
	}

	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)
	oracledBin := buildOracledBinary(t, repoRoot)
	sourceServer := startOracleSoakSourceServer(t)
	defer sourceServer.Close()

	node, accounts := bootstrapOracleTxSmokeNetwork(t, repoRoot, bin, t.TempDir())
	patchOracleSoakGenesis(t, node.home, accounts.moderator)
	setOracleMinValidators(t, node.home, 1)
	setOracleNodeAppConfig(t, node.home, node.oracleSocket, node.apiPort)
	setOracleNodeCometConfig(t, node.home)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", node.home)

	sidecar := startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
	defer func() { stopOracleProcess(t, sidecar, syscall.SIGTERM) }()
	node.node = startOracleRestartNode(t, repoRoot, bin, node, nil)
	defer func() { stopNode(t, node.node) }()

	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, 20, 90*time.Second)
	latestBeforeRestart := waitForOracleLatestHeight(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcAddr,
		"BTC/USD",
		3,
		90*time.Second,
	)
	heightBeforeRestart := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})

	stopNode(t, node.node)
	assertOracleProcessRunning(t, sidecar)
	node.node = startOracleRestartNode(t, repoRoot, bin, node, nil)

	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, heightBeforeRestart+20, 90*time.Second)
	latestAfterNodeRestart := waitForOracleLatestHeight(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcAddr,
		"BTC/USD",
		latestBeforeRestart.BlockHeight+1,
		90*time.Second,
	)

	if logs := oracleSoakNodeLogs(node); strings.Contains(logs, "failed to verify validator") {
		t.Fatalf("honest restart produced a vote-extension signature validation failure:\n%s", logs)
	}

	heightBeforeSidecarRestart := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
	stopOracleProcess(t, sidecar, syscall.SIGTERM)
	sidecar = startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, heightBeforeSidecarRestart+20, 90*time.Second)
	waitForOracleLatestHeight(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcAddr,
		"BTC/USD",
		latestAfterNodeRestart.BlockHeight+1,
		90*time.Second,
	)

	heightBeforeForcedRestart := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
	stopNode(t, node.node)
	node.node = startOracleRestartDebugNode(t, repoRoot, bin, node)
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, heightBeforeForcedRestart+3, 90*time.Second)
	logOffset := len(oracleSoakNodeLogs(node))
	waitForOraclePrecommitLog(t, node, logOffset, 30*time.Second)
	killNodeProcess(t, node.node)
	stopOracleProcess(t, sidecar, syscall.SIGKILL)

	sidecar = startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
	node.node = startOracleRestartNode(t, repoRoot, bin, node, nil)
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, heightBeforeForcedRestart+23, 90*time.Second)

	if logs := oracleSoakNodeLogs(node); strings.Contains(logs, "failed to verify validator") {
		t.Fatalf("forced precommit restart produced a vote-extension signature validation failure:\n%s", logs)
	}
}

func startOracleRestartDebugNode(t *testing.T, repoRoot, bin string, node *oracleSoakNode) *runningNode {
	t.Helper()
	return startOracleRestartNode(
		t,
		repoRoot,
		bin,
		node,
		[]string{"--log_level", "consensus:debug,*:error"},
	)
}

func startOracleRestartNode(
	t *testing.T,
	repoRoot, bin string,
	node *oracleSoakNode,
	extraArgs []string,
) *runningNode {
	t.Helper()
	// Snapshot shutdown is validated separately. Store v2 currently starts
	// snapshot creation asynchronously, which can race application DB closure
	// under phase-targeted SIGTERM and obscure the Oracle restart result.
	args := []string{"--state-sync.snapshot-interval", "0"}
	args = append(args, extraArgs...)

	return startNodeWithChainIDOption(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcPort,
		node.p2pPort,
		node.pprofPort,
		node.grpcPort,
		node.jsonRPCPort,
		node.jsonWSRPCPort,
		false,
		args,
		nil,
	)
}

type runningOracleProcess struct {
	cmd  *exec.Cmd
	done chan error
	logs *synchronizedBuffer
}

func buildOracledBinary(t *testing.T, repoRoot string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "oracled")
	cmd := exec.Command(
		"go",
		"-C", "oracle",
		"build",
		"-mod=readonly",
		"-o", bin,
		"./cmd/oracled",
	)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build standalone oracled: %v\n%s", err, output)
	}

	return bin
}

func startOracleProcess(
	t *testing.T,
	repoRoot, bin string,
	node *oracleSoakNode,
	sourceServer *oracleTestHTTPSServer,
) *runningOracleProcess {
	t.Helper()

	initializeOracleProcessHome(t, repoRoot, bin, node, sourceServer.URL)
	logs := &synchronizedBuffer{}
	cmd := exec.Command(bin, "--home", node.oracleHome, "start")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+sourceServer.certFile)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start oracled: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	process := &runningOracleProcess{cmd: cmd, done: done, logs: logs}
	waitForOracleProcessReady(t, process, node.oracleSocket, node.oracleAdminSocket, 5*time.Second)

	return process
}

func initializeOracleProcessHome(
	t *testing.T,
	repoRoot, bin string,
	node *oracleSoakNode,
	sourceURL string,
) {
	t.Helper()

	configPath := filepath.Join(node.oracleHome, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect oracled config: %v", err)
	}

	runCmd(t, repoRoot, bin, "--home", node.oracleHome, "init")
	sources := renderOracleSourcesConfig(sourceURL)
	digest := sha256.Sum256(sources)
	config := fmt.Sprintf(`schema_version = 1
publication_revision = "e2e-v1"
sources_sha256 = %q

[server]
consumer_socket = "run/oracle.sock"
admin_socket = "run/admin.sock"
max_request_bytes = 65536
max_response_bytes = 1048576

[collector]
max_concurrency = 32
source_response_bytes = 1048576
max_redirects = 3
max_attempts = 3
request_timeout = "2s"
connect_timeout = "1s"
tls_handshake_timeout = "1s"
response_header_timeout = "1s"
retry_initial_backoff = "10ms"
retry_max_backoff = "100ms"

[storage]
database = "data/oracle.db"
marker = "data/storage.meta"
lock = "run/oracled.lock"
history_retention = 30

[runtime]
shutdown_timeout = "5s"

[logging]
level = "info"
format = "text"
`, hex.EncodeToString(digest[:]))
	if err := os.WriteFile(filepath.Join(node.oracleHome, "sources.toml"), sources, 0o600); err != nil {
		t.Fatalf("write oracled sources: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write oracled config: %v", err)
	}
	runCmd(t, repoRoot, bin, "--home", node.oracleHome, "validate")
}

func waitForOracleProcessReady(
	t *testing.T,
	process *runningOracleProcess,
	consumerSocket, adminSocket string,
	timeout time.Duration,
) {
	t.Helper()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", adminSocket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-process.done:
			t.Fatalf("oracled exited before services were ready: %v\nlogs:\n%s", err, process.logs.String())
		default:
		}
		consumerInfo, consumerErr := os.Stat(consumerSocket)
		adminInfo, adminErr := os.Stat(adminSocket)
		if consumerErr == nil && consumerInfo.Mode()&os.ModeSocket != 0 &&
			adminErr == nil && adminInfo.Mode()&os.ModeSocket != 0 {
			response, err := client.Get("http://unix/v1/status")
			if err == nil {
				_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
				closeErr := response.Body.Close()
				if response.StatusCode == http.StatusOK && readErr == nil && closeErr == nil {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf(
		"oracled consumer/admin services were not ready within %s; logs:\n%s",
		timeout,
		process.logs.String(),
	)
}

func assertOracleProcessRunning(t *testing.T, process *runningOracleProcess) {
	t.Helper()

	select {
	case err := <-process.done:
		t.Fatalf("oracled exited unexpectedly: %v\nlogs:\n%s", err, process.logs.String())
	default:
	}
}

func stopOracleProcess(t *testing.T, process *runningOracleProcess, signal syscall.Signal) {
	t.Helper()
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}

	if err := process.cmd.Process.Signal(signal); err != nil {
		t.Fatalf("signal oracled with %s: %v", signal, err)
	}
	select {
	case err := <-process.done:
		if signal != syscall.SIGKILL && err != nil {
			t.Fatalf("oracled did not stop cleanly after %s: %v\nlogs:\n%s", signal, err, process.logs.String())
		}
	case <-time.After(10 * time.Second):
		_ = process.cmd.Process.Kill()
		t.Fatalf("oracled did not stop after %s; logs:\n%s", signal, process.logs.String())
	}
	process.cmd = nil
}

func waitForOraclePrecommitLog(t *testing.T, node *oracleSoakNode, offset int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs := oracleSoakNodeLogs(node)
		if offset < len(logs) {
			logs = logs[offset:]
		}
		if strings.Contains(logs, "signed and pushed vote") && strings.Contains(logs, "(Precommit)") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for a signed precommit log; logs:\n%s", oracleSoakNodeLogs(node))
}

func killNodeProcess(t *testing.T, node *runningNode) {
	t.Helper()
	if node == nil || node.cmd == nil || node.cmd.Process == nil {
		return
	}

	if err := node.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill node: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- node.cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("killed node process did not exit")
	}
	node.cmd = nil
}

func setOracleMinValidators(t *testing.T, home string, minValidators uint32) {
	t.Helper()

	genesisPath := filepath.Join(home, "config", "genesis.json")
	bz, err := os.ReadFile(genesisPath)
	if err != nil {
		t.Fatalf("read genesis: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(bz, &doc); err != nil {
		t.Fatalf("unmarshal genesis: %v", err)
	}
	appState := mustJSONMap(t, doc, "app_state")
	oracleState := mustJSONMap(t, appState, "oracle")
	params := mustJSONMap(t, oracleState, "params")
	params["min_validators"] = minValidators

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}
	if err := os.WriteFile(genesisPath, out, 0o644); err != nil {
		t.Fatalf("write genesis: %v", err)
	}
}
