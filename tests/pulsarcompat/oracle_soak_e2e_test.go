//go:build e2e || soak
// +build e2e soak

package pulsarcompat

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	envOracleSoak           = "GURU_E2E_SOAK"
	envOracleTxSmoke        = "GURU_E2E_ORACLE_TX_SMOKE"
	oracleSoakBlocks        = int64(1_000)
	oracleSoakWorkloadStep  = int64(5)
	oracleSoakImportBlocks  = int64(20)
	oracleSoakTimeout       = 25 * time.Minute
	oracleSoakTxFee         = highFeeAGXN
	oracleSoakValidatorFund = "1000000000000000000000agxn"
	oracleSoakOperatorFund  = "3000000000000000000000agxn"
	oracleSoakInitialMinGas = "630000000000.000000000000000000"
	// 630000000000 min gas price * 250000 gas. It is valid at genesis but
	// must be rejected after the oracle raises the chain-wide minimum.
	oracleSoakGenesisFeeAt250KGas = "157500000000000000agxn"
)

func TestE2EOracleSyncTxSmoke(t *testing.T) {
	if os.Getenv(envOracleTxSmoke) != "1" {
		t.Skipf("set %s=1 to run the oracle sync tx smoke", envOracleTxSmoke)
	}

	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)
	node, accounts := bootstrapOracleTxSmokeNetwork(t, repoRoot, bin, t.TempDir())
	node.node = startNode(
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
	)
	defer stopNode(t, node.node)

	waitForOracleSoakNodeHeight(t, repoRoot, bin, node, 3, 90*time.Second)
	beforeSeq := queryAccountSequence(t, repoRoot, bin, node.home, node.rpcAddr, accounts.moderator)
	txHash := submitOracleSyncTxWithDiagnostics(
		t, repoRoot, bin, node, beforeSeq,
		"tx", "oracle", "upsert-task", "SMOKE/TX", "11",
		"--enabled=false",
		"--from", "moderator",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", oracleSoakTxFee,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	afterSeq := waitForAccountSequenceAtLeast(t, repoRoot, bin, node.home, node.rpcAddr, accounts.moderator, beforeSeq+1, 30*time.Second)
	task := queryOracleTaskSnapshot(t, repoRoot, bin, node.home, node.rpcAddr, "smoke/tx")
	if task.symbol != "SMOKE/TX" || task.valueType != "VALUE_TYPE_NUMERIC" || task.enabled || task.submissionInterval != 11 {
		t.Fatalf("unexpected oracle task after tx hash=%s task=%+v", txHash, task)
	}
	t.Logf("oracle sync tx smoke passed tx_hash=%s sequence=%d->%d task=%+v", txHash, beforeSeq, afterSeq, task)
}

func TestE2EFourValidatorOracleSoakRestartExportImport(t *testing.T) {
	if os.Getenv(envOracleSoak) != "1" {
		t.Skipf("set %s=1 to run the 4-validator oracle soak", envOracleSoak)
	}
	previousLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(previousLogOutput)

	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)
	oracledBin := buildOracledBinary(t, repoRoot)
	sourceServer := startOracleSoakSourceServer(t)
	defer sourceServer.Close()

	nodes, accounts := bootstrapOracleSoakNetwork(t, repoRoot, bin, t.TempDir(), sourceServer.URL)
	startOracleSidecars(t, repoRoot, oracledBin, nodes, sourceServer)
	defer stopOracleSidecars(t, nodes)
	startOracleSoakNodes(t, repoRoot, bin, nodes)
	defer stopOracleSoakNodes(t, nodes)

	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, 15, 2*time.Minute)
	waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD", 3, 90*time.Second)
	waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "TRX/USD", 3, 90*time.Second)
	t.Logf("four-validator checkpoint=baseline height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes))
	updatedMinGasPrice := waitForMinGasPriceAbove(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, oracleSoakInitialMinGas, 2*time.Minute)
	t.Logf("oracle-driven min gas price update observed current_min_gas_price=%s", updatedMinGasPrice)
	assertUpdatedMinGasPriceRejectsGenesisFee(t, repoRoot, bin, nodes[0], accounts)

	runOracleSoakTxMix(t, repoRoot, bin, nodes[0], accounts)

	for index := range nodes {
		assertNoOracleVoteExtensionValidationFailures(t, nodes)
		heightBeforeRestart := latestOracleSoakHeight(t, repoRoot, bin, nodes)
		latestBeforeRestart := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
		restartOracleSoakNode(t, repoRoot, bin, nodes, index)
		waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, heightBeforeRestart+5, 2*time.Minute)
		waitForOracleLatestHeight(
			t,
			repoRoot,
			bin,
			nodes[0].home,
			nodes[0].rpcAddr,
			"BTC/USD",
			latestBeforeRestart.BlockHeight+1,
			90*time.Second,
		)
		t.Logf(
			"four-validator checkpoint=node-restart validator=%d height=%d",
			index,
			latestOracleSoakHeight(t, repoRoot, bin, nodes),
		)
	}
	for index := range nodes {
		heightBeforeRestart := latestOracleSoakHeight(t, repoRoot, bin, nodes)
		latestBeforeRestart := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
		stopOracleSidecar(t, nodes[index])
		startOracleSidecar(t, repoRoot, oracledBin, nodes[index], sourceServer)
		waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, heightBeforeRestart+5, 90*time.Second)
		waitForOracleLatestHeight(
			t,
			repoRoot,
			bin,
			nodes[0].home,
			nodes[0].rpcAddr,
			"BTC/USD",
			latestBeforeRestart.BlockHeight+1,
			90*time.Second,
		)
		t.Logf(
			"four-validator checkpoint=sidecar-restart validator=%d height=%d",
			index,
			latestOracleSoakHeight(t, repoRoot, bin, nodes),
		)
	}

	heightBeforeStop := latestOracleSoakHeight(t, repoRoot, bin, nodes[:3])
	stopNode(t, nodes[3].node)
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes[:3], heightBeforeStop+10, 2*time.Minute)
	t.Logf("four-validator checkpoint=one-validator-offline height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes[:3]))

	restartOracleSoakNode(t, repoRoot, bin, nodes, 3)
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, heightBeforeStop+20, 3*time.Minute)
	t.Logf("four-validator checkpoint=one-validator-recovered height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes))

	latestWithAllSidecars := waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD", 3, 90*time.Second)
	stopOracleSidecar(t, nodes[3])
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, latestOracleSoakHeight(t, repoRoot, bin, nodes)+8, 90*time.Second)
	waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD", latestWithAllSidecars.BlockHeight+1, 90*time.Second)
	startOracleSidecar(t, repoRoot, oracledBin, nodes[3], sourceServer)

	stopOracleSidecar(t, nodes[1])
	stopOracleSidecar(t, nodes[2])
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, latestOracleSoakHeight(t, repoRoot, bin, nodes)+4, 60*time.Second)
	latestBeforeOracleQuorumLoss := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, latestOracleSoakHeight(t, repoRoot, bin, nodes)+10, 90*time.Second)
	latestDuringOracleQuorumLoss := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
	if latestDuringOracleQuorumLoss.BlockHeight != latestBeforeOracleQuorumLoss.BlockHeight {
		t.Fatalf("oracle value advanced without min_validators quorum: before=%+v after=%+v", latestBeforeOracleQuorumLoss, latestDuringOracleQuorumLoss)
	}
	t.Logf(
		"four-validator checkpoint=oracle-quorum-lost chain_height=%d oracle_height=%d",
		latestOracleSoakHeight(t, repoRoot, bin, nodes),
		latestDuringOracleQuorumLoss.BlockHeight,
	)
	startOracleSidecar(t, repoRoot, oracledBin, nodes[1], sourceServer)
	startOracleSidecar(t, repoRoot, oracledBin, nodes[2], sourceServer)
	waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD", latestBeforeOracleQuorumLoss.BlockHeight+1, 90*time.Second)
	t.Logf("four-validator checkpoint=oracle-quorum-recovered height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes))

	heightBeforeValidatorQuorumLoss := latestOracleSoakHeight(t, repoRoot, bin, nodes)
	stopNode(t, nodes[2].node)
	stopNode(t, nodes[3].node)
	assertOracleSoakHalted(t, repoRoot, bin, nodes[0], 6*time.Second)
	t.Logf("four-validator checkpoint=consensus-quorum-halted height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes[:2]))
	restartOracleSoakNode(t, repoRoot, bin, nodes, 2)
	restartOracleSoakNode(t, repoRoot, bin, nodes, 3)
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, heightBeforeValidatorQuorumLoss, 2*time.Minute)
	t.Logf("four-validator checkpoint=consensus-quorum-recovered height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes))

	oracleSoakStartHeight := latestOracleSoakHeight(t, repoRoot, bin, nodes)
	oracleSoakTargetHeight := oracleSoakStartHeight + oracleSoakBlocks
	t.Logf(
		"four-validator checkpoint=soak-start height=%d target=%d additional=%d",
		oracleSoakStartHeight,
		oracleSoakTargetHeight,
		oracleSoakBlocks,
	)
	runOracleSoakWorkloadUntilHeight(t, repoRoot, bin, nodes, accounts, oracleSoakTargetHeight)
	assertNoOracleVoteExtensionValidationFailures(t, nodes)
	assertCommonOracleSoakBlock(t, repoRoot, bin, nodes, oracleSoakTargetHeight)
	assertOracleHistory(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
	t.Logf("four-validator checkpoint=soak-complete height=%d", latestOracleSoakHeight(t, repoRoot, bin, nodes))

	exportHeight := latestOracleSoakHeight(t, repoRoot, bin, nodes)
	latestBTCBeforeExport := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
	latestETHBeforeExport := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "ETH/USD")
	stopOracleSoakNodes(t, nodes)
	assertZeroHeightExportRejected(t, repoRoot, bin, nodes[0].home, exportHeight)
	exportedGenesis := exportOracleSoakGenesis(t, repoRoot, bin, nodes[0].home, exportHeight)
	assertExportedOracleState(t, exportedGenesis, exportHeight)
	stopOracleSidecars(t, nodes)

	importedNodes := bootstrapImportedOracleSoakNetwork(t, repoRoot, bin, t.TempDir(), nodes, exportedGenesis)
	startOracleSidecars(t, repoRoot, oracledBin, importedNodes, sourceServer)
	defer stopOracleSidecars(t, importedNodes)
	startOracleSoakNodes(t, repoRoot, bin, importedNodes)
	defer stopOracleSoakNodes(t, importedNodes)

	importTargetHeight := exportHeight + oracleSoakImportBlocks
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, importedNodes, importTargetHeight, 4*time.Minute)
	waitForOracleLatestHeight(t, repoRoot, bin, importedNodes[0].home, importedNodes[0].rpcAddr, "BTC/USD", latestBTCBeforeExport.BlockHeight+1, 2*time.Minute)
	waitForOracleLatestHeight(t, repoRoot, bin, importedNodes[0].home, importedNodes[0].rpcAddr, "ETH/USD", latestETHBeforeExport.BlockHeight+1, 2*time.Minute)
	assertCommonOracleSoakBlock(t, repoRoot, bin, importedNodes, importTargetHeight)
	t.Logf(
		"four-validator checkpoint=export-import export_height=%d imported_height=%d",
		exportHeight,
		latestOracleSoakHeight(t, repoRoot, bin, importedNodes),
	)
}

