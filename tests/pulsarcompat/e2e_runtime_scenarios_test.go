package pulsarcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
)

const (
	e2eChainID                = "guru_631"
	e2eUpgradeNameV1          = "v1"
	e2eCrossBinaryUpgradeName = "cosmos-evm-v0.7.1"
	envEnableUpgradeHandlerV1 = "GURU_ENABLE_UPGRADE_HANDLER_V1"
	envUpgradeTargetBinary    = "GURU_E2E_UPGRADE_TARGET_BINARY"
	e2eAppDBBackend           = "goleveldb"
	highFeeAGXN               = "10000000000000000000agxn"
)

func TestE2EStateSyncUpgradeIBCCompatibility(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)

	home := filepath.Join(t.TempDir(), "node-a")
	bootstrapSingleValidatorGenesis(t, repoRoot, bin, home)
	seedStateSyncCustomGenesis(t, home)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", home)

	rpcPort := pickTCPPort(t)
	p2pPort := pickTCPPort(t)
	pprofPort := pickTCPPort(t)
	grpcPort := pickTCPPort(t)
	jsonRPCPort := pickTCPPort(t)
	jsonWSRPCPort := pickTCPPort(t)
	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort)

	node := startNode(t, repoRoot, bin, home, rpcPort, p2pPort, pprofPort, grpcPort, jsonRPCPort, jsonWSRPCPort)
	defer stopNode(t, node)
	waitForBlockHeight(t, repoRoot, bin, home, rpcAddr, 4, 40*time.Second)

	t.Run("ibc transfer query works", func(t *testing.T) {
		out := runCmd(
			t, repoRoot, bin,
			"query", "ibc-transfer", "params",
			"--node", rpcAddr,
			"--home", home,
			"--chain-id", e2eChainID,
			"--output", "json",
		)

		var params struct {
			SendEnabled    bool `json:"send_enabled"`
			ReceiveEnabled bool `json:"receive_enabled"`
		}
		if err := json.Unmarshal([]byte(out), &params); err != nil {
			t.Fatalf("unmarshal ibc-transfer params: %v\noutput:\n%s", err, out)
		}
		if !params.SendEnabled || !params.ReceiveEnabled {
			t.Fatalf("unexpected ibc-transfer params: %+v", params)
		}
	})

	t.Run("upgrade query works", func(t *testing.T) {
		out := runCmd(
			t, repoRoot, bin,
			"query", "upgrade", "module-versions",
			"--node", rpcAddr,
			"--home", home,
			"--chain-id", e2eChainID,
			"--output", "json",
		)

		var versions struct {
			ModuleVersions []struct {
				Name string `json:"name"`
			} `json:"module_versions"`
		}
		if err := json.Unmarshal([]byte(out), &versions); err != nil {
			t.Fatalf("unmarshal upgrade module-versions: %v\noutput:\n%s", err, out)
		}

		hasUpgrade := false
		hasIBC := false
		for _, v := range versions.ModuleVersions {
			if v.Name == "upgrade" {
				hasUpgrade = true
			}
			if v.Name == "ibc" {
				hasIBC = true
			}
		}
		if !hasUpgrade || !hasIBC {
			t.Fatalf("expected module versions to include upgrade and ibc; got: %+v", versions.ModuleVersions)
		}

		authorityOut := runCmd(
			t, repoRoot, bin,
			"query", "upgrade", "authority",
			"--node", rpcAddr,
			"--home", home,
			"--chain-id", e2eChainID,
			"--output", "json",
		)
		var authority struct {
			Address string `json:"address"`
		}
		if err := json.Unmarshal([]byte(authorityOut), &authority); err != nil {
			t.Fatalf("unmarshal upgrade authority: %v\noutput:\n%s", err, authorityOut)
		}
		if !strings.HasPrefix(authority.Address, "guru1") {
			t.Fatalf("unexpected upgrade authority address: %s", authority.Address)
		}
	})

	stopNode(t, node)

	t.Run("snapshot dump load restore works", func(t *testing.T) {
		listOut := runCmd(t, repoRoot, bin, "snapshots", "list", "--home", home)

		snapshotRe := regexp.MustCompile(`height:\s*(\d+)\s+format:\s*(\d+)`)
		matches := snapshotRe.FindAllStringSubmatch(listOut, -1)
		if len(matches) == 0 {
			t.Fatalf("no snapshots found in output:\n%s", listOut)
		}

		height := matches[0][1]
		format := matches[0][2]
		archive := filepath.Join(t.TempDir(), "snapshot.tar.gz")

		runCmd(
			t, repoRoot, bin,
			"snapshots", "dump", height, format,
			"--home", home,
			"--output", archive,
		)

		restoreHome := filepath.Join(t.TempDir(), "node-b")
		runInitWithConstitutionAddresses(t, repoRoot, bin, "restore", restoreHome)
		runCmd(t, repoRoot, bin, "snapshots", "load", archive, "--home", restoreHome)

		restoreListOut := runCmd(t, repoRoot, bin, "snapshots", "list", "--home", restoreHome)
		if !strings.Contains(restoreListOut, "height: "+height) {
			t.Fatalf("loaded snapshot height %s not found in restore home:\n%s", height, restoreListOut)
		}

		runCmd(t, repoRoot, bin, "snapshots", "restore", height, format, "--home", restoreHome)

		exportedPath := filepath.Join(t.TempDir(), "restored-genesis.json")
		runCmd(t, repoRoot, bin, "export", "--height", height, "--home", restoreHome, "--output-document", exportedPath)
		exported, err := os.ReadFile(exportedPath)
		if err != nil {
			t.Fatalf("read restored snapshot export: %v", err)
		}
		assertRestoredSnapshotCustomState(t, exported)
	})
}

