package pulsarcompat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const envDistributionStakingStress = "GURU_E2E_DISTRIBUTION_STAKING_STRESS"
const distributionStakingStressTxFee = "1agxn"

func TestE2EDistributionStakingConcurrentRewardStress(t *testing.T) {
	if os.Getenv(envDistributionStakingStress) != "1" {
		t.Skipf("set %s=1 to run the distribution/staking stress test", envDistributionStakingStress)
	}

	repoRoot := projectRootFromTestFile(t)
	bin := buildGurudBinary(t, repoRoot)

	home := filepath.Join(t.TempDir(), "node-distribution-staking")
	bootstrapDistributionStakingStressGenesis(t, repoRoot, bin, home)
	setDistributionStakingStressConsensusTiming(t, home)

	rpcPort := pickTCPPort(t)
	p2pPort := pickTCPPort(t)
	pprofPort := pickTCPPort(t)
	grpcPort := pickTCPPort(t)
	jsonRPCPort := pickTCPPort(t)
	jsonWSRPCPort := pickTCPPort(t)
	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", rpcPort)

	node := startNode(t, repoRoot, bin, home, rpcPort, p2pPort, pprofPort, grpcPort, jsonRPCPort, jsonWSRPCPort)
	defer stopNode(t, node)
	waitForBlockHeight(t, repoRoot, bin, home, rpcAddr, 5, 45*time.Second)

	valoper := strings.TrimSpace(runCmd(t, repoRoot, bin,
		"keys", "show", "validator", "--bech", "val", "-a",
		"--keyring-backend", "test",
		"--home", home,
	))

	const accountCount = 96
	accounts := make([]string, accountCount)
	addresses := make([]string, accountCount)
	for i := range accounts {
		name := fmt.Sprintf("stress%d", i)
		runCmd(t, repoRoot, bin, "keys", "add", name, "--keyring-backend", "test", "--home", home)
		addr := strings.TrimSpace(runCmd(t, repoRoot, bin, "keys", "show", name, "-a", "--keyring-backend", "test", "--home", home))
		accounts[i] = name
		addresses[i] = addr
	}

	for start := 0; start < len(addresses); start += 32 {
		end := start + 32
		if end > len(addresses) {
			end = len(addresses)
		}
		args := append([]string{"tx", "bank", "multi-send", "validator"}, addresses[start:end]...)
		args = append(args,
			"1000000000000000agxn",
			"--from", "validator",
			"--keyring-backend", "test",
			"--home", home,
			"--chain-id", e2eChainID,
			"--node", rpcAddr,
			"--gas", "8000000",
			"--fees", distributionStakingStressTxFee,
			"--broadcast-mode", "sync",
			"--yes",
			"--output", "json",
		)
		txHash := parseTxHashFromSyncResponse(t, runCmd(t, repoRoot, bin, args...))
		waitForTx(t, repoRoot, bin, home, rpcAddr, txHash)
	}

	submitConcurrentStressTxs(t, repoRoot, bin, home, rpcAddr, accounts, func(account string) []string {
		return []string{
			"tx", "staking", "delegate", valoper, "100000agxn",
			"--from", account,
			"--keyring-backend", "test",
			"--home", home,
			"--chain-id", e2eChainID,
			"--node", rpcAddr,
			"--gas", "600000",
			"--fees", distributionStakingStressTxFee,
			"--broadcast-mode", "sync",
			"--yes",
			"--output", "json",
		}
	})
	waitForBlockHeight(t, repoRoot, bin, home, rpcAddr, mustParseInt64(t, mustNodeStatus(t, repoRoot, bin, home, rpcAddr).SyncInfo.LatestBlockHeight)+4, 45*time.Second)
	requireNoDistributionReferencePanic(t, node)

	for round := 0; round < 10; round++ {
		submitConcurrentStressTxs(t, repoRoot, bin, home, rpcAddr, accounts, func(account string) []string {
			if round%2 == 0 {
				return []string{
					"tx", "staking", "delegate", valoper, "1000agxn",
					"--from", account,
					"--keyring-backend", "test",
					"--home", home,
					"--chain-id", e2eChainID,
					"--node", rpcAddr,
					"--gas", "600000",
					"--fees", distributionStakingStressTxFee,
					"--broadcast-mode", "sync",
					"--yes",
					"--output", "json",
				}
			}
			return []string{
				"tx", "distribution", "withdraw-rewards", valoper,
				"--from", account,
				"--keyring-backend", "test",
				"--home", home,
				"--chain-id", e2eChainID,
				"--node", rpcAddr,
				"--gas", "600000",
				"--fees", distributionStakingStressTxFee,
				"--broadcast-mode", "sync",
				"--yes",
				"--output", "json",
			}
		})
		currentHeight := mustParseInt64(t, mustNodeStatus(t, repoRoot, bin, home, rpcAddr).SyncInfo.LatestBlockHeight)
		waitForBlockHeight(t, repoRoot, bin, home, rpcAddr, currentHeight+4, 45*time.Second)
		requireNoDistributionReferencePanic(t, node)
	}
}