type oracleSoakNode struct {
	index             int
	home              string
	keyName           string
	rpcPort           int
	p2pPort           int
	pprofPort         int
	grpcPort          int
	jsonRPCPort       int
	jsonWSRPCPort     int
	apiPort           int
	rpcAddr           string
	grpcAddr          string
	nodeID            string
	oracleHome        string
	oracleSocket      string
	oracleAdminSocket string
	node              *runningNode
	sidecar           *runningOracleProcess
}

type oracleSoakAccounts struct {
	moderator string
	user      string
	receiver  string
	poor      string
	valoper0  string
}

type oracleLatestValue struct {
	Symbol      string
	Value       string
	BlockHeight int64
}

type oracleTaskSnapshot struct {
	symbol             string
	valueType          string
	enabled            bool
	submissionInterval int64
}

func bootstrapOracleTxSmokeNetwork(t *testing.T, repoRoot, bin, baseDir string) (*oracleSoakNode, oracleSoakAccounts) {
	t.Helper()

	node := newOracleSoakNode(t, baseDir, 0)
	runInitWithConstitutionAddresses(t, repoRoot, bin, "oracle-tx-smoke", node.home)
	runCmd(t, repoRoot, bin, "keys", "add", node.keyName, "--keyring-backend", "test", "--home", node.home)
	runCmd(t, repoRoot, bin, "keys", "add", "moderator", "--keyring-backend", "test", "--home", node.home)

	accounts := oracleSoakAccounts{
		moderator: keyAddress(t, repoRoot, bin, node.home, "moderator"),
		valoper0:  keyValoperAddress(t, repoRoot, bin, node.home, node.keyName),
	}
	validatorAddr := keyAddress(t, repoRoot, bin, node.home, node.keyName)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", validatorAddr, "100000000000000000000agxn", "--home", node.home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", accounts.moderator, "50000000000000000000agxn", "--home", node.home)
	patchConstitutionModeratorGenesis(t, node.home, accounts.moderator)
	runCmd(t, repoRoot, bin, "genesis", "gentx", node.keyName, "10000000000000000000agxn", "--chain-id", e2eChainID, "--keyring-backend", "test", "--home", node.home, "--fees", highFeeAGXN)
	runCmd(t, repoRoot, bin, "genesis", "collect-gentxs", "--home", node.home)
	patchConstitutionModeratorGenesis(t, node.home, accounts.moderator)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", node.home)

	return node, accounts
}