func TestE2EOnChainUpgradeAppliesAfterBinarySwitch(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)
	targetBin := strings.TrimSpace(os.Getenv(envUpgradeTargetBinary))
	crossBinary := targetBin != ""
	upgradeName := e2eUpgradeNameV1
	if crossBinary {
		if !filepath.IsAbs(targetBin) {
			t.Fatalf("%s must be an absolute path: %s", envUpgradeTargetBinary, targetBin)
		}
		if info, err := os.Stat(targetBin); err != nil || info.IsDir() {
			t.Fatalf("invalid %s %q: %v", envUpgradeTargetBinary, targetBin, err)
		}
		upgradeName = e2eCrossBinaryUpgradeName
	} else {
		targetBin = bin
	}

	home := filepath.Join(t.TempDir(), "node-upgrade")
	bootstrapSingleValidatorGenesis(t, repoRoot, bin, home)
	if crossBinary {
		seedStateSyncCustomGenesis(t, home)
		privateKey, err := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
		if err != nil {
			t.Fatalf("parse cross-binary EVM fixture key: %v", err)
		}
		evmAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
		runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", evmAddress.Hex(), "1000000000000000000000agxn", "--home", home)
	}
	setFastGovernanceTimings(t, home)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", home)

	// old binary (without upgrade handler) runs until scheduled upgrade height
	oldRPCPort := pickTCPPort(t)
	oldP2PPort := pickTCPPort(t)
	oldPProfPort := pickTCPPort(t)
	oldGRPCPort := pickTCPPort(t)
	oldJSONRPCPort := pickTCPPort(t)
	oldJSONWSRPCPort := pickTCPPort(t)
	oldRPCAddr := fmt.Sprintf("tcp://127.0.0.1:%d", oldRPCPort)

	oldNode := startNodeWithOptions(
		t,
		repoRoot,
		bin,
		home,
		oldRPCPort,
		oldP2PPort,
		oldPProfPort,
		oldGRPCPort,
		oldJSONRPCPort,
		oldJSONWSRPCPort,
		nil,
		map[string]string{envEnableUpgradeHandlerV1: "0"},
	)
	defer stopNode(t, oldNode)
	waitForBlockHeight(t, repoRoot, bin, home, oldRPCAddr, 6, 45*time.Second)
	var preUpgradeState map[string]string
	var preUpgradeEVMState crossBinaryEVMState
	if crossBinary {
		preUpgradeState = queryStateSyncCustomState(t, repoRoot, bin, home, oldRPCAddr)
		assertSeededStateSyncCustomState(t, preUpgradeState)
		preUpgradeEVMState = deployCrossBinaryEVMFixture(t, oldJSONRPCPort)
	}

	status := mustNodeStatus(t, repoRoot, bin, home, oldRPCAddr)
	currentHeight := mustParseInt64(t, status.SyncInfo.LatestBlockHeight)
	upgradeHeight := currentHeight + 40
	authority := queryUpgradeAuthority(t, repoRoot, bin, home, oldRPCAddr)

	proposalPath := filepath.Join(t.TempDir(), "software-upgrade.json")
	writeSoftwareUpgradeProposal(t, proposalPath, authority, upgradeName, upgradeHeight)

	submitTxHash := submitGovProposal(t, repoRoot, bin, home, oldRPCAddr, proposalPath)
	waitForTx(t, repoRoot, bin, home, oldRPCAddr, submitTxHash)
	proposalID := latestProposalID(t, repoRoot, bin, home, oldRPCAddr)

	voteTxHash := voteGovProposalYes(t, repoRoot, bin, home, oldRPCAddr, proposalID)
	waitForTx(t, repoRoot, bin, home, oldRPCAddr, voteTxHash)
	waitForProposalStatus(t, repoRoot, bin, home, oldRPCAddr, proposalID, "PROPOSAL_STATUS_PASSED", 35*time.Second)

	haltHeight := waitForNodeHaltAtOrAfterHeight(t, repoRoot, bin, home, oldRPCAddr, upgradeHeight, 6*time.Second, 150*time.Second)
	if haltHeight < upgradeHeight {
		t.Fatalf("node halted before target upgrade height: got=%d want>=%d", haltHeight, upgradeHeight)
	}
	if !strings.Contains(oldNode.logBuf.String(), "UPGRADE") {
		t.Fatalf("expected old node logs to contain upgrade halt message, got:\n%s", oldNode.logBuf.String())
	}
	stopNode(t, oldNode)

	// Both phases use the current test binary, so BEX and transwap have existed
	// in this fresh database since genesis. The missing-store loader path is
	// covered by TestV1StoreLoaderAddsBEXAndTranswapToLegacyDatabase; this E2E isolates the on-chain
	// halt, handler, migration, and resume path without adding BEX twice.
	if crossBinary {
		setCometMempoolTypeForV071Upgrade(t, home)
	} else {
		upgradeInfoPath := filepath.Join(home, "data", "upgrade-info.json")
		if err := os.Remove(upgradeInfoPath); err != nil {
			t.Fatalf("remove same-binary upgrade info %s: %v", upgradeInfoPath, err)
		}
	}

	// new binary enables handler and resumes from upgrade height
	newRPCPort := pickTCPPort(t)
	newP2PPort := pickTCPPort(t)
	newPProfPort := pickTCPPort(t)
	newGRPCPort := pickTCPPort(t)
	newJSONRPCPort := pickTCPPort(t)
	newJSONWSRPCPort := pickTCPPort(t)
	newRPCAddr := fmt.Sprintf("tcp://127.0.0.1:%d", newRPCPort)

	newNode := startNodeWithOptions(
		t,
		repoRoot,
		targetBin,
		home,
		newRPCPort,
		newP2PPort,
		newPProfPort,
		newGRPCPort,
		newJSONRPCPort,
		newJSONWSRPCPort,
		nil,
		map[string]string{envEnableUpgradeHandlerV1: "1"},
	)
	defer stopNode(t, newNode)
	defer func() {
		if t.Failed() {
			t.Logf("new node logs:\n%s", newNode.logBuf.String())
		}
	}()

	waitForBlockHeight(t, repoRoot, targetBin, home, newRPCAddr, upgradeHeight+2, 90*time.Second)
	appliedHeight := queryAppliedUpgradeHeight(t, repoRoot, targetBin, home, newRPCAddr, upgradeName)
	if appliedHeight != upgradeHeight {
		t.Fatalf("unexpected applied upgrade height: got=%d want=%d", appliedHeight, upgradeHeight)
	}
	if crossBinary {
		postUpgradeState := queryStateSyncCustomState(t, repoRoot, targetBin, home, newRPCAddr)
		if !maps.Equal(preUpgradeState, postUpgradeState) {
			t.Fatalf("custom state differs after cross-binary upgrade:\nbefore=%v\nafter=%v", preUpgradeState, postUpgradeState)
		}
		postStatus := mustNodeStatus(t, repoRoot, targetBin, home, newRPCAddr)
		if postStatus.NodeInfo.Network != e2eChainID {
			t.Fatalf("SDK chain ID changed after cross-binary upgrade: got=%s want=%s", postStatus.NodeInfo.Network, e2eChainID)
		}
		assertCrossBinaryEVMState(t, newJSONRPCPort, preUpgradeEVMState)
	}
}