func bootstrapDistributionStakingStressGenesis(t *testing.T, repoRoot, bin, home string) {
	t.Helper()

	runInitWithConstitutionAddresses(t, repoRoot, bin, "e2e", home)
	runCmd(t, repoRoot, bin, "keys", "add", "validator", "--keyring-backend", "test", "--home", home)
	runCmd(t, repoRoot, bin, "genesis", "add-genesis-account", "validator", "1000000000000000000000000000000agxn", "--keyring-backend", "test", "--home", home)
	runCmd(t, repoRoot, bin, "genesis", "gentx", "validator", "10000000000000000000agxn", "--chain-id", e2eChainID, "--keyring-backend", "test", "--home", home)
	runCmd(t, repoRoot, bin, "genesis", "collect-gentxs", "--home", home)
}

func setDistributionStakingStressConsensusTiming(t *testing.T, home string) {
	t.Helper()

	configPath := filepath.Join(home, "config", "config.toml")
	bz, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}

	lines := strings.Split(string(bz), "\n")
	inConsensus := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "[consensus]"):
			inConsensus = true
			continue
		case strings.HasPrefix(trimmed, "["):
			inConsensus = false
		}
		if !inConsensus {
			continue
		}
		if strings.HasPrefix(trimmed, "timeout_commit =") {
			lines[i] = `timeout_commit = "3s"`
		}
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

func submitConcurrentStressTxs(
	t *testing.T,
	repoRoot, bin, home, rpcAddr string,
	accounts []string,
	buildArgs func(account string) []string,
) {
	t.Helper()

	var wg sync.WaitGroup
	errs := make(chan string, len(accounts))
	accepted := make(chan struct{}, len(accounts))
	for _, account := range accounts {
		account := account
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := runCmdE(t, repoRoot, bin, buildArgs(account)...)
			if err != nil {
				errs <- fmt.Sprintf("%s: %v: %s", account, err, out)
				return
			}
			if !stressSyncResponseAccepted(out) {
				errs <- fmt.Sprintf("%s: non-zero sync response: %s", account, out)
				return
			}
			accepted <- struct{}{}
		}()
	}
	wg.Wait()
	close(errs)
	close(accepted)

	if len(accepted) < len(accounts)/2 {
		var details []string
		for err := range errs {
			details = append(details, err)
			if len(details) == 8 {
				break
			}
		}
		t.Fatalf("too few stress txs accepted: accepted=%d total=%d sample_errors=%s", len(accepted), len(accounts), strings.Join(details, "\n"))
	}
}

func stressSyncResponseAccepted(out string) bool {
	var txResp struct {
		TxHash string `json:"txhash"`
		Code   uint32 `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &txResp); err != nil {
		return false
	}
	return txResp.Code == 0 && txResp.TxHash != ""
}

func requireNoDistributionReferencePanic(t *testing.T, node *runningNode) {
	t.Helper()
	if node == nil || node.logBuf == nil {
		return
	}
	logs := node.logBuf.String()
	if strings.Contains(logs, "cannot set negative reference count") {
		t.Fatalf("distribution historical rewards reference count panic found in node logs:\n%s", tailString(logs, 12000))
	}
}

func tailString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
}