func bootstrapOracleSoakNetwork(t *testing.T, repoRoot, bin, baseDir, sourceURL string) ([]*oracleSoakNode, oracleSoakAccounts) {
	t.Helper()

	nodes := make([]*oracleSoakNode, 4)
	for i := range nodes {
		nodes[i] = newOracleSoakNode(t, baseDir, i)
		runInitWithConstitutionAddresses(t, repoRoot, bin, fmt.Sprintf("oracle-soak-%d", i), nodes[i].home)
		runCmd(t, repoRoot, bin, "keys", "add", nodes[i].keyName, "--keyring-backend", "test", "--home", nodes[i].home)
	}

	for _, key := range []string{"moderator", "user", "receiver", "poor"} {
		runCmd(t, repoRoot, bin, "keys", "add", key, "--keyring-backend", "test", "--home", nodes[0].home)
	}

	accounts := oracleSoakAccounts{
		moderator: keyAddress(t, repoRoot, bin, nodes[0].home, "moderator"),
		user:      keyAddress(t, repoRoot, bin, nodes[0].home, "user"),
		receiver:  keyAddress(t, repoRoot, bin, nodes[0].home, "receiver"),
		poor:      keyAddress(t, repoRoot, bin, nodes[0].home, "poor"),
		valoper0:  keyValoperAddress(t, repoRoot, bin, nodes[0].home, nodes[0].keyName),
	}

	for _, node := range nodes {
		addr := keyAddress(t, repoRoot, bin, node.home, node.keyName)
		runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", addr, oracleSoakValidatorFund, "--home", nodes[0].home)
	}
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", accounts.moderator, oracleSoakOperatorFund, "--home", nodes[0].home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", accounts.user, oracleSoakOperatorFund, "--home", nodes[0].home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", accounts.receiver, "1000000000000000000agxn", "--home", nodes[0].home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", accounts.poor, "1agxn", "--home", nodes[0].home)

	patchOracleSoakGenesis(t, nodes[0].home, accounts.moderator)
	for _, node := range nodes[1:] {
		copyFile(t, filepath.Join(nodes[0].home, "config", "genesis.json"), filepath.Join(node.home, "config", "genesis.json"))
	}

	for _, node := range nodes {
		runCmd(t, repoRoot, bin, "genesis", "gentx", node.keyName, "10000000000000000000agxn", "--chain-id", e2eChainID, "--keyring-backend", "test", "--home", node.home, "--fees", highFeeAGXN)
		if node.index != 0 {
			copyGentxFiles(t, node.home, nodes[0].home)
		}
	}
	runCmd(t, repoRoot, bin, "genesis", "collect-gentxs", "--home", nodes[0].home)

	patchOracleSoakGenesis(t, nodes[0].home, accounts.moderator)
	for _, node := range nodes {
		if node.index != 0 {
			copyFile(t, filepath.Join(nodes[0].home, "config", "genesis.json"), filepath.Join(node.home, "config", "genesis.json"))
		}
		setOracleNodeAppConfig(t, node.home, node.oracleSocket, node.apiPort)
		setOracleNodeCometConfig(t, node.home)
		node.nodeID = showOracleSoakNodeID(t, repoRoot, bin, node.home)
	}

	return nodes, accounts
}

func patchConstitutionModeratorGenesis(t *testing.T, home, moderatorAddress string) {
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
	constitutionState := mustJSONMap(t, appState, "constitution")
	constitutionState["moderator_address"] = moderatorAddress

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}
	if err := os.WriteFile(genesisPath, out, 0o644); err != nil {
		t.Fatalf("write genesis: %v", err)
	}
}

func bootstrapImportedOracleSoakNetwork(
	t *testing.T,
	repoRoot, bin, baseDir string,
	sourceNodes []*oracleSoakNode,
	exportedGenesis string,
) []*oracleSoakNode {
	t.Helper()

	nodes := make([]*oracleSoakNode, len(sourceNodes))
	for i := range sourceNodes {
		nodes[i] = newOracleSoakNode(t, baseDir, i)
		runInitWithConstitutionAddresses(t, repoRoot, bin, fmt.Sprintf("oracle-import-%d", i), nodes[i].home)
		if err := os.WriteFile(filepath.Join(nodes[i].home, "config", "genesis.json"), []byte(exportedGenesis), 0o644); err != nil {
			t.Fatalf("write imported genesis: %v", err)
		}
		copyFile(t, filepath.Join(sourceNodes[i].home, "config", "priv_validator_key.json"), filepath.Join(nodes[i].home, "config", "priv_validator_key.json"))
		setOracleNodeAppConfig(t, nodes[i].home, nodes[i].oracleSocket, nodes[i].apiPort)
		setOracleNodeCometConfig(t, nodes[i].home)
		nodes[i].nodeID = showOracleSoakNodeID(t, repoRoot, bin, nodes[i].home)
	}
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", nodes[0].home)

	return nodes
}

func newOracleSoakNode(t *testing.T, baseDir string, index int) *oracleSoakNode {
	t.Helper()

	home := filepath.Join(baseDir, fmt.Sprintf("validator-%d", index))
	oracleTemp := os.TempDir()
	if runtime.GOOS == "darwin" {
		oracleTemp = "/private/tmp"
	}
	oracleBase, err := filepath.EvalSymlinks(oracleTemp)
	if err != nil {
		t.Fatalf("resolve temporary directory for oracle home: %v", err)
	}
	oracleHome := filepath.Join(
		oracleBase,
		fmt.Sprintf("gor-e2e-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), index),
	)
	t.Cleanup(func() {
		if err := os.RemoveAll(oracleHome); err != nil {
			t.Errorf("remove oracle home: %v", err)
		}
	})
	rpcPort := pickTCPPort(t)
	grpcPort := pickTCPPort(t)
	return &oracleSoakNode{
		index:             index,
		home:              home,
		keyName:           fmt.Sprintf("validator-%d", index),
		rpcPort:           rpcPort,
		p2pPort:           pickTCPPort(t),
		pprofPort:         pickTCPPort(t),
		grpcPort:          grpcPort,
		jsonRPCPort:       pickTCPPort(t),
		jsonWSRPCPort:     pickTCPPort(t),
		apiPort:           pickTCPPort(t),
		rpcAddr:           fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort),
		grpcAddr:          fmt.Sprintf("127.0.0.1:%d", grpcPort),
		oracleHome:        oracleHome,
		oracleSocket:      filepath.Join(oracleHome, "run", "oracle.sock"),
		oracleAdminSocket: filepath.Join(oracleHome, "run", "admin.sock"),
	}
}

func patchOracleSoakGenesis(t *testing.T, home, moderatorAddress string) {
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

	constitutionState := mustJSONMap(t, appState, "constitution")
	constitutionState["moderator_address"] = moderatorAddress

	oracleState := mustJSONMap(t, appState, "oracle")
	oracleState["params"] = map[string]any{
		"min_validators": 3,
		"min_sources":    3,
		"history_limit":  100,
	}
	oracleState["tasks"] = []any{
		map[string]any{"symbol": "BTC/USD", "value_type": 1, "enabled": true, "submission_interval": 5},
		map[string]any{"symbol": "ETH/USD", "value_type": 1, "enabled": true, "submission_interval": 7},
		map[string]any{"symbol": "TRX/USD", "value_type": 1, "enabled": true, "submission_interval": 5},
	}
	oracleState["task_schedule"] = []any{
		map[string]any{"symbol": "BTC/USD", "height": 3},
		map[string]any{"symbol": "BTC/USD", "height": 8},
		map[string]any{"symbol": "ETH/USD", "height": 4},
		map[string]any{"symbol": "ETH/USD", "height": 11},
		map[string]any{"symbol": "TRX/USD", "height": 3},
		map[string]any{"symbol": "TRX/USD", "height": 8},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}
	if err := os.WriteFile(genesisPath, out, 0o644); err != nil {
		t.Fatalf("write genesis: %v", err)
	}
}

func setOracleNodeAppConfig(t *testing.T, home, socket string, apiPort int) {
	t.Helper()

	appTomlPath := filepath.Join(home, "config", "app.toml")
	bz, err := os.ReadFile(appTomlPath)
	if err != nil {
		t.Fatalf("read app.toml: %v", err)
	}
	content := string(bz)
	content = strings.Replace(content, `sidecar_socket = ""`, fmt.Sprintf(`sidecar_socket = "%s"`, socket), 1)
	content = strings.Replace(content, `sidecar_timeout = "200ms"`, `sidecar_timeout = "1s"`, 1)
	content = strings.Replace(content, `address = "tcp://localhost:1317"`, fmt.Sprintf(`address = "tcp://127.0.0.1:%d"`, apiPort), 1)
	if err := os.WriteFile(appTomlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write app.toml: %v", err)
	}
}