func TestE2ECometStateSyncFromPeerSnapshot(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)

	homeA := filepath.Join(t.TempDir(), "node-a")
	bootstrapSingleValidatorGenesis(t, repoRoot, bin, homeA)
	seedStateSyncCustomGenesis(t, homeA)
	runCmd(t, repoRoot, bin, "genesis", "validate-genesis", "--home", homeA)

	rpcPortA := pickTCPPort(t)
	p2pPortA := pickTCPPort(t)
	pprofPortA := pickTCPPort(t)
	grpcPortA := pickTCPPort(t)
	jsonRPCPortA := pickTCPPort(t)
	jsonWSRPCPortA := pickTCPPort(t)
	rpcAddrA := fmt.Sprintf("tcp://127.0.0.1:%d", rpcPortA)

	nodeA := startNodeWithOptions(
		t,
		repoRoot,
		bin,
		homeA,
		rpcPortA,
		p2pPortA,
		pprofPortA,
		grpcPortA,
		jsonRPCPortA,
		jsonWSRPCPortA,
		[]string{
			"--state-sync.snapshot-interval", "10",
			"--state-sync.snapshot-keep-recent", "50",
		},
		nil,
	)
	defer stopNode(t, nodeA)

	waitForBlockHeight(t, repoRoot, bin, homeA, rpcAddrA, 40, 90*time.Second)
	runCmd(t, repoRoot, bin, "keys", "add", "receiver", "--keyring-backend", "test", "--home", homeA)
	receiverAddr := strings.TrimSpace(runCmd(t, repoRoot, bin, "keys", "show", "receiver", "-a", "--keyring-backend", "test", "--home", homeA))
	seedChainWithBankSend(t, repoRoot, bin, homeA, rpcAddrA, receiverAddr)
	waitForBlockHeight(t, repoRoot, bin, homeA, rpcAddrA, 60, 90*time.Second)
	sourceCustomState := queryStateSyncCustomState(t, repoRoot, bin, homeA, rpcAddrA)
	assertSeededStateSyncCustomState(t, sourceCustomState)

	statusA := mustNodeStatus(t, repoRoot, bin, homeA, rpcAddrA)
	latestA := mustParseInt64(t, statusA.SyncInfo.LatestBlockHeight)
	if latestA < 10 {
		t.Fatalf("latest height too low for statesync trust setup: %d", latestA)
	}

	trustHeight := latestA
	trustHash := statusA.SyncInfo.LatestBlockHash
	if trustHash == "" {
		t.Fatalf("empty trust hash at height %d", trustHeight)
	}

	homeB := filepath.Join(t.TempDir(), "node-b")
	runInitWithConstitutionAddresses(t, repoRoot, bin, "nodeb", homeB)
	copyFile(t, filepath.Join(homeA, "config", "genesis.json"), filepath.Join(homeB, "config", "genesis.json"))
	enableStateSyncInConfig(t, homeB, rpcPortA, trustHeight, trustHash)

	persistentPeer := fmt.Sprintf("%s@127.0.0.1:%d", statusA.NodeInfo.ID, p2pPortA)
	rpcPortB := pickTCPPort(t)
	p2pPortB := pickTCPPort(t)
	pprofPortB := pickTCPPort(t)
	grpcPortB := pickTCPPort(t)
	jsonRPCPortB := pickTCPPort(t)
	jsonWSRPCPortB := pickTCPPort(t)
	rpcAddrB := fmt.Sprintf("tcp://127.0.0.1:%d", rpcPortB)

	nodeB := startNodeWithOptions(
		t,
		repoRoot,
		bin,
		homeB,
		rpcPortB,
		p2pPortB,
		pprofPortB,
		grpcPortB,
		jsonRPCPortB,
		jsonWSRPCPortB,
		[]string{"--p2p.persistent_peers", persistentPeer},
		nil,
	)
	defer stopNode(t, nodeB)

	statusB, syncErr := waitForStateSyncCompletion(t, repoRoot, bin, homeB, rpcAddrB, trustHeight, 120*time.Second, nodeB)
	if syncErr != nil {
		logsA := ""
		if nodeA.logBuf != nil {
			logsA = nodeA.logBuf.String()
		}
		logsB := ""
		if nodeB.logBuf != nil {
			logsB = nodeB.logBuf.String()
		}
		t.Fatalf("statesync did not complete: %v\nnodeA logs:\n%s\nnodeB logs:\n%s", syncErr, logsA, logsB)
	}
	earliestB := mustParseInt64(t, statusB.SyncInfo.EarliestBlockHeight)
	if earliestB <= 1 {
		t.Fatalf("expected statesync node to have truncated history; earliest height=%d status=%+v", earliestB, statusB.SyncInfo)
	}
	targetCustomState := queryStateSyncCustomState(t, repoRoot, bin, homeB, rpcAddrB)
	if !maps.Equal(sourceCustomState, targetCustomState) {
		t.Fatalf("custom module state differs after state sync:\nsource=%v\ntarget=%v", sourceCustomState, targetCustomState)
	}
}

