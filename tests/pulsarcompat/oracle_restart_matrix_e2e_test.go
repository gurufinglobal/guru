//go:build e2e || soak
// +build e2e soak

package pulsarcompat

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	envOracleRestartMatrix = "GURU_E2E_ORACLE_RESTART_MATRIX"
	envOracleRestartLedger = "GURU_E2E_ORACLE_RESTART_LEDGER"
	envOracleRestartCase   = "GURU_E2E_ORACLE_RESTART_CASE"
)

type oracleRestartPhase struct {
	name   string
	marker string
}

type oracleRestartCase struct {
	phase        oracleRestartPhase
	signal       syscall.Signal
	due          bool
	startupOrder string
}

type oracleRestartLedgerRow struct {
	caseNumber        int
	phase             string
	signal            string
	due               bool
	startupOrder      string
	startHeight       int64
	interruptHeight   int64
	targetHeight      int64
	finalHeight       int64
	oracleBefore      int64
	oracleAfter       int64
	validationErrors  int
	privValidatorStep string
}

func TestE2ESingleValidatorOracleRestartMatrix(t *testing.T) {
	if os.Getenv(envOracleRestartMatrix) != "1" {
		t.Skipf("set %s=1 to run the 100-case single-validator oracle restart matrix", envOracleRestartMatrix)
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
	node.node = startOracleRestartDebugNode(t, repoRoot, bin, node)
	defer func() { stopNode(t, node.node) }()
	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, 20, 90*time.Second)

	latest := waitForOracleLatestHeight(
		t,
		repoRoot,
		bin,
		node.home,
		node.rpcAddr,
		"BTC/USD",
		3,
		90*time.Second,
	)
	initialHeight := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
	cases := oracleRestartCases()
	if len(cases) != 100 {
		t.Fatalf("restart matrix must contain exactly 100 cases, got %d", len(cases))
	}
	caseOffset := 0
	if selected := strings.TrimSpace(os.Getenv(envOracleRestartCase)); selected != "" {
		caseNumber, err := strconv.Atoi(selected)
		if err != nil || caseNumber < 1 || caseNumber > len(cases) {
			t.Fatalf("%s must be an integer from 1 through %d", envOracleRestartCase, len(cases))
		}
		caseOffset = caseNumber - 1
		cases = cases[caseOffset : caseOffset+1]
	}

	ledgerPath := os.Getenv(envOracleRestartLedger)
	ledger := make([]oracleRestartLedgerRow, 0, len(cases))
	for i, testCase := range cases {
		startHeight := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
		logOffset := len(oracleSoakNodeLogs(node))
		interruptHeight := waitForOracleConsensusPhase(
			t,
			node,
			logOffset,
			testCase.phase,
			testCase.due,
			30*time.Second,
		)

		stopNodeWithSignal(t, node.node, testCase.signal)
		validationErrors := oracleValidationErrorCount(oracleSoakNodeLogs(node))
		if validationErrors != 0 {
			t.Fatalf(
				"case %d produced %d vote-extension validation errors before restart; logs:\n%s",
				caseOffset+i+1,
				validationErrors,
				oracleSoakNodeLogs(node),
			)
		}
		stopOracleProcess(t, sidecar, testCase.signal)

		switch testCase.startupOrder {
		case "sidecar-first":
			sidecar = startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
			node.node = startOracleRestartDebugNode(t, repoRoot, bin, node)
		case "node-first":
			node.node = startOracleRestartDebugNode(t, repoRoot, bin, node)
			sidecar = startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
		default:
			t.Fatalf("unsupported startup order %q", testCase.startupOrder)
		}

		targetHeight := interruptHeight + 20
		waitForOracleSoakNodeHeight(t, repoRoot, bin, node, targetHeight, 90*time.Second)
		latestAfter := waitForOracleLatestHeight(
			t,
			repoRoot,
			bin,
			node.home,
			node.rpcAddr,
			"BTC/USD",
			latest.BlockHeight+10,
			90*time.Second,
		)
		finalHeight := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
		if finalHeight < targetHeight {
			t.Fatalf("case %d advanced to %d, expected at least %d", caseOffset+i+1, finalHeight, targetHeight)
		}
		privValidatorStep := assertPrivValidatorStateAtOrAbove(t, node.home, finalHeight)
		validationErrors = oracleValidationErrorCount(oracleSoakNodeLogs(node))
		if validationErrors != 0 {
			t.Fatalf(
				"case %d produced %d vote-extension validation errors after restart; logs:\n%s",
				caseOffset+i+1,
				validationErrors,
				oracleSoakNodeLogs(node),
			)
		}

		row := oracleRestartLedgerRow{
			caseNumber:        caseOffset + i + 1,
			phase:             testCase.phase.name,
			signal:            oracleSignalName(testCase.signal),
			due:               testCase.due,
			startupOrder:      testCase.startupOrder,
			startHeight:       startHeight,
			interruptHeight:   interruptHeight,
			targetHeight:      targetHeight,
			finalHeight:       finalHeight,
			oracleBefore:      latest.BlockHeight,
			oracleAfter:       latestAfter.BlockHeight,
			validationErrors:  validationErrors,
			privValidatorStep: privValidatorStep,
		}
		ledger = append(ledger, row)
		writeOracleRestartLedger(t, ledgerPath, ledger)
		t.Logf(
			"restart case=%d phase=%s signal=%s due=%t order=%s height=%d->%d oracle=%d->%d",
			row.caseNumber,
			row.phase,
			row.signal,
			row.due,
			row.startupOrder,
			row.startHeight,
			row.finalHeight,
			row.oracleBefore,
			row.oracleAfter,
		)
		latest = latestAfter
	}

	finalHeight := latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})
	if caseOffset == 0 && len(cases) == 100 && finalHeight-initialHeight < 2_000 {
		t.Fatalf(
			"restart matrix advanced %d cumulative heights, expected at least 2000 (initial=%d final=%d)",
			finalHeight-initialHeight,
			initialHeight,
			finalHeight,
		)
	}
}

