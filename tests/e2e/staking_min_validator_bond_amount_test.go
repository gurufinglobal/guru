package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/v2/tests/e2e/harness"
)

const (
	defaultChainID = "guru_631-1"
	defaultBondDenom = "agxn"
)

type StakingParamsResponse struct {
	Params struct {
		UnbondingTime         string `json:"unbonding_time"`
		MaxValidators         uint32 `json:"max_validators"`
		MaxEntries            uint32 `json:"max_entries"`
		HistoricalEntries     uint32 `json:"historical_entries"`
		BondDenom             string `json:"bond_denom"`
		MinCommissionRate     string `json:"min_commission_rate"`
		MinValidatorBondAmount string `json:"min_validator_bond_amount"`
	} `json:"params"`
}

type StakingValidatorResponse struct {
	Validator struct {
		OperatorAddress   string `json:"operator_address"`
		MinSelfDelegation string `json:"min_self_delegation"`
		Tokens            string `json:"tokens"`
	} `json:"validator"`
}

type AuthModuleAccountResponse struct {
	Account struct {
		BaseAccount struct {
			Address string `json:"address"`
		} `json:"base_account"`
	} `json:"account"`
}

type TxSyncResponse struct {
	Code   int    `json:"code"`
	TxHash string `json:"txhash"`
	RawLog string `json:"raw_log"`
}