func seedStateSyncCustomGenesis(t *testing.T, home string) {
	t.Helper()

	genesisPath := filepath.Join(home, "config", "genesis.json")
	bz, err := os.ReadFile(genesisPath)
	if err != nil {
		t.Fatalf("read state-sync genesis: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(bz, &doc); err != nil {
		t.Fatalf("decode state-sync genesis: %v", err)
	}
	appState := mustJSONMap(t, doc, "app_state")
	constitutionState := mustJSONMap(t, appState, "constitution")
	constitutionState["separation_ratio"] = map[string]any{
		"base_ppm":       111_111,
		"burn_ppm":       222_222,
		"validators_ppm": 666_667,
	}

	oracleState := mustJSONMap(t, appState, "oracle")
	oracleState["params"] = map[string]any{
		"min_validators": 2,
		"min_sources":    4,
		"history_limit":  7,
	}
	oracleState["tasks"] = []any{map[string]any{
		"symbol":              "STATE/SYNC",
		"value_type":          "VALUE_TYPE_NUMERIC",
		"enabled":             false,
		"submission_interval": 13,
	}}
	oracleState["task_schedule"] = []any{}
	value := map[string]any{
		"symbol":          "STATE/SYNC",
		"value_type":      "VALUE_TYPE_NUMERIC",
		"value":           "631.125",
		"block_height":    "7",
		"block_time_unix": "70",
	}
	oracleState["latest_values"] = []any{value}
	oracleState["history"] = []any{map[string]any{
		"symbol": "STATE/SYNC",
		"values": []any{value},
	}}

	_, moderatorAddress := e2eConstitutionAddresses(t)
	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	reserveAddress, err := accountCodec.BytesToString(authtypes.NewModuleAddress(bexkeeper.ReserveModuleName(1)))
	if err != nil {
		t.Fatalf("encode BEX reserve address: %v", err)
	}
	ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis("agxn", "transwap", "channel-0")
	if err != nil {
		t.Fatalf("derive BEX denom A: %v", err)
	}
	ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transwap", "channel-1")
	if err != nil {
		t.Fatalf("derive BEX denom B: %v", err)
	}
	bexState := mustJSONMap(t, appState, "bex")
	bexState["admins"] = []any{moderatorAddress}
	bexState["exchanges"] = []any{map[string]any{
		"id":                           1,
		"admin_address":                moderatorAddress,
		"reserve_address":              reserveAddress,
		"denom_a":                      "agxn",
		"port_a":                       "transwap",
		"channel_a":                    "channel-0",
		"ibc_denom_a":                  ibcDenomA,
		"denom_b":                      "gxusd",
		"port_b":                       "transwap",
		"channel_b":                    "channel-1",
		"ibc_denom_b":                  ibcDenomB,
		"oracle_symbol_a_to_b":         "AGXN/GXUSD",
		"oracle_symbol_b_to_a":         "GXUSD/AGXN",
		"fee_bps_a_to_b":               25,
		"fee_bps_b_to_a":               10,
		"limit_a_to_b":                 "10000",
		"limit_b_to_a":                 "10000",
		"volume_cap_a_to_b":            "1000",
		"volume_cap_b_to_a":            "1000",
		"revision":                     1,
		"status":                       1,
		"metadata":                     map[string]any{"fixture": "state-rich-upgrade"},
		"volume_epoch_seconds":         86400,
		"max_oracle_staleness_seconds": 300,
		"volume_window_generation":     1,
	}}
	bexState["collected_fees"] = []any{}
	bexState["locked_fees"] = []any{}
	bexState["volume_windows"] = []any{}
	bexState["reserve_depositors"] = []any{map[string]any{
		"exchange_id":       1,
		"depositor_address": moderatorAddress,
	}}
	bexState["pending_liabilities"] = []any{}
	bexState["next_exchange_id"] = 2

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode state-sync genesis: %v", err)
	}
	if err := os.WriteFile(genesisPath, out, 0o644); err != nil {
		t.Fatalf("write state-sync genesis: %v", err)
	}
}

func queryStateSyncCustomState(t *testing.T, repoRoot, bin, home, rpcAddr string) map[string]string {
	t.Helper()

	queries := map[string][]string{
		"constitution_ratio": {"query", "constitution", "separation-ratio"},
		"oracle_params":      {"query", "oracle", "params"},
		"oracle_task":        {"query", "oracle", "task", "STATE/SYNC"},
		"oracle_latest":      {"query", "oracle", "latest-value", "STATE/SYNC"},
		"oracle_history":     {"query", "oracle", "history", "STATE/SYNC", "--limit", "20"},
		"bex_exchange":       {"query", "bex", "exchange", "1"},
		"bex_depositors":     {"query", "bex", "reserve-depositors", "1"},
	}
	state := make(map[string]string, len(queries))
	for name, args := range queries {
		args = append(args,
			"--node", rpcAddr,
			"--home", home,
			"--chain-id", e2eChainID,
			"--output", "json",
		)
		out := runCmd(t, repoRoot, bin, args...)
		var payload any
		decoder := json.NewDecoder(strings.NewReader(out))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Fatalf("decode %s query: %v\n%s", name, err, out)
		}
		canonical, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("canonicalize %s query: %v", name, err)
		}
		state[name] = string(canonical)
	}
	return state
}

func assertSeededStateSyncCustomState(t *testing.T, state map[string]string) {
	t.Helper()

	expectedFragments := map[string][]string{
		"constitution_ratio": {`"base_ppm":111111`, `"burn_ppm":222222`, `"validators_ppm":666667`},
		"oracle_params":      {`"min_validators":2`, `"min_sources":4`, `"history_limit":7`},
		"oracle_task":        {`"symbol":"STATE/SYNC"`, `"submission_interval":13`},
		"oracle_latest":      {`"symbol":"STATE/SYNC"`, `"value":"631.125"`},
		"oracle_history":     {`"symbol":"STATE/SYNC"`, `"value":"631.125"`},
		"bex_exchange":       {`"fixture":"state-rich-upgrade"`},
		"bex_depositors":     {},
	}
	for name, fragments := range expectedFragments {
		for _, fragment := range fragments {
			if !strings.Contains(state[name], fragment) {
				t.Fatalf("%s query is missing %s: %s", name, fragment, state[name])
			}
		}
	}
	if strings.Contains(state["oracle_task"], `"enabled":true`) {
		t.Fatalf("state-sync oracle task unexpectedly enabled: %s", state["oracle_task"])
	}
	for _, name := range []string{"oracle_latest", "oracle_history"} {
		if !strings.Contains(state[name], `"block_height":7`) && !strings.Contains(state[name], `"block_height":"7"`) {
			t.Fatalf("%s query has unexpected block height: %s", name, state[name])
		}
	}
	if !strings.Contains(state["bex_exchange"], `"id":1`) && !strings.Contains(state["bex_exchange"], `"id":"1"`) {
		t.Fatalf("BEX query has unexpected exchange ID: %s", state["bex_exchange"])
	}
	_, moderatorAddress := e2eConstitutionAddresses(t)
	if !strings.Contains(state["bex_depositors"], moderatorAddress) {
		t.Fatalf("BEX depositor query is missing moderator %s: %s", moderatorAddress, state["bex_depositors"])
	}
	if !strings.Contains(state["bex_exchange"], `"status":1`) &&
		!strings.Contains(state["bex_exchange"], `"status":"EXCHANGE_STATUS_ACTIVE"`) {
		t.Fatalf("BEX query has unexpected exchange status: %s", state["bex_exchange"])
	}
}