func TestOracleRestartCasesCoverAcceptedMatrix(t *testing.T) {
	cases := oracleRestartCases()
	if len(cases) != 100 {
		t.Fatalf("expected 100 cases, got %d", len(cases))
	}

	baseCounts := map[string]int{}
	for _, testCase := range cases[:96] {
		key := fmt.Sprintf(
			"%s/%s/%t/%s",
			testCase.phase.name,
			oracleSignalName(testCase.signal),
			testCase.due,
			testCase.startupOrder,
		)
		baseCounts[key]++
	}
	if len(baseCounts) != 48 {
		t.Fatalf("expected all 48 base combinations, got %d", len(baseCounts))
	}
	for key, count := range baseCounts {
		if count != 2 {
			t.Fatalf("base case %s occurred %d times, expected 2", key, count)
		}
	}
}

func TestOracleConsensusLogHeight(t *testing.T) {
	for _, testCase := range []struct {
		line   string
		height int64
	}{
		{line: "entering precommit step height=5289 module=consensus", height: 5289},
		{line: `{"level":"debug","height":5290,"message":"committed block"}`, height: 5290},
	} {
		height, ok := oracleConsensusLogHeight(testCase.line)
		if !ok || height != testCase.height {
			t.Fatalf("parse height from %q: height=%d ok=%t", testCase.line, height, ok)
		}
	}
}

func TestOracleProposalHeightIsDue(t *testing.T) {
	for _, height := range []int64{4, 5, 9, 12} {
		if !oracleProposalHeightIsDue(height) {
			t.Fatalf("expected proposal height %d to carry a due vote extension", height)
		}
	}
	for _, height := range []int64{2, 3, 6, 7} {
		if oracleProposalHeightIsDue(height) {
			t.Fatalf("expected proposal height %d not to carry a due vote extension", height)
		}
	}
}

func oracleRestartCases() []oracleRestartCase {
	phases := []oracleRestartPhase{
		{name: "new-round", marker: "entering new round"},
		{name: "propose", marker: "entering propose step"},
		{name: "prevote", marker: "entering prevote step"},
		{name: "precommit", marker: "entering precommit step"},
		{name: "commit", marker: "entering commit step"},
		{name: "committed", marker: "committed block"},
	}
	signals := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
	dueStates := []bool{true, false}
	startupOrders := []string{"sidecar-first", "node-first"}

	base := make([]oracleRestartCase, 0, 48)
	for _, phase := range phases {
		for _, signal := range signals {
			for _, due := range dueStates {
				for _, startupOrder := range startupOrders {
					base = append(base, oracleRestartCase{
						phase:        phase,
						signal:       signal,
						due:          due,
						startupOrder: startupOrder,
					})
				}
			}
		}
	}

	cases := make([]oracleRestartCase, 0, 100)
	cases = append(cases, base...)
	cases = append(cases, base...)
	cases = append(cases,
		oracleRestartCase{phase: phases[3], signal: syscall.SIGKILL, due: true, startupOrder: "sidecar-first"},
		oracleRestartCase{phase: phases[4], signal: syscall.SIGKILL, due: false, startupOrder: "node-first"},
		oracleRestartCase{phase: phases[5], signal: syscall.SIGTERM, due: true, startupOrder: "node-first"},
		oracleRestartCase{phase: phases[0], signal: syscall.SIGKILL, due: false, startupOrder: "sidecar-first"},
	)

	return cases
}