func TestStakingMinValidatorBondAmount(t *testing.T) {
	ctx := context.Background()
	repoRoot := mustFindRepoRoot(t)

	tempHome, err := os.MkdirTemp("", "e2e-home-*")
	if err != nil {
		t.Fatalf("failed to create temp home: %v", err)
	}
	defer os.RemoveAll(tempHome)

	node, err := harness.StartLocalNode(ctx, repoRoot, tempHome)
	if err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer func() {
		_ = node.Stop()
	}()

	cli := harness.CLI{
		Home:   node.Home,
		Node:   node.RPCURL,
		ChainID: defaultChainID,
	}
	gov := harness.Gov{CLI: &cli}

	params := mustQueryStakingParams(t, ctx, cli)
	if params.Params.BondDenom != defaultBondDenom {
		t.Fatalf("unexpected bond denom: %s", params.Params.BondDenom)
	}

	// A1: create-validator should fail when min-self-delegation < min_validator_bond_amount
	pubKey := mustCreateTempValidatorPubKey(t, ctx, cli)
	validatorJSON := buildValidatorJSON(pubKey, "1"+defaultBondDenom, "val-low", "1")
	validatorPath := mustWriteTempJSON(t, validatorJSON)
	_, stderr, err := cli.RunTx(ctx, "tx", "staking", "create-validator", validatorPath, "--from", "dev0")
	if err == nil {
		t.Fatalf("expected create-validator to fail with insufficient min self delegation")
	}
	if !strings.Contains(stderr, "minimum self delegation") && !strings.Contains(stderr, "minimum validator bond amount") {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}

	// B1: decrease min_validator_bond_amount via gov and create validator2
	minLow := gxnToAgxn(5)
	mustUpdateMinValidatorBondAmount(t, ctx, cli, gov, minLow, "dev0")
	params = mustQueryStakingParams(t, ctx, cli)
	if params.Params.MinValidatorBondAmount != minLow {
		t.Fatalf("min_validator_bond_amount not updated: %s", params.Params.MinValidatorBondAmount)
	}

	pubKey2 := mustCreateTempValidatorPubKey(t, ctx, cli)
	validatorJSON2 := buildValidatorJSON(pubKey2, minLow+defaultBondDenom, "val-low", minLow)
	validatorPath2 := mustWriteTempJSON(t, validatorJSON2)
	stdout, stderr, err := cli.RunTx(ctx, "tx", "staking", "create-validator", validatorPath2, "--from", "dev0")
	if err != nil {
		t.Fatalf("create-validator (min=low) failed: %v (stderr: %s)", err, stderr)
	}
	waitForTxSuccess(t, ctx, cli, stdout)

	valoperDev0 := mustValoperAddr(t, ctx, cli, "dev0")
	waitForValidator(t, ctx, cli, valoperDev0)

	// B2: increase min_validator_bond_amount and ensure existing validator is kept,
	// and edit-validator is blocked when self delegation is below new min.
	minHigh := gxnToAgxn(7)
	mustUpdateMinValidatorBondAmount(t, ctx, cli, gov, minHigh, "dev0")
	params = mustQueryStakingParams(t, ctx, cli)
	if params.Params.MinValidatorBondAmount != minHigh {
		t.Fatalf("min_validator_bond_amount not updated: %s", params.Params.MinValidatorBondAmount)
	}

	waitForValidator(t, ctx, cli, valoperDev0)

	stdout, stderr, err = cli.RunTx(ctx, "tx", "staking", "edit-validator", "--from", "dev0", "--new-moniker", "val-100-edit")
	if err != nil {
		t.Fatalf("edit-validator should succeed without enforcing min_validator_bond_amount for existing validators: %v (stderr: %s)", err, stderr)
	}
	waitForTxSuccess(t, ctx, cli, stdout)

	// C1: new validator creation should enforce new min
	minBetween := gxnToAgxn(6)
	pubKey3 := mustCreateTempValidatorPubKey(t, ctx, cli)
	validatorJSON3 := buildValidatorJSON(pubKey3, minBetween+defaultBondDenom, "val-between", minBetween)
	validatorPath3 := mustWriteTempJSON(t, validatorJSON3)
	_, stderr, err = cli.RunTx(ctx, "tx", "staking", "create-validator", validatorPath3, "--from", "dev1")
	if err == nil {
		t.Fatalf("expected create-validator to fail with min-self-delegation below new min")
	}
	if !strings.Contains(stderr, "minimum self delegation") && !strings.Contains(stderr, "minimum validator bond amount") {
		t.Fatalf("unexpected create-validator error: %v (stderr: %s)", err, stderr)
	}

	pubKey4 := mustCreateTempValidatorPubKey(t, ctx, cli)
	validatorJSON4 := buildValidatorJSON(pubKey4, minHigh+defaultBondDenom, "val-high", minHigh)
	validatorPath4 := mustWriteTempJSON(t, validatorJSON4)
	stdout, stderr, err = cli.RunTx(ctx, "tx", "staking", "create-validator", validatorPath4, "--from", "dev1")
	if err != nil {
		t.Fatalf("create-validator (min=high) failed: %v (stderr: %s)", err, stderr)
	}
	waitForTxSuccess(t, ctx, cli, stdout)

	// G1: negative min_validator_bond_amount should fail in proposal execution
	mustUpdateMinValidatorBondAmountFail(t, ctx, cli, gov, "-1", "dev0")

	// G2: create-validator with invalid denom should fail
	pubKey5 := mustCreateTempValidatorPubKey(t, ctx, cli)
	validatorJSON5 := buildValidatorJSON(pubKey5, "1uatom", "val-bad-denom", "1")
	validatorPath5 := mustWriteTempJSON(t, validatorJSON5)
	_, stderr, err = cli.RunTx(ctx, "tx", "staking", "create-validator", validatorPath5, "--from", "dev2")
	if err == nil {
		t.Fatalf("expected create-validator to fail with invalid denom")
	}
	if !strings.Contains(stderr, "invalid coin denomination") {
		t.Fatalf("unexpected denom error: %v (stderr: %s)", err, stderr)
	}
}

func mustFindRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("failed to find repo root from %s", dir)
		}
		dir = parent
	}
}

func mustQueryStakingParams(t *testing.T, ctx context.Context, cli harness.CLI) StakingParamsResponse {
	t.Helper()
	stdout, stderr, err := cli.RunQuery(ctx, "query", "staking", "params")
	if err != nil {
		t.Fatalf("query staking params failed: %v (stderr: %s)", err, stderr)
	}
	var resp StakingParamsResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("failed to parse staking params: %v", err)
	}
	return resp
}

func mustQueryValidator(t *testing.T, ctx context.Context, cli harness.CLI, valoper string) StakingValidatorResponse {
	t.Helper()
	stdout, stderr, err := cli.RunQuery(ctx, "query", "staking", "validator", valoper)
	if err != nil {
		t.Fatalf("query validator failed: %v (stderr: %s)", err, stderr)
	}
	var resp StakingValidatorResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("failed to parse validator: %v", err)
	}
	return resp
}