func assertRestoredSnapshotCustomState(t *testing.T, exported []byte) {
	t.Helper()

	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(exported))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("decode restored snapshot export: %v", err)
	}
	appState := mustJSONMap(t, doc, "app_state")
	constitutionState := mustJSONMap(t, appState, "constitution")
	oracleState := mustJSONMap(t, appState, "oracle")
	bexState := mustJSONMap(t, appState, "bex")
	state := map[string]string{
		"constitution_ratio": canonicalJSONValue(t, constitutionState["separation_ratio"]),
		"oracle_params":      canonicalJSONValue(t, oracleState["params"]),
		"oracle_task":        canonicalJSONValue(t, oracleState["tasks"]),
		"oracle_latest":      canonicalJSONValue(t, oracleState["latest_values"]),
		"oracle_history":     canonicalJSONValue(t, oracleState["history"]),
		"bex_exchange":       canonicalJSONValue(t, bexState["exchanges"]),
		"bex_depositors":     canonicalJSONValue(t, bexState["reserve_depositors"]),
	}
	assertSeededStateSyncCustomState(t, state)
}

func canonicalJSONValue(t *testing.T, value any) string {
	t.Helper()
	bz, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("canonicalize JSON value: %v", err)
	}
	return string(bz)
}

type runningNode struct {
	cmd    *exec.Cmd
	logBuf *synchronizedBuffer
}

// synchronizedBuffer allows diagnostics to snapshot process output while the
// os/exec copy goroutines are still writing to it.
type synchronizedBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buf.String()
}

type nodeStatus struct {
	NodeInfo struct {
		ID      string `json:"id"`
		Network string `json:"network"`
	} `json:"node_info"`
	SyncInfo struct {
		LatestBlockHeight   string `json:"latest_block_height"`
		LatestBlockHash     string `json:"latest_block_hash"`
		EarliestBlockHeight string `json:"earliest_block_height"`
		CatchingUp          bool   `json:"catching_up"`
	} `json:"sync_info"`
}

type crossBinaryEVMState struct {
	ChainID         string
	Sender          string
	SenderNonce     uint64
	SenderBalance   string
	Contract        string
	ContractNonce   uint64
	ContractBalance string
	Code            string
	Storage         string
}

func deployCrossBinaryEVMFixture(t *testing.T, jsonRPCPort int) crossBinaryEVMState {
	t.Helper()

	privateKey, err := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("parse cross-binary EVM fixture key: %v", err)
	}
	sender := crypto.PubkeyToAddress(privateKey.PublicKey)
	client, err := ethclient.Dial(fmt.Sprintf("http://127.0.0.1:%d", jsonRPCPort))
	if err != nil {
		t.Fatalf("dial pre-upgrade EVM JSON-RPC: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	chainID, err := client.ChainID(ctx)
	cancel()
	if err != nil {
		t.Fatalf("query pre-upgrade EVM chain ID: %v", err)
	}
	if chainID.Cmp(big.NewInt(631)) != 0 {
		t.Fatalf("unexpected pre-upgrade EVM chain ID: got=%s want=631", chainID)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	nonce, err := client.PendingNonceAt(ctx, sender)
	cancel()
	if err != nil {
		t.Fatalf("query pre-upgrade EVM nonce: %v", err)
	}
	initCode := common.FromHex("0x602a600055600b6011600039600b6000f360005460005260206000f3")
	signedTx, err := gethtypes.SignNewTx(
		privateKey,
		gethtypes.LatestSignerForChainID(chainID),
		&gethtypes.LegacyTx{
			Nonce:    nonce,
			Value:    big.NewInt(0),
			Gas:      300_000,
			GasPrice: big.NewInt(1_000_000_000_000),
			Data:     initCode,
		},
	)
	if err != nil {
		t.Fatalf("sign cross-binary EVM fixture deployment: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	err = client.SendTransaction(ctx, signedTx)
	cancel()
	if err != nil {
		t.Fatalf("send cross-binary EVM fixture deployment: %v", err)
	}

	contract := crypto.CreateAddress(sender, nonce)
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		var code []byte
		code, lastErr = client.CodeAt(ctx, contract, nil)
		cancel()
		if lastErr == nil && len(code) != 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("wait for cross-binary EVM fixture deployment: %v", lastErr)
	}

	state := readCrossBinaryEVMState(t, client, sender, contract)
	if state.Code != "60005460005260206000f3" {
		t.Fatalf("unexpected deployed EVM fixture code: %s", state.Code)
	}
	if state.Storage != fmt.Sprintf("%064x", 42) {
		t.Fatalf("unexpected deployed EVM fixture storage: %s", state.Storage)
	}
	return state
}

func assertCrossBinaryEVMState(t *testing.T, jsonRPCPort int, want crossBinaryEVMState) {
	t.Helper()

	client, err := ethclient.Dial(fmt.Sprintf("http://127.0.0.1:%d", jsonRPCPort))
	if err != nil {
		t.Fatalf("dial post-upgrade EVM JSON-RPC: %v", err)
	}
	defer client.Close()

	got := readCrossBinaryEVMState(
		t,
		client,
		common.HexToAddress(want.Sender),
		common.HexToAddress(want.Contract),
	)
	if got != want {
		t.Fatalf("EVM consensus state differs after cross-binary upgrade:\nbefore=%+v\nafter=%+v", want, got)
	}
}

func readCrossBinaryEVMState(
	t *testing.T,
	client *ethclient.Client,
	sender, contract common.Address,
) crossBinaryEVMState {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	chainID, err := client.ChainID(ctx)
	if err != nil {
		t.Fatalf("query EVM chain ID: %v", err)
	}
	senderNonce, err := client.NonceAt(ctx, sender, nil)
	if err != nil {
		t.Fatalf("query EVM sender nonce: %v", err)
	}
	senderBalance, err := client.BalanceAt(ctx, sender, nil)
	if err != nil {
		t.Fatalf("query EVM sender balance: %v", err)
	}
	contractNonce, err := client.NonceAt(ctx, contract, nil)
	if err != nil {
		t.Fatalf("query EVM contract nonce: %v", err)
	}
	contractBalance, err := client.BalanceAt(ctx, contract, nil)
	if err != nil {
		t.Fatalf("query EVM contract balance: %v", err)
	}
	code, err := client.CodeAt(ctx, contract, nil)
	if err != nil {
		t.Fatalf("query EVM contract code: %v", err)
	}
	storage, err := client.StorageAt(ctx, contract, common.Hash{}, nil)
	if err != nil {
		t.Fatalf("query EVM contract storage: %v", err)
	}
	return crossBinaryEVMState{
		ChainID:         chainID.String(),
		Sender:          sender.Hex(),
		SenderNonce:     senderNonce,
		SenderBalance:   senderBalance.String(),
		Contract:        contract.Hex(),
		ContractNonce:   contractNonce,
		ContractBalance: contractBalance.String(),
		Code:            fmt.Sprintf("%x", code),
		Storage:         fmt.Sprintf("%x", storage),
	}
}

func startNode(
	t *testing.T,
	repoRoot, bin, home string,
	rpcPort, p2pPort, pprofPort, grpcPort, jsonRPCPort, jsonWSRPCPort int,
) *runningNode {
	t.Helper()
	return startNodeWithOptions(
		t,
		repoRoot,
		bin,
		home,
		rpcPort,
		p2pPort,
		pprofPort,
		grpcPort,
		jsonRPCPort,
		jsonWSRPCPort,
		nil,
		nil,
	)
}

func startNodeWithOptions(
	t *testing.T,
	repoRoot, bin, home string,
	rpcPort, p2pPort, pprofPort, grpcPort, jsonRPCPort, jsonWSRPCPort int,
	extraArgs []string,
	extraEnv map[string]string,
) *runningNode {
	t.Helper()

	return startNodeWithChainIDOption(
		t,
		repoRoot,
		bin,
		home,
		rpcPort,
		p2pPort,
		pprofPort,
		grpcPort,
		jsonRPCPort,
		jsonWSRPCPort,
		true,
		extraArgs,
		extraEnv,
	)
}

func startNodeWithChainIDOption(
	t *testing.T,
	repoRoot, bin, home string,
	rpcPort, p2pPort, pprofPort, grpcPort, jsonRPCPort, jsonWSRPCPort int,
	includeChainID bool,
	extraArgs []string,
	extraEnv map[string]string,
) *runningNode {
	t.Helper()

	args := []string{
		"start",
		"--home", home,
	}
	if includeChainID {
		args = append(args, "--chain-id", e2eChainID)
	}
	args = append(args,
		"--minimum-gas-prices", "0agxn",
		"--log_level", "error",
		"--app-db-backend", e2eAppDBBackend,
		"--rpc.laddr", fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort),
		"--p2p.laddr", fmt.Sprintf("tcp://127.0.0.1:%d", p2pPort),
		"--rpc.pprof_laddr", fmt.Sprintf("127.0.0.1:%d", pprofPort),
		"--grpc.address", fmt.Sprintf("127.0.0.1:%d", grpcPort),
		"--json-rpc.address", fmt.Sprintf("127.0.0.1:%d", jsonRPCPort),
		"--json-rpc.ws-address", fmt.Sprintf("127.0.0.1:%d", jsonWSRPCPort),
		"--state-sync.snapshot-interval", "2",
		"--state-sync.snapshot-keep-recent", "2",
	)
	args = append(args, extraArgs...)

	logBuf := &synchronizedBuffer{}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoRoot
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("node logs (%s):\n%s", home, logBuf.String())
		}
	})

	return &runningNode{
		cmd:    cmd,
		logBuf: logBuf,
	}
}