func setOracleNodeCometConfig(t *testing.T, home string) {
	t.Helper()

	configPath := filepath.Join(home, "config", "config.toml")
	bz, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(bz)
	content = strings.Replace(content, "addr_book_strict = true", "addr_book_strict = false", 1)
	content = strings.Replace(content, "allow_duplicate_ip = false", "allow_duplicate_ip = true", 1)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

func startOracleSoakNodes(t *testing.T, repoRoot, bin string, nodes []*oracleSoakNode) {
	t.Helper()
	for _, node := range nodes {
		node.node = startNodeWithOptions(
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
			[]string{
				"--p2p.persistent_peers", persistentPeersFor(node, nodes),
				"--state-sync.snapshot-interval", "0",
			},
			nil,
		)
	}
}

func restartOracleSoakNode(t *testing.T, repoRoot, bin string, nodes []*oracleSoakNode, index int) {
	t.Helper()
	stopNode(t, nodes[index].node)
	nodes[index].node = startNodeWithOptions(
		t,
		repoRoot,
		bin,
		nodes[index].home,
		nodes[index].rpcPort,
		nodes[index].p2pPort,
		nodes[index].pprofPort,
		nodes[index].grpcPort,
		nodes[index].jsonRPCPort,
		nodes[index].jsonWSRPCPort,
		[]string{
			"--p2p.persistent_peers", persistentPeersFor(nodes[index], nodes),
			"--state-sync.snapshot-interval", "0",
		},
		nil,
	)
}

func stopOracleSoakNodes(t *testing.T, nodes []*oracleSoakNode) {
	t.Helper()
	for _, node := range nodes {
		stopNode(t, node.node)
	}
}

func persistentPeersFor(self *oracleSoakNode, nodes []*oracleSoakNode) string {
	peers := make([]string, 0, len(nodes)-1)
	for _, node := range nodes {
		if node.index == self.index {
			continue
		}
		peers = append(peers, fmt.Sprintf("%s@127.0.0.1:%d", node.nodeID, node.p2pPort))
	}
	return strings.Join(peers, ",")
}

func startOracleSidecars(
	t *testing.T,
	repoRoot, oracledBin string,
	nodes []*oracleSoakNode,
	sourceServer *oracleTestHTTPSServer,
) {
	t.Helper()
	for _, node := range nodes {
		startOracleSidecar(t, repoRoot, oracledBin, node, sourceServer)
	}
}

func startOracleSidecar(
	t *testing.T,
	repoRoot, oracledBin string,
	node *oracleSoakNode,
	sourceServer *oracleTestHTTPSServer,
) {
	t.Helper()
	node.sidecar = startOracleProcess(t, repoRoot, oracledBin, node, sourceServer)
}

func stopOracleSidecars(t *testing.T, nodes []*oracleSoakNode) {
	t.Helper()
	for _, node := range nodes {
		stopOracleSidecar(t, node)
	}
}

func stopOracleSidecar(t *testing.T, node *oracleSoakNode) {
	t.Helper()
	if node == nil || node.sidecar == nil {
		return
	}
	stopOracleProcess(t, node.sidecar, syscall.SIGTERM)
	node.sidecar = nil
}

type oracleTestSource struct {
	id          string
	url         string
	jsonPointer string
}

type oracleTestFeed struct {
	symbol     string
	interval   string
	staleAfter string
	sources    []oracleTestSource
}

type oracleTestHTTPSServer struct {
	*httptest.Server
	certFile string
}

func renderOracleSourcesConfig(baseURL string) []byte {
	feeds := []oracleTestFeed{
		{
			symbol: "BTC/USD", interval: "1s", staleAfter: "5s",
			sources: []oracleTestSource{
				{id: "btc-a", url: baseURL + "/price?symbol=BTC%2FUSD&price=101.0", jsonPointer: "/data/price"},
				{id: "btc-b", url: baseURL + "/price?symbol=BTC%2FUSD&price=102.0", jsonPointer: "/data/price"},
				{id: "btc-c", url: baseURL + "/price?symbol=BTC%2FUSD&price=103.0", jsonPointer: "/data/price"},
				{id: "btc-bad", url: baseURL + "/price?symbol=BTC%2FUSD&bad=1", jsonPointer: "/data/price"},
			},
		},
		{
			symbol: "ETH/USD", interval: "1s", staleAfter: "5s",
			sources: []oracleTestSource{
				{id: "eth-a", url: baseURL + "/price?symbol=ETH%2FUSD&price=201.0", jsonPointer: "/data/price"},
				{id: "eth-b", url: baseURL + "/price?symbol=ETH%2FUSD&price=202.0", jsonPointer: "/data/price"},
				{id: "eth-c", url: baseURL + "/price?symbol=ETH%2FUSD&price=203.0", jsonPointer: "/data/price"},
				{id: "eth-nonnumeric", url: baseURL + "/price?symbol=ETH%2FUSD&nonnumeric=1", jsonPointer: "/data/price"},
			},
		},
		{
			symbol: "TRX/USD", interval: "1s", staleAfter: "5s",
			sources: []oracleTestSource{
				{id: "trx-a", url: baseURL + "/price?symbol=TRX%2FUSD&price=0.10345&provider=a", jsonPointer: "/data/price"},
				{id: "trx-b", url: baseURL + "/price?symbol=TRX%2FUSD&price=0.10345&provider=b", jsonPointer: "/data/price"},
				{id: "trx-c", url: baseURL + "/price?symbol=TRX%2FUSD&price=0.10345&provider=c", jsonPointer: "/data/price"},
			},
		},
	}
	var content strings.Builder
	_, _ = content.WriteString("schema_version = 1\npublication_revision = \"e2e-v1\"\n")
	for _, feed := range feeds {
		_, _ = fmt.Fprintf(
			&content,
			"\n[[feeds]]\nsymbol = %q\ninterval = %q\nstale_after = %q\n",
			feed.symbol,
			feed.interval,
			feed.staleAfter,
		)
		for _, source := range feed.sources {
			_, _ = fmt.Fprintf(
				&content,
				"\n[[feeds.sources]]\nid = %q\nurl = %q\njson_pointer = %q\n",
				source.id,
				source.url,
				source.jsonPointer,
			)
		}
	}
	return []byte(content.String())
}

func startOracleSoakSourceServer(t *testing.T) *oracleTestHTTPSServer {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bad") == "1" {
			http.Error(w, "bad source", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nonnumeric") == "1" {
			_, _ = w.Write([]byte(`{"data":{"price":"not-a-number"}}`))
			return
		}
		price := r.URL.Query().Get("price")
		if price == "" {
			price = "1.0"
		}
		_, _ = fmt.Fprintf(w, `{"data":{"price":"%s"}}`, price)
	})
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate oracle test CA key: %v", err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Guru Oracle E2E CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCertificate, err := x509.CreateCertificate(
		rand.Reader,
		caTemplate,
		caTemplate,
		&caKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create oracle test CA: %v", err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate oracle test server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Guru Oracle E2E Source"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}
	serverCertificate, err := x509.CreateCertificate(
		rand.Reader,
		serverTemplate,
		caTemplate,
		&serverKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create oracle test server certificate: %v", err)
	}
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatalf("marshal oracle test server key: %v", err)
	}
	certificatePEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertificate}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate})...,
	)
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER})
	keyPair, err := tls.X509KeyPair(certificatePEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("load oracle test server key pair: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{keyPair},
	}
	server.StartTLS()
	certificate := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertificate,
	})
	certFile := filepath.Join(t.TempDir(), "oracle-test-ca.pem")
	if err := os.WriteFile(certFile, certificate, 0o600); err != nil {
		server.Close()
		t.Fatalf("write oracle test CA: %v", err)
	}
	return &oracleTestHTTPSServer{Server: server, certFile: certFile}
}