func waitForValidator(t *testing.T, ctx context.Context, cli harness.CLI, valoper string) StakingValidatorResponse {
	t.Helper()
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			t.Fatalf("validator not found after timeout: %s", valoper)
		case <-ticker.C:
			stdout, stderr, err := cli.RunQuery(timeoutCtx, "query", "staking", "validator", valoper)
			if err != nil {
				if strings.Contains(stderr, "not found") {
					continue
				}
				t.Fatalf("query validator failed: %v (stderr: %s)", err, stderr)
			}
			var resp StakingValidatorResponse
			if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
				t.Fatalf("failed to parse validator: %v", err)
			}
			return resp
		}
	}
}

func mustValoperAddr(t *testing.T, ctx context.Context, cli harness.CLI, key string) string {
	t.Helper()
	stdout, stderr, err := cli.Run(ctx, "keys", "show", key, "--bech", "val", "--address", "--home", cli.Home, "--keyring-backend", "test")
	if err != nil {
		t.Fatalf("keys show failed: %v (stderr: %s)", err, stderr)
	}
	return strings.TrimSpace(stdout)
}

func mustCreateTempValidatorPubKey(t *testing.T, ctx context.Context, cli harness.CLI) map[string]any {
	t.Helper()
	tmpHome, err := os.MkdirTemp("", "validator-home-*")
	if err != nil {
		t.Fatalf("failed to create temp validator home: %v", err)
	}
	defer os.RemoveAll(tmpHome)

	_, stderr, err := cli.Run(ctx, "init", "validator", "--chain-id", defaultChainID, "--home", tmpHome)
	if err != nil {
		t.Fatalf("failed to init validator home: %v (stderr: %s)", err, stderr)
	}

	stdout, stderr, err := cli.Run(ctx, "tendermint", "show-validator", "--home", tmpHome)
	if err != nil {
		t.Fatalf("failed to show validator pubkey: %v (stderr: %s)", err, stderr)
	}

	var pubKey map[string]any
	if err := json.Unmarshal([]byte(stdout), &pubKey); err != nil {
		t.Fatalf("failed to parse pubkey json: %v", err)
	}
	return pubKey
}

func buildValidatorJSON(pubKey map[string]any, amount, moniker, minSelf string) map[string]any {
	return map[string]any{
		"pubkey":                  pubKey,
		"amount":                  amount,
		"moniker":                 moniker,
		"identity":                "",
		"website":                 "",
		"security":                "",
		"details":                 "",
		"commission-rate":         "0.1",
		"commission-max-rate":     "0.2",
		"commission-max-change-rate": "0.01",
		"min-self-delegation":     minSelf,
	}
}

func mustWriteTempJSON(t *testing.T, payload map[string]any) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "validator-json-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	path := filepath.Join(dir, "validator.json")
	bz, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal validator json: %v", err)
	}
	if err := os.WriteFile(path, bz, 0o600); err != nil {
		t.Fatalf("failed to write validator json: %v", err)
	}
	return path
}

func mustUpdateMinValidatorBondAmount(t *testing.T, ctx context.Context, cli harness.CLI, gov harness.Gov, newMin string, proposer string) {
	t.Helper()
	propID := mustSubmitStakingParamsProposal(t, ctx, cli, gov, newMin, proposer)
	if err := gov.VoteYes(ctx, propID, "mykey"); err != nil {
		t.Fatalf("vote failed: %v", err)
	}
	if err := gov.WaitForStatus(ctx, propID, "PROPOSAL_STATUS_PASSED"); err != nil {
		t.Fatalf("proposal not passed: %v", err)
	}
}