func stopNode(t *testing.T, node *runningNode) {
	t.Helper()
	if node == nil || node.cmd == nil || node.cmd.Process == nil {
		return
	}

	_ = node.cmd.Process.Signal(syscall.SIGINT)

	done := make(chan error, 1)
	go func() {
		done <- node.cmd.Wait()
	}()

	select {
	case <-time.After(10 * time.Second):
		_ = node.cmd.Process.Kill()
		_ = node.cmd.Wait()
	case <-done:
	}
	node.cmd = nil
}

func waitForBlockHeight(
	t *testing.T,
	repoRoot, bin, home, rpcAddr string,
	minHeight int64,
	timeout time.Duration,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := getNodeStatus(repoRoot, bin, home, rpcAddr)
		if err == nil {
			h, convErr := strconv.ParseInt(status.SyncInfo.LatestBlockHeight, 10, 64)
			if convErr == nil && h >= minHeight {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("node did not reach height %d within %s", minHeight, timeout)
}

func waitForNodeHaltAtOrAfterHeight(
	t *testing.T,
	repoRoot, bin, home, rpcAddr string,
	targetHeight int64,
	stableFor time.Duration,
	timeout time.Duration,
) int64 {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastHeight := int64(-1)
	lastChange := time.Now()

	for time.Now().Before(deadline) {
		status, err := getNodeStatus(repoRoot, bin, home, rpcAddr)
		if err == nil {
			height, convErr := strconv.ParseInt(status.SyncInfo.LatestBlockHeight, 10, 64)
			if convErr == nil {
				if height != lastHeight {
					lastHeight = height
					lastChange = time.Now()
				} else if height >= targetHeight && time.Since(lastChange) >= stableFor {
					return height
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("node did not halt at or after height %d within %s", targetHeight, timeout)
	return -1
}

func waitForStateSyncCompletion(
	t *testing.T,
	repoRoot, bin, home, rpcAddr string,
	minLatestHeight int64,
	timeout time.Duration,
	node *runningNode,
) (nodeStatus, error) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastStatus nodeStatus
	var lastErr error
	for time.Now().Before(deadline) {
		status, err := getNodeStatus(repoRoot, bin, home, rpcAddr)
		if err == nil {
			lastStatus = status
			latest, latestErr := strconv.ParseInt(status.SyncInfo.LatestBlockHeight, 10, 64)
			earliest, earliestErr := strconv.ParseInt(status.SyncInfo.EarliestBlockHeight, 10, 64)
			if latestErr == nil && earliestErr == nil &&
				!status.SyncInfo.CatchingUp &&
				latest >= minLatestHeight &&
				earliest > 1 {
				return status, nil
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}

	logs := ""
	if node != nil && node.logBuf != nil {
		logs = node.logBuf.String()
	}
	return nodeStatus{}, fmt.Errorf("statesync node did not finish within %s (last_err=%v last_status=%+v logs=%s)", timeout, lastErr, lastStatus.SyncInfo, logs)
}

func waitForProposalStatus(
	t *testing.T,
	repoRoot, bin, home, rpcAddr string,
	proposalID int64,
	expectedStatus string,
	timeout time.Duration,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := runCmdE(
			t,
			repoRoot,
			bin,
			"query", "gov", "proposal", strconv.FormatInt(proposalID, 10),
			"--node", rpcAddr,
			"--home", home,
			"--chain-id", e2eChainID,
			"--output", "json",
		)
		if err == nil {
			var proposalResp struct {
				Proposal struct {
					Status string `json:"status"`
				} `json:"proposal"`
			}
			if jsonErr := json.Unmarshal([]byte(out), &proposalResp); jsonErr == nil {
				if proposalResp.Proposal.Status == expectedStatus {
					return
				}
			}
		}
		time.Sleep(400 * time.Millisecond)
	}

	t.Fatalf("proposal %d did not reach status %s within %s", proposalID, expectedStatus, timeout)
}

func bootstrapSingleValidatorGenesis(t *testing.T, repoRoot, bin, home string) {
	t.Helper()
	runInitWithConstitutionAddresses(t, repoRoot, bin, "e2e", home)
	runCmd(t, repoRoot, bin, "keys", "add", "validator", "--keyring-backend", "test", "--home", home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", "validator", "100000000000000000000agxn", "--keyring-backend", "test", "--home", home)
	runCmd(t, repoRoot, bin, "genesis", "gentx", "validator", "10000000000000000000agxn", "--chain-id", e2eChainID, "--keyring-backend", "test", "--home", home, "--fees", highFeeAGXN)
	runCmd(t, repoRoot, bin, "genesis", "collect-gentxs", "--home", home)
}

func runInitWithConstitutionAddresses(t *testing.T, repoRoot, bin, moniker, home string) {
	t.Helper()

	baseAddress, moderatorAddress := e2eConstitutionAddresses(t)
	runCmd(
		t,
		repoRoot,
		bin,
		"init",
		moniker,
		"--chain-id", e2eChainID,
		"--home", home,
		"--constitution-base-address", baseAddress,
		"--constitution-moderator-address", moderatorAddress,
	)
}

func e2eConstitutionAddresses(t *testing.T) (string, string) {
	t.Helper()

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	baseAddress, err := accountCodec.BytesToString(bytes.Repeat([]byte{0x21}, 20))
	if err != nil {
		t.Fatalf("encode constitution base address: %v", err)
	}
	moderatorAddress, err := accountCodec.BytesToString(bytes.Repeat([]byte{0x22}, 20))
	if err != nil {
		t.Fatalf("encode constitution moderator address: %v", err)
	}

	return baseAddress, moderatorAddress
}

func setFastGovernanceTimings(t *testing.T, home string) {
	t.Helper()

	genesisPath := filepath.Join(home, "config", "genesis.json")
	genesisBz, err := os.ReadFile(genesisPath)
	if err != nil {
		t.Fatalf("read genesis file: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(genesisBz, &doc); err != nil {
		t.Fatalf("unmarshal genesis file: %v", err)
	}

	appState, ok := doc["app_state"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected app_state type in genesis")
	}
	govState, ok := appState["gov"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected gov state type in genesis")
	}
	params, ok := govState["params"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected gov params type in genesis")
	}

	params["voting_period"] = "6s"
	params["max_deposit_period"] = "6s"
	params["expedited_voting_period"] = "4s"

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal updated genesis: %v", err)
	}
	if err := os.WriteFile(genesisPath, out, 0o644); err != nil {
		t.Fatalf("write updated genesis: %v", err)
	}
}

func writeSoftwareUpgradeProposal(t *testing.T, path, authority, upgradeName string, upgradeHeight int64) {
	t.Helper()

	proposal := map[string]any{
		"messages": []map[string]any{
			{
				"@type":     "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
				"authority": authority,
				"plan": map[string]any{
					"name":   upgradeName,
					"height": strconv.FormatInt(upgradeHeight, 10),
					"info":   "e2e-upgrade",
				},
			},
		},
		"metadata":  "e2e-upgrade",
		"deposit":   "10000000agxn",
		"title":     "e2e software upgrade",
		"summary":   "apply " + upgradeName + " upgrade handler",
		"expedited": false,
	}

	bz, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		t.Fatalf("marshal software-upgrade proposal: %v", err)
	}
	if err := os.WriteFile(path, bz, 0o644); err != nil {
		t.Fatalf("write proposal file: %v", err)
	}
}

func queryUpgradeAuthority(t *testing.T, repoRoot, bin, home, rpcAddr string) string {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"query", "upgrade", "authority",
		"--node", rpcAddr,
		"--home", home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)
	var authority struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(out), &authority); err != nil {
		t.Fatalf("unmarshal upgrade authority: %v\noutput:\n%s", err, out)
	}
	if authority.Address == "" {
		t.Fatalf("empty upgrade authority in output: %s", out)
	}
	return authority.Address
}

func submitGovProposal(t *testing.T, repoRoot, bin, home, rpcAddr, proposalPath string) string {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"tx", "gov", "submit-proposal", proposalPath,
		"--from", "validator",
		"--keyring-backend", "test",
		"--home", home,
		"--chain-id", e2eChainID,
		"--node", rpcAddr,
		"--gas", "500000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	return parseTxHashFromSyncResponse(t, out)
}

func voteGovProposalYes(t *testing.T, repoRoot, bin, home, rpcAddr string, proposalID int64) string {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"tx", "gov", "vote", strconv.FormatInt(proposalID, 10), "yes",
		"--from", "validator",
		"--keyring-backend", "test",
		"--home", home,
		"--chain-id", e2eChainID,
		"--node", rpcAddr,
		"--gas", "200000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	return parseTxHashFromSyncResponse(t, out)
}

func seedChainWithBankSend(t *testing.T, repoRoot, bin, home, rpcAddr, toAddr string) {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"tx", "bank", "send", "validator", toAddr, "100agxn",
		"--from", "validator",
		"--keyring-backend", "test",
		"--home", home,
		"--chain-id", e2eChainID,
		"--node", rpcAddr,
		"--gas", "250000",
		"--fees", highFeeAGXN,
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	txHash := parseTxHashFromSyncResponse(t, out)
	waitForTx(t, repoRoot, bin, home, rpcAddr, txHash)
}

func latestProposalID(t *testing.T, repoRoot, bin, home, rpcAddr string) int64 {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"query", "gov", "proposals",
		"--node", rpcAddr,
		"--home", home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)

	var proposalsResp struct {
		Proposals []struct {
			ID string `json:"id"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(out), &proposalsResp); err != nil {
		t.Fatalf("unmarshal gov proposals: %v\noutput:\n%s", err, out)
	}
	if len(proposalsResp.Proposals) == 0 {
		t.Fatalf("no proposals found in gov query output:\n%s", out)
	}

	var maxID int64
	for _, p := range proposalsResp.Proposals {
		id, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil {
			continue
		}
		if id > maxID {
			maxID = id
		}
	}
	if maxID == 0 {
		t.Fatalf("failed to parse proposal id from output:\n%s", out)
	}
	return maxID
}

func parseTxHashFromSyncResponse(t *testing.T, out string) string {
	t.Helper()
	var txResp struct {
		TxHash string `json:"txhash"`
		Code   uint32 `json:"code"`
		RawLog string `json:"raw_log"`
	}
	if err := json.Unmarshal([]byte(out), &txResp); err != nil {
		t.Fatalf("unmarshal tx sync response: %v\noutput:\n%s", err, out)
	}
	if txResp.Code != 0 {
		t.Fatalf("tx sync response has non-zero code=%d raw_log=%s output=%s", txResp.Code, txResp.RawLog, out)
	}
	if txResp.TxHash == "" {
		t.Fatalf("tx hash missing in sync response: %s", out)
	}
	return txResp.TxHash
}

func waitForTx(t *testing.T, repoRoot, bin, home, rpcAddr, txHash string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var lastOutput string
	var lastErr error
	for {
		out, err := runCmdE(
			t,
			repoRoot,
			bin,
			"query", "tx", txHash,
			"--node", rpcAddr,
			"--home", home,
			"--output", "json",
		)
		if err == nil {
			var txResp struct {
				Code   uint32 `json:"code"`
				RawLog string `json:"raw_log"`
			}
			if err := json.Unmarshal([]byte(out), &txResp); err != nil {
				t.Fatalf("unmarshal tx query response: %v\noutput:\n%s", err, out)
			}
			if txResp.Code != 0 {
				t.Fatalf("tx query returned non-zero code=%d raw_log=%s output=%s", txResp.Code, txResp.RawLog, out)
			}
			return
		}

		lastOutput = out
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for transaction %s in the tx index: %v\nlast output:\n%s", txHash, lastErr, lastOutput)
}

func queryAppliedUpgradeHeight(t *testing.T, repoRoot, bin, home, rpcAddr, planName string) int64 {
	t.Helper()
	out := runCmd(
		t,
		repoRoot,
		bin,
		"query", "upgrade", "applied", planName,
		"--node", rpcAddr,
		"--home", home,
		"--chain-id", e2eChainID,
		"--output", "json",
	)

	var payload struct {
		Height string `json:"height"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err == nil && payload.Height != "" {
		return mustParseInt64(t, payload.Height)
	}

	re := regexp.MustCompile(`\d+`)
	match := re.FindString(out)
	if match == "" {
		t.Fatalf("failed to parse applied upgrade height from output:\n%s", out)
	}
	return mustParseInt64(t, match)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	bz, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read file %s: %v", src, err)
	}
	if err := os.WriteFile(dst, bz, 0o644); err != nil {
		t.Fatalf("write file %s: %v", dst, err)
	}
}

func enableStateSyncInConfig(t *testing.T, home string, rpcPort int, trustHeight int64, trustHash string) {
	t.Helper()

	configPath := filepath.Join(home, "config", "config.toml")
	bz, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}

	lines := strings.Split(string(bz), "\n")
	inStateSync := false
	inP2P := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[statesync]") {
			inStateSync = true
			inP2P = false
			continue
		}
		if strings.HasPrefix(trimmed, "[p2p]") {
			inP2P = true
			inStateSync = false
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[statesync]") && !strings.HasPrefix(trimmed, "[p2p]") {
			inStateSync = false
			inP2P = false
		}

		if inStateSync {
			switch {
			case strings.HasPrefix(trimmed, "enable ="):
				lines[i] = "enable = true"
			case strings.HasPrefix(trimmed, "rpc_servers ="):
				rpc := fmt.Sprintf("\"127.0.0.1:%d,127.0.0.1:%d\"", rpcPort, rpcPort)
				lines[i] = "rpc_servers = " + rpc
			case strings.HasPrefix(trimmed, "trust_height ="):
				lines[i] = fmt.Sprintf("trust_height = %d", trustHeight)
			case strings.HasPrefix(trimmed, "trust_hash ="):
				lines[i] = fmt.Sprintf("trust_hash = \"%s\"", trustHash)
			case strings.HasPrefix(trimmed, "discovery_time ="):
				lines[i] = "discovery_time = \"5s\""
			}
		}
		if inP2P && strings.HasPrefix(trimmed, "addr_book_strict =") {
			lines[i] = "addr_book_strict = false"
		}
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write updated config.toml: %v", err)
	}
}

func setCometMempoolTypeForV071Upgrade(t *testing.T, home string) {
	t.Helper()

	configPath := filepath.Join(home, "config", "config.toml")
	bz, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml for v0.7.1 upgrade: %v", err)
	}
	lines := strings.Split(string(bz), "\n")
	inMempool := false
	updated := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inMempool = trimmed == "[mempool]"
			continue
		}
		if inMempool && strings.HasPrefix(trimmed, "type =") {
			if trimmed != `type = "flood"` {
				t.Fatalf("unexpected pre-upgrade CometBFT mempool type: %s", trimmed)
			}
			lines[i] = `type = "app"`
			updated = true
			break
		}
	}
	if !updated {
		t.Fatalf("CometBFT mempool type was not found in %s", configPath)
	}
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write v0.7.1 CometBFT mempool config: %v", err)
	}
}