func runOracleSoakTxMix(t *testing.T, repoRoot, bin string, node *oracleSoakNode, accounts oracleSoakAccounts) {
	t.Helper()

	successTx(
		t, repoRoot, bin, node.home, node.rpcAddr,
		"tx", "bank", "send", "user", accounts.receiver, "1000000000000000000agxn",
		"--from", "user",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	successTx(
		t, repoRoot, bin, node.home, node.rpcAddr,
		"tx", "staking", "delegate", accounts.valoper0, "1000000000000000000agxn",
		"--from", "user",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "350000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	if out, err := runOracleSoakCmdE(
		repoRoot, bin, 30*time.Second,
		"tx", "oracle", "update-params", "3", "3", "100",
		"--from", "moderator",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--fees", highFeeAGXN,
		"--generate-only",
		"--output", "json",
	); err != nil {
		t.Fatalf("oracle generate-only command failed: %v\noutput:\n%s", err, out)
	}

	expectSyncFailure(
		t, repoRoot, bin,
		"tx", "bank", "send", "user", accounts.receiver, "1agxn",
		"--from", "user",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", "wrong-chain",
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	expectDeliverFailure(
		t, repoRoot, bin, node.home, node.rpcAddr,
		"tx", "gov", "vote", "999999", "yes",
		"--from", "user",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	expectCLIFailure(
		t, repoRoot, bin,
		"tx", "oracle", "update-params", "0", "3", "100",
		"--from", "moderator",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	waitForAllOracleSoakNodesHeight(t, repoRoot, bin, []*oracleSoakNode{node}, latestOracleSoakHeight(t, repoRoot, bin, []*oracleSoakNode{node})+10, 60*time.Second)
}

func assertUpdatedMinGasPriceRejectsGenesisFee(
	t *testing.T,
	repoRoot, bin string,
	node *oracleSoakNode,
	accounts oracleSoakAccounts,
) {
	t.Helper()

	expectSyncFailureContaining(
		t, repoRoot, bin, "minimum global fee",
		"tx", "bank", "send", "user", accounts.receiver, "1agxn",
		"--from", "user",
		"--keyring-backend", "test",
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--node", node.rpcAddr,
		"--gas", "250000",
		"--fees", oracleSoakGenesisFeeAt250KGas,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
}

func runOracleSoakWorkloadUntilHeight(
	t *testing.T,
	repoRoot, bin string,
	nodes []*oracleSoakNode,
	accounts oracleSoakAccounts,
	targetHeight int64,
) {
	t.Helper()

	currentHeight := latestOracleSoakHeight(t, repoRoot, bin, nodes)
	latestBTC := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD")
	round := 0
	deadline := time.Now().Add(oracleSoakTimeout)
	for currentHeight < targetHeight {
		if time.Now().After(deadline) {
			t.Fatalf("oracle soak workload did not reach height %d before %s; current=%d", targetHeight, oracleSoakTimeout, currentHeight)
		}
		round++
		txNode := nodes[round%len(nodes)]

		successTx(
			t, repoRoot, bin, txNode.home, txNode.rpcAddr,
			"tx", "bank", "send", txNode.keyName, accounts.receiver, strconv.FormatInt(int64(round), 10)+"agxn",
			"--from", txNode.keyName,
			"--keyring-backend", "test",
			"--home", txNode.home,
			"--chain-id", e2eChainID,
			"--node", txNode.rpcAddr,
			"--gas", "250000",
			"--fees", oracleSoakTxFee,
			"--broadcast-mode", "sync",
			"--yes",
			"--output", "json",
		)

		if round%2 == 0 {
			successTx(
				t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr,
				"tx", "staking", "delegate", accounts.valoper0, "1000000000000000agxn",
				"--from", "user",
				"--keyring-backend", "test",
				"--home", nodes[0].home,
				"--chain-id", e2eChainID,
				"--node", nodes[0].rpcAddr,
				"--gas", "350000",
				"--fees", oracleSoakTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			)
		}

		if round%3 == 0 {
			successTx(
				t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr,
				"tx", "constitution", "update-separation-ratio", "100000", "200000", "700000",
				"--from", "moderator",
				"--keyring-backend", "test",
				"--home", nodes[0].home,
				"--chain-id", e2eChainID,
				"--node", nodes[0].rpcAddr,
				"--gas", "250000",
				"--fees", oracleSoakTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			)
		}

		if out, err := runOracleSoakCmdE(
			repoRoot, bin, 30*time.Second,
			"tx", "oracle", "upsert-task", fmt.Sprintf("LOAD/%d", round), "11",
			"--enabled=false",
			"--from", "moderator",
			"--keyring-backend", "test",
			"--home", nodes[0].home,
			"--chain-id", e2eChainID,
			"--fees", oracleSoakTxFee,
			"--generate-only",
			"--output", "json",
		); err != nil {
			t.Fatalf("oracle workload generate-only command failed: %v\noutput:\n%s", err, out)
		}
		if round%2 == 0 {
			successTx(
				t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr,
				"tx", "oracle", "upsert-task", fmt.Sprintf("LOAD/%d", round), "11",
				"--enabled=false",
				"--from", "moderator",
				"--keyring-backend", "test",
				"--home", nodes[0].home,
				"--chain-id", e2eChainID,
				"--node", nodes[0].rpcAddr,
				"--gas", "250000",
				"--fees", oracleSoakTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			)
		}

		switch round % 4 {
		case 0:
			expectDeliverFailure(
				t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr,
				"tx", "gov", "vote", "999999", "yes",
				"--from", "user",
				"--keyring-backend", "test",
				"--home", nodes[0].home,
				"--chain-id", e2eChainID,
				"--node", nodes[0].rpcAddr,
				"--gas", "250000",
				"--fees", oracleSoakTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			)
		default:
			expectSyncFailure(
				t, repoRoot, bin,
				"tx", "bank", "send", txNode.keyName, accounts.receiver, "1agxn",
				"--from", txNode.keyName,
				"--keyring-backend", "test",
				"--home", txNode.home,
				"--chain-id", "wrong-chain",
				"--node", txNode.rpcAddr,
				"--gas", "250000",
				"--fees", oracleSoakTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			)
		}

		nextHeight := currentHeight + oracleSoakWorkloadStep
		if nextHeight > targetHeight {
			nextHeight = targetHeight
		}
		waitForAllOracleSoakNodesHeight(t, repoRoot, bin, nodes, nextHeight, 2*time.Minute)
		latestBTC = waitForOracleLatestHeight(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "BTC/USD", latestBTC.BlockHeight+1, 90*time.Second)
		assertOracleLatestFresh(t, "BTC/USD", latestBTC, nextHeight, 10)
		latestETH := queryOracleLatest(t, repoRoot, bin, nodes[0].home, nodes[0].rpcAddr, "ETH/USD")
		assertOracleLatestFresh(t, "ETH/USD", latestETH, nextHeight, 14)
		assertCommonOracleSoakBlock(t, repoRoot, bin, nodes, nextHeight)
		currentHeight = latestOracleSoakHeight(t, repoRoot, bin, nodes)
	}
}

func assertOracleLatestFresh(t *testing.T, symbol string, latest oracleLatestValue, checkpointHeight, maxLag int64) {
	t.Helper()
	if checkpointHeight > latest.BlockHeight && checkpointHeight-latest.BlockHeight > maxLag {
		t.Fatalf(
			"oracle latest %s is too stale at checkpoint %d: latest_height=%d max_lag=%d",
			symbol,
			checkpointHeight,
			latest.BlockHeight,
			maxLag,
		)
	}
}

func successTx(t *testing.T, repoRoot, bin, home, rpcAddr string, args ...string) {
	t.Helper()
	out, err := runOracleSoakCmdE(repoRoot, bin, 30*time.Second, args...)
	if err != nil {
		t.Fatalf("tx command failed: %s\nerror: %v\noutput:\n%s", strings.Join(args, " "), err, out)
	}
	txHash := parseTxHashFromSyncResponse(t, out)
	waitForTx(t, repoRoot, bin, home, rpcAddr, txHash)
}

func submitOracleSyncTxWithDiagnostics(
	t *testing.T,
	repoRoot, bin string,
	node *oracleSoakNode,
	sequenceBefore uint64,
	args ...string,
) string {
	t.Helper()

	broadcastOut, err := runOracleSoakCmdE(repoRoot, bin, 45*time.Second, args...)
	if err != nil {
		t.Fatalf(
			"oracle sync tx command failed: %s\nsequence_before=%d\nerror: %v\nbroadcast_output:\n%s\nnode_logs:\n%s",
			strings.Join(args, " "),
			sequenceBefore,
			err,
			broadcastOut,
			oracleSoakNodeLogs(node),
		)
	}
	t.Logf("oracle sync tx broadcast output: %s", broadcastOut)

	txHash := parseTxHashFromSyncResponse(t, broadcastOut)
	waitOut, waitErr := runCmdE(
		t,
		repoRoot,
		bin,
		"query", "wait-tx", txHash,
		"--node", node.rpcAddr,
		"--home", node.home,
		"--timeout", "60s",
		"--output", "json",
	)
	queryOut, queryErr := runCmdE(
		t,
		repoRoot,
		bin,
		"query", "tx", txHash,
		"--node", node.rpcAddr,
		"--home", node.home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)
	if waitErr != nil {
		t.Fatalf(
			"oracle sync tx wait-tx failed hash=%s sequence_before=%d error=%v\nbroadcast_output:\n%s\nquery_tx_error=%v\nquery_tx_output:\n%s\nnode_logs:\n%s",
			txHash,
			sequenceBefore,
			waitErr,
			broadcastOut,
			queryErr,
			queryOut,
			oracleSoakNodeLogs(node),
		)
	}
	t.Logf("oracle sync tx wait-tx output: %s", waitOut)
	if queryErr != nil {
		t.Fatalf(
			"oracle sync tx query tx diagnostic failed hash=%s sequence_before=%d error=%v\nbroadcast_output:\n%s\nwait_tx_output:\n%s\nquery_tx_output:\n%s\nnode_logs:\n%s",
			txHash,
			sequenceBefore,
			queryErr,
			broadcastOut,
			waitOut,
			queryOut,
			oracleSoakNodeLogs(node),
		)
	}
	t.Logf("oracle sync tx query tx output: %s", queryOut)

	code, ok := txResponseCode(waitOut)
	if !ok || code != 0 {
		t.Fatalf(
			"oracle sync tx delivered with unexpected code hash=%s code=%d parsed=%t sequence_before=%d\nbroadcast_output:\n%s\nwait_tx_output:\n%s\nquery_tx_output:\n%s\nnode_logs:\n%s",
			txHash,
			code,
			ok,
			sequenceBefore,
			broadcastOut,
			waitOut,
			queryOut,
			oracleSoakNodeLogs(node),
		)
	}

	return txHash
}

func expectSyncFailure(t *testing.T, repoRoot, bin string, args ...string) {
	t.Helper()
	out, err := runOracleSoakCmdE(repoRoot, bin, 30*time.Second, args...)
	if code, ok := txResponseCode(out); ok && code != 0 {
		return
	}
	if err != nil {
		return
	}
	t.Fatalf("expected sync tx failure, got err=%v output=%s", err, out)
}

func expectSyncFailureContaining(t *testing.T, repoRoot, bin, expected string, args ...string) {
	t.Helper()
	out, err := runOracleSoakCmdE(repoRoot, bin, 30*time.Second, args...)
	code, hasCode := txResponseCode(out)
	if err == nil && (!hasCode || code == 0) {
		t.Fatalf("expected sync tx failure, got err=%v output=%s", err, out)
	}
	if !strings.Contains(out, expected) {
		t.Fatalf("expected sync tx failure containing %q, got err=%v output=%s", expected, err, out)
	}
}

func expectCLIFailure(t *testing.T, repoRoot, bin string, args ...string) {
	t.Helper()
	out, err := runOracleSoakCmdE(repoRoot, bin, 30*time.Second, args...)
	if err == nil {
		t.Fatalf("expected CLI failure, output=%s", out)
	}
}

func expectDeliverFailure(t *testing.T, repoRoot, bin, home, rpcAddr string, args ...string) {
	t.Helper()
	out, err := runOracleSoakCmdE(repoRoot, bin, 30*time.Second, args...)
	if err != nil {
		t.Fatalf("tx command failed: %s\nerror: %v\noutput:\n%s", strings.Join(args, " "), err, out)
	}
	txHash := parseTxHashFromSyncResponse(t, out)
	out = runCmd(t, repoRoot, bin, "query", "wait-tx", txHash, "--node", rpcAddr, "--home", home, "--timeout", "60s", "--output", "json")
	code, ok := txResponseCode(out)
	if !ok || code == 0 {
		t.Fatalf("expected deliver tx failure, output=%s", out)
	}
}

func runOracleSoakCmdE(dir, name string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func txResponseCode(out string) (uint32, bool) {
	var txResp struct {
		Code uint32 `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &txResp); err != nil {
		return 0, false
	}
	return txResp.Code, true
}

func queryAccountSequence(t *testing.T, repoRoot, bin, home, rpcAddr, address string) uint64 {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"query", "auth", "account", address,
		"--node", rpcAddr,
		"--home", home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)
	var payload any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode account query: %v\n%s", err, out)
	}
	sequence, ok := findJSONUint64(payload, "sequence")
	if !ok {
		if _, hasAccountNumber := findJSONUint64(payload, "account_number"); hasAccountNumber {
			return 0
		}
		t.Fatalf("account sequence missing for %s: %s", address, out)
	}
	return sequence
}

func waitForAccountSequenceAtLeast(
	t *testing.T,
	repoRoot, bin, home, rpcAddr, address string,
	minSequence uint64,
	timeout time.Duration,
) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var sequence uint64
	for time.Now().Before(deadline) {
		sequence = queryAccountSequence(t, repoRoot, bin, home, rpcAddr, address)
		if sequence >= minSequence {
			return sequence
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("account %s sequence did not reach %d within %s; last=%d", address, minSequence, timeout, sequence)
	return 0
}

func queryOracleTaskSnapshot(t *testing.T, repoRoot, bin, home, rpcAddr, symbol string) oracleTaskSnapshot {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"query", "oracle", "task", symbol,
		"--node", rpcAddr,
		"--home", home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode oracle task query: %v\n%s", err, out)
	}
	task := mustJSONMap(t, payload, "task")
	interval, err := jsonInt64(task["submission_interval"])
	if err != nil {
		t.Fatalf("decode oracle task submission_interval: %v\n%s", err, out)
	}
	return oracleTaskSnapshot{
		symbol:             strings.ToUpper(strings.TrimSpace(fmt.Sprint(task["symbol"]))),
		valueType:          fmt.Sprint(task["value_type"]),
		enabled:            task["enabled"] == true,
		submissionInterval: interval,
	}
}

func findJSONUint64(value any, key string) (uint64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for candidateKey, candidateValue := range typed {
			if candidateKey == key {
				parsed, ok := jsonUint64(candidateValue)
				return parsed, ok
			}
			if parsed, ok := findJSONUint64(candidateValue, key); ok {
				return parsed, true
			}
		}
	case []any:
		for _, item := range typed {
			if parsed, ok := findJSONUint64(item, key); ok {
				return parsed, true
			}
		}
	}
	return 0, false
}

func jsonUint64(value any) (uint64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseUint(typed, 10, 64)
		return parsed, err == nil
	case float64:
		if typed < 0 || typed != float64(uint64(typed)) {
			return 0, false
		}
		return uint64(typed), true
	default:
		return 0, false
	}
}

func oracleSoakNodeLogs(node *oracleSoakNode) string {
	if node == nil || node.node == nil || node.node.logBuf == nil {
		return ""
	}
	return node.node.logBuf.String()
}

func assertNoOracleVoteExtensionValidationFailures(t *testing.T, nodes []*oracleSoakNode) {
	t.Helper()
	for _, node := range nodes {
		if count := oracleValidationErrorCount(oracleSoakNodeLogs(node)); count != 0 {
			t.Fatalf(
				"node %d logged %d oracle vote-extension validation errors:\n%s",
				node.index,
				count,
				oracleSoakNodeLogs(node),
			)
		}
	}
}

func waitForAllOracleSoakNodesHeight(t *testing.T, repoRoot, bin string, nodes []*oracleSoakNode, minHeight int64, timeout time.Duration) {
	t.Helper()
	for _, node := range nodes {
		waitForOracleSoakNodeHeight(t, repoRoot, bin, node, minHeight, timeout)
	}
}

func waitForOracleSoakNodeHeight(t *testing.T, repoRoot, bin string, node *oracleSoakNode, minHeight int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastStatus nodeStatus
	var lastErr error
	for time.Now().Before(deadline) {
		status, err := getNodeStatus(repoRoot, bin, node.home, node.rpcAddr)
		if err == nil {
			lastStatus = status
			height, convErr := strconv.ParseInt(status.SyncInfo.LatestBlockHeight, 10, 64)
			if convErr == nil && height >= minHeight {
				return
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}

	logs := ""
	if node.node != nil && node.node.logBuf != nil {
		logs = node.node.logBuf.String()
	}
	t.Fatalf(
		"node %d did not reach height %d within %s; last_err=%v last_status=%+v process_running=%t logs:\n%s",
		node.index,
		minHeight,
		timeout,
		lastErr,
		lastStatus.SyncInfo,
		node.node != nil && node.node.cmd != nil && node.node.cmd.Process != nil,
		logs,
	)
}

func latestOracleSoakHeight(t *testing.T, repoRoot, bin string, nodes []*oracleSoakNode) int64 {
	t.Helper()
	minHeight := int64(0)
	for i, node := range nodes {
		status := mustNodeStatus(t, repoRoot, bin, node.home, node.rpcAddr)
		height := mustParseInt64(t, status.SyncInfo.LatestBlockHeight)
		if i == 0 || height < minHeight {
			minHeight = height
		}
	}
	return minHeight
}

func assertOracleSoakHalted(t *testing.T, repoRoot, bin string, node *oracleSoakNode, stableFor time.Duration) {
	t.Helper()
	time.Sleep(4 * time.Second)
	start := mustParseInt64(t, mustNodeStatus(t, repoRoot, bin, node.home, node.rpcAddr).SyncInfo.LatestBlockHeight)
	time.Sleep(stableFor)
	end := mustParseInt64(t, mustNodeStatus(t, repoRoot, bin, node.home, node.rpcAddr).SyncInfo.LatestBlockHeight)
	if end > start {
		t.Fatalf("expected chain halt with two of four validators stopped, height advanced from %d to %d", start, end)
	}
}

func waitForOracleLatestHeight(t *testing.T, repoRoot, bin, home, rpcAddr, symbol string, minHeight int64, timeout time.Duration) oracleLatestValue {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	var lastValue oracleLatestValue
	for time.Now().Before(deadline) {
		value, err := queryOracleLatestE(repoRoot, bin, home, rpcAddr, symbol)
		if err == nil && value.BlockHeight >= minHeight {
			return value
		}
		if err == nil {
			lastValue = value
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("oracle latest %s did not reach height %d within %s: last_value=%+v last_err=%v", symbol, minHeight, timeout, lastValue, lastErr)
	return oracleLatestValue{}
}

func queryOracleLatest(t *testing.T, repoRoot, bin, home, rpcAddr, symbol string) oracleLatestValue {
	t.Helper()
	value, err := queryOracleLatestE(repoRoot, bin, home, rpcAddr, symbol)
	if err != nil {
		t.Fatalf("query oracle latest %s: %v", symbol, err)
	}
	return value
}

func queryOracleLatestE(repoRoot, bin, home, rpcAddr, symbol string) (oracleLatestValue, error) {
	out, err := runCmdE(nil, repoRoot, bin, "query", "oracle", "latest-value", symbol, "--node", rpcAddr, "--home", home, "--chain-id", e2eChainID, "--output", "json")
	if err != nil {
		return oracleLatestValue{}, err
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return oracleLatestValue{}, err
	}
	value, ok := payload["value"].(map[string]any)
	if !ok {
		return oracleLatestValue{}, fmt.Errorf("missing value in %s", out)
	}
	height, err := jsonInt64(value["block_height"])
	if err != nil {
		return oracleLatestValue{}, err
	}
	return oracleLatestValue{
		Symbol:      fmt.Sprint(value["symbol"]),
		Value:       fmt.Sprint(value["value"]),
		BlockHeight: height,
	}, nil
}

func waitForMinGasPriceAbove(t *testing.T, repoRoot, bin, home, rpcAddr, floor string, timeout time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastValue := ""
	var lastErr error
	for time.Now().Before(deadline) {
		current, err := queryCurrentMinGasPriceE(repoRoot, bin, home, rpcAddr)
		if err == nil {
			lastValue = current
			if decimalStringGreater(current, floor) {
				return current
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("min gas price did not rise above %s within %s: last_value=%q last_err=%v", floor, timeout, lastValue, lastErr)
	return ""
}

func queryCurrentMinGasPriceE(repoRoot, bin, home, rpcAddr string) (string, error) {
	out, err := runCmdE(nil, repoRoot, bin, "query", "constitution", "min-gas-price", "--node", rpcAddr, "--home", home, "--chain-id", e2eChainID, "--output", "json")
	if err != nil {
		return "", err
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	current, ok := payload["current_min_gas_price"].(string)
	if !ok || strings.TrimSpace(current) == "" {
		return "", fmt.Errorf("missing current_min_gas_price in %s", out)
	}
	return current, nil
}

func decimalStringGreater(value, floor string) bool {
	valueRat, ok := new(big.Rat).SetString(value)
	if !ok {
		return false
	}
	floorRat, ok := new(big.Rat).SetString(floor)
	if !ok {
		return false
	}
	return valueRat.Cmp(floorRat) > 0
}

func assertOracleHistory(t *testing.T, repoRoot, bin, home, rpcAddr, symbol string) {
	t.Helper()
	out := runCmd(t, repoRoot, bin, "query", "oracle", "history", symbol, "--node", rpcAddr, "--home", home, "--chain-id", e2eChainID, "--output", "json", "--limit", "200")
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode oracle history: %v\n%s", err, out)
	}
	history := mustJSONMap(t, payload, "history")
	values, ok := history["values"].([]any)
	if !ok || len(values) == 0 {
		t.Fatalf("missing oracle history values: %s", out)
	}
	if len(values) > 100 {
		t.Fatalf("oracle history exceeds limit: got=%d want<=100", len(values))
	}
}

func assertCommonOracleSoakBlock(t *testing.T, repoRoot, bin string, nodes []*oracleSoakNode, height int64) {
	t.Helper()
	var expected string
	for _, node := range nodes {
		out := runCmd(t, repoRoot, bin, "query", "block", "--type", "height", strconv.FormatInt(height, 10), "--node", node.rpcAddr, "--home", node.home, "--chain-id", e2eChainID, "--output", "json")
		fingerprint := oracleSoakBlockFingerprint(t, out)
		if expected == "" {
			expected = fingerprint
			continue
		}
		if fingerprint != expected {
			t.Fatalf("block fingerprint mismatch at height %d: expected=%s got=%s node=%d", height, expected, fingerprint, node.index)
		}
	}
}

func oracleSoakBlockFingerprint(t *testing.T, out string) string {
	t.Helper()
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode block output: %v\n%s", err, out)
	}
	header := map[string]any(nil)
	if block, ok := payload["block"].(map[string]any); ok {
		header = mustJSONMap(t, block, "header")
	} else {
		header = mustJSONMap(t, payload, "header")
	}
	headerBz, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal block header: %v", err)
	}
	appHash := stringAt(header, "app_hash")
	if appHash == "" {
		appHash = stringAt(header, "appHash")
	}
	validatorsHash := stringAt(header, "validators_hash")
	if validatorsHash == "" {
		validatorsHash = stringAt(header, "validatorsHash")
	}
	if appHash == "" || validatorsHash == "" {
		t.Fatalf("incomplete block fingerprint app_hash=%q validators_hash=%q output=%s", appHash, validatorsHash, out)
	}
	return string(headerBz) + "/" + appHash + "/" + validatorsHash
}

func exportOracleSoakGenesis(t *testing.T, repoRoot, bin, home string, height int64) string {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "exported-genesis.json")
	_ = runCmd(t, repoRoot, bin, "export", "--height", strconv.FormatInt(height, 10), "--home", home, "--output-document", outputPath)
	bz, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read exported genesis: %v", err)
	}
	return string(bz)
}

func assertZeroHeightExportRejected(t *testing.T, repoRoot, bin, home string, height int64) {
	t.Helper()
	out, err := runOracleSoakCmdE(
		repoRoot,
		bin,
		30*time.Second,
		"export",
		"--height", strconv.FormatInt(height, 10),
		"--for-zero-height",
		"--home", home,
	)
	if err == nil {
		t.Fatalf("expected zero-height export to fail, output=%s", out)
	}
	if !strings.Contains(out, "zero-height export is not supported") {
		t.Fatalf("unexpected zero-height export failure: %v\noutput:\n%s", err, out)
	}
}

func assertExportedOracleState(t *testing.T, exportedGenesis string, exportHeight int64) {
	t.Helper()
	var doc map[string]any
	decoder := json.NewDecoder(strings.NewReader(exportedGenesis))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("decode exported genesis: %v", err)
	}
	oracleState := mustJSONMap(t, mustJSONMap(t, doc, "app_state"), "oracle")
	tasks := mustJSONArray(t, oracleState, "tasks")
	schedule := mustJSONArray(t, oracleState, "task_schedule")
	latestValues := mustJSONArray(t, oracleState, "latest_values")
	if len(tasks) < 2 {
		t.Fatalf("expected exported oracle tasks, got %d", len(tasks))
	}
	if len(schedule) == 0 {
		t.Fatalf("expected exported oracle task_schedule")
	}
	if len(latestValues) == 0 {
		t.Fatalf("expected exported latest oracle values")
	}

	intervalBySymbol := map[string]int64{}
	enabledBySymbol := map[string]bool{}
	for _, raw := range tasks {
		task := raw.(map[string]any)
		symbol := strings.ToUpper(strings.TrimSpace(fmt.Sprint(task["symbol"])))
		interval, err := jsonInt64(task["submission_interval"])
		if err != nil {
			t.Fatalf("invalid task interval for %s: %v", symbol, err)
		}
		intervalBySymbol[symbol] = interval
		enabledBySymbol[symbol] = task["enabled"] == true
	}

	seen := map[string]struct{}{}
	firstHeightBySymbol := map[string]int64{}
	countBySymbol := map[string]int{}
	for _, raw := range schedule {
		entry := raw.(map[string]any)
		symbol := strings.ToUpper(strings.TrimSpace(fmt.Sprint(entry["symbol"])))
		height, err := jsonInt64(entry["height"])
		if err != nil || height <= 0 {
			t.Fatalf("invalid schedule height for %s: height=%v err=%v", symbol, entry["height"], err)
		}
		if height < exportHeight-1 {
			t.Fatalf("stale schedule entry for %s at height %d exported from height %d", symbol, height, exportHeight)
		}
		if !enabledBySymbol[symbol] {
			t.Fatalf("schedule references missing or disabled task %q", symbol)
		}
		key := fmt.Sprintf("%d/%s", height, symbol)
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate schedule entry %s", key)
		}
		seen[key] = struct{}{}
		if _, ok := firstHeightBySymbol[symbol]; !ok {
			firstHeightBySymbol[symbol] = height
		}
		if (height-firstHeightBySymbol[symbol])%intervalBySymbol[symbol] != 0 {
			t.Fatalf("schedule entry for %s at height %d is not interval-aligned", symbol, height)
		}
		countBySymbol[symbol]++
	}
	for symbol, enabled := range enabledBySymbol {
		if enabled && countBySymbol[symbol] < 2 {
			t.Fatalf("enabled task %s exported with %d schedule entries", symbol, countBySymbol[symbol])
		}
	}

	initialHeight, err := jsonInt64(doc["initial_height"])
	if err != nil {
		t.Fatalf("invalid exported initial_height: %v", err)
	}
	if initialHeight < 1 {
		initialHeight = 1
	}
	consensus := mustJSONMap(t, doc, "consensus")
	abciParams := mustJSONMap(t, mustJSONMap(t, consensus, "params"), "abci")
	voteExtensionsEnableHeight, err := jsonInt64(abciParams["vote_extensions_enable_height"])
	if err != nil {
		t.Fatalf("invalid exported vote_extensions_enable_height: %v", err)
	}
	if voteExtensionsEnableHeight < initialHeight {
		t.Fatalf(
			"exported genesis enables vote extensions before initial height: enable_height=%d initial_height=%d",
			voteExtensionsEnableHeight,
			initialHeight,
		)
	}
}

func keyAddress(t *testing.T, repoRoot, bin, home, keyName string) string {
	t.Helper()
	return strings.TrimSpace(runCmd(t, repoRoot, bin, "keys", "show", keyName, "-a", "--keyring-backend", "test", "--home", home))
}

func keyValoperAddress(t *testing.T, repoRoot, bin, home, keyName string) string {
	t.Helper()
	return strings.TrimSpace(runCmd(t, repoRoot, bin, "keys", "show", keyName, "-a", "--bech", "val", "--keyring-backend", "test", "--home", home))
}

func showOracleSoakNodeID(t *testing.T, repoRoot, bin, home string) string {
	t.Helper()
	if out, err := runCmdE(t, repoRoot, bin, "comet", "show-node-id", "--home", home); err == nil {
		return strings.TrimSpace(out)
	}
	return strings.TrimSpace(runCmd(t, repoRoot, bin, "tendermint", "show-node-id", "--home", home))
}

func copyGentxFiles(t *testing.T, fromHome, toHome string) {
	t.Helper()
	fromDir := filepath.Join(fromHome, "config", "gentx")
	toDir := filepath.Join(toHome, "config", "gentx")
	entries, err := os.ReadDir(fromDir)
	if err != nil {
		t.Fatalf("read gentx dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		copyFile(t, filepath.Join(fromDir, entry.Name()), filepath.Join(toDir, entry.Name()))
	}
}

func mustJSONMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("missing object key %q in %+v", key, parent)
	}
	return value
}

func mustJSONArray(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("missing array key %q in %+v", key, parent)
	}
	return value
}

func jsonInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric value %T", value)
	}
}

func stringAt(parent map[string]any, keys ...string) string {
	var current any = parent
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = asMap[key]
	}
	if current == nil {
		return ""
	}
	return fmt.Sprint(current)
}