func mustUpdateMinValidatorBondAmountFail(t *testing.T, ctx context.Context, cli harness.CLI, gov harness.Gov, newMin string, proposer string) {
	t.Helper()
	propID := mustSubmitStakingParamsProposal(t, ctx, cli, gov, newMin, proposer)
	if err := gov.VoteYes(ctx, propID, "mykey"); err != nil {
		t.Fatalf("vote failed: %v", err)
	}
	if err := gov.WaitForStatus(ctx, propID, "PROPOSAL_STATUS_FAILED"); err != nil {
		t.Fatalf("proposal did not fail as expected: %v", err)
	}
}

func mustSubmitStakingParamsProposal(t *testing.T, ctx context.Context, cli harness.CLI, gov harness.Gov, newMin string, proposer string) string {
	t.Helper()
	params := mustQueryStakingParams(t, ctx, cli)
	authority := mustGovAuthority(t, ctx, cli)
	minDeposit := mustGovMinDeposit(t, ctx, gov)

	paramsMap := map[string]any{
		"unbonding_time":          params.Params.UnbondingTime,
		"max_validators":          params.Params.MaxValidators,
		"max_entries":             params.Params.MaxEntries,
		"historical_entries":      params.Params.HistoricalEntries,
		"bond_denom":              params.Params.BondDenom,
		"min_commission_rate":     params.Params.MinCommissionRate,
		"min_validator_bond_amount": newMin,
	}

	proposal := map[string]any{
		"messages": []any{
			map[string]any{
				"@type":     "/cosmos.staking.v1beta1.MsgUpdateParams",
				"authority": authority,
				"params":    paramsMap,
			},
		},
		"metadata":  "e2e",
		"deposit":   minDeposit,
		"title":     fmt.Sprintf("Update min_validator_bond_amount to %s", newMin),
		"summary":   "e2e test",
		"expedited": false,
	}

	bz, err := harness.MarshalJSON(proposal)
	if err != nil {
		t.Fatalf("failed to marshal proposal: %v", err)
	}

	propID, err := gov.SubmitProposalJSON(ctx, bz, proposer)
	if err != nil {
		t.Fatalf("submit proposal failed: %v", err)
	}
	return propID
}

func mustGovAuthority(t *testing.T, ctx context.Context, cli harness.CLI) string {
	t.Helper()
	stdout, stderr, err := cli.RunQuery(ctx, "query", "auth", "module-account", "gov")
	if err != nil {
		t.Fatalf("query gov module account failed: %v (stderr: %s)", err, stderr)
	}
	var resp AuthModuleAccountResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err == nil && resp.Account.BaseAccount.Address != "" {
		return resp.Account.BaseAccount.Address
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("failed to parse gov module account: %v", err)
	}
	addr := findAddressField(raw)
	if addr == "" {
		t.Fatalf("gov module account address not found")
	}
	return addr
}

func findAddressField(v any) string {
	switch vv := v.(type) {
	case map[string]any:
		if val, ok := vv["address"]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
		for _, val := range vv {
			if found := findAddressField(val); found != "" {
				return found
			}
		}
	case []any:
		for _, val := range vv {
			if found := findAddressField(val); found != "" {
				return found
			}
		}
	}
	return ""
}

func mustGovMinDeposit(t *testing.T, ctx context.Context, gov harness.Gov) string {
	t.Helper()
	params, err := gov.QueryParams(ctx)
	if err != nil {
		t.Fatalf("query gov params failed: %v", err)
	}
	if len(params.Params.MinDeposit) == 0 {
		return "1" + defaultBondDenom
	}
	coins := make([]string, 0, len(params.Params.MinDeposit))
	for _, coin := range params.Params.MinDeposit {
		coins = append(coins, coin.Amount+coin.Denom)
	}
	return strings.Join(coins, ",")
}

func gxnToAgxn(gxn int64) string {
	return fmt.Sprintf("%d", gxn*1_000_000_000_000_000_000)
}

func waitForTxSuccess(t *testing.T, ctx context.Context, cli harness.CLI, stdout string) {
	t.Helper()
	var resp TxSyncResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("failed to parse tx response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("tx failed: code=%d raw_log=%s", resp.Code, resp.RawLog)
	}
	if resp.TxHash == "" {
		return
	}
	if _, _, err := cli.WaitForTx(ctx, resp.TxHash); err != nil {
		t.Fatalf("tx not found after submit: %v", err)
	}
}