func waitForOracleConsensusPhase(
	t *testing.T,
	node *oracleSoakNode,
	logOffset int,
	phase oracleRestartPhase,
	due bool,
	timeout time.Duration,
) int64 {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs := oracleSoakNodeLogs(node)
		if logOffset < len(logs) {
			for _, line := range strings.Split(logs[logOffset:], "\n") {
				if !strings.Contains(line, phase.marker) {
					continue
				}
				height, ok := oracleConsensusLogHeight(line)
				if ok && oracleProposalHeightIsDue(height) == due {
					return height
				}
			}
		}
		time.Sleep(time.Millisecond)
	}

	logs := oracleSoakNodeLogs(node)
	if len(logs) > 8_000 {
		logs = logs[len(logs)-8_000:]
	}
	t.Fatalf(
		"timed out waiting for consensus phase=%s due=%t marker=%q; recent logs:\n%s",
		phase.name,
		due,
		phase.marker,
		logs,
	)

	return 0
}

func oracleConsensusLogHeight(line string) (int64, bool) {
	for _, marker := range []string{"height=", `"height":`} {
		index := strings.Index(line, marker)
		if index < 0 {
			continue
		}
		remainder := strings.TrimLeft(line[index+len(marker):], " \"")
		end := 0
		for end < len(remainder) && remainder[end] >= '0' && remainder[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		height, err := strconv.ParseInt(remainder[:end], 10, 64)
		if err == nil {
			return height, true
		}
	}

	return 0, false
}

func oracleProposalHeightIsDue(proposalHeight int64) bool {
	voteExtensionHeight := proposalHeight - 1
	btcOrTRXDue := voteExtensionHeight >= 3 && (voteExtensionHeight-3)%5 == 0
	ethDue := voteExtensionHeight >= 4 && (voteExtensionHeight-4)%7 == 0

	return btcOrTRXDue || ethDue
}

func stopNodeWithSignal(t *testing.T, node *runningNode, signal syscall.Signal) {
	t.Helper()
	if node == nil || node.cmd == nil || node.cmd.Process == nil {
		return
	}

	if err := node.cmd.Process.Signal(signal); err != nil {
		t.Fatalf("signal node with %s: %v", signal, err)
	}
	done := make(chan error, 1)
	go func() {
		done <- node.cmd.Wait()
	}()
	select {
	case err := <-done:
		if signal != syscall.SIGKILL && err != nil {
			t.Fatalf(
				"node did not stop cleanly after %s: %v\nlogs:\n%s",
				signal,
				err,
				node.logBuf.String(),
			)
		}
	case <-time.After(10 * time.Second):
		_ = node.cmd.Process.Kill()
		t.Fatalf("node did not stop after %s", signal)
	}
	node.cmd = nil
}

func oracleValidationErrorCount(logs string) int {
	return strings.Count(logs, "oracle vote extension validation failed") +
		strings.Count(logs, "failed to verify validator")
}

func assertPrivValidatorStateAtOrAbove(t *testing.T, home string, minHeight int64) string {
	t.Helper()

	bz, err := os.ReadFile(filepath.Join(home, "data", "priv_validator_state.json"))
	if err != nil {
		t.Fatalf("read private validator state: %v", err)
	}
	var state struct {
		Height string `json:"height"`
		Round  int32  `json:"round"`
		Step   int8   `json:"step"`
	}
	if err := json.Unmarshal(bz, &state); err != nil {
		t.Fatalf("decode private validator state: %v", err)
	}
	height, err := strconv.ParseInt(state.Height, 10, 64)
	if err != nil {
		t.Fatalf("parse private validator height %q: %v", state.Height, err)
	}
	if height < minHeight {
		t.Fatalf("private validator state regressed to height %d below committed height %d", height, minHeight)
	}

	return fmt.Sprintf("%d/%d/%d", height, state.Round, state.Step)
}

func oracleSignalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return signal.String()
	}
}

func writeOracleRestartLedger(t *testing.T, path string, rows []oracleRestartLedgerRow) {
	t.Helper()
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create restart ledger directory: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create restart ledger: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close restart ledger: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{
		"case",
		"phase",
		"signal",
		"oracle_due",
		"startup_order",
		"start_height",
		"interrupt_height",
		"target_height",
		"final_height",
		"oracle_before",
		"oracle_after",
		"validation_errors",
		"priv_validator_state",
	}); err != nil {
		t.Fatalf("write restart ledger header: %v", err)
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			strconv.Itoa(row.caseNumber),
			row.phase,
			row.signal,
			strconv.FormatBool(row.due),
			row.startupOrder,
			strconv.FormatInt(row.startHeight, 10),
			strconv.FormatInt(row.interruptHeight, 10),
			strconv.FormatInt(row.targetHeight, 10),
			strconv.FormatInt(row.finalHeight, 10),
			strconv.FormatInt(row.oracleBefore, 10),
			strconv.FormatInt(row.oracleAfter, 10),
			strconv.Itoa(row.validationErrors),
			row.privValidatorStep,
		}); err != nil {
			t.Fatalf("write restart ledger row %d: %v", row.caseNumber, err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush restart ledger: %v", err)
	}
}