func mustNodeStatus(t *testing.T, repoRoot, bin, home, rpcAddr string) nodeStatus {
	t.Helper()
	status, err := getNodeStatus(repoRoot, bin, home, rpcAddr)
	if err != nil {
		t.Fatalf("query node status: %v", err)
	}
	return status
}

func getNodeStatus(repoRoot, bin, home, rpcAddr string) (nodeStatus, error) {
	out, err := runCmdE(
		nil,
		repoRoot,
		bin,
		"status",
		"--node", rpcAddr,
		"--home", home,
		"--output", "json",
	)
	if err != nil {
		return nodeStatus{}, fmt.Errorf("%w: %s", err, out)
	}

	var status nodeStatus
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		return nodeStatus{}, fmt.Errorf("unmarshal status: %w: %s", err, out)
	}
	return status, nil
}

func mustParseInt64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse int64 from %q: %v", s, err)
	}
	return v
}

func buildGurudBinary(t *testing.T, repoRoot string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gurud")
	runCmd(t, repoRoot, "go", "build", "-o", bin, "./cmd/gurud")
	return bin
}

func runCmd(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := runCmdE(t, dir, name, args...)
	if err != nil {
		t.Fatalf("command failed: %s %s\nerror: %v\noutput:\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func runCmdE(t *testing.T, dir, name string, args ...string) (string, error) {
	if t != nil {
		t.Helper()
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func projectRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func pickTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick tcp port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}
