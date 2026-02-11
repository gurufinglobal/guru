package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Gov struct {
	CLI *CLI
}

type GovParamsResponse struct {
	Params struct {
		MinDeposit []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"min_deposit"`
		VotingPeriod string `json:"voting_period"`
	} `json:"params"`
}

type GovProposalResponse struct {
	Proposal struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		FailedReason string `json:"failed_reason"`
	} `json:"proposal"`
}

type TxResponse struct {
	Code   int    `json:"code"`
	TxHash string `json:"txhash"`
	Logs   []struct {
		Events []struct {
			Type       string `json:"type"`
			Attributes []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"attributes"`
		} `json:"events"`
	} `json:"logs"`
	RawLog string `json:"raw_log"`
}

func (g Gov) QueryParams(ctx context.Context) (*GovParamsResponse, error) {
	stdout, stderr, err := g.CLI.RunQuery(ctx, "query", "gov", "params")
	if err != nil {
		return nil, fmt.Errorf("query gov params failed: %w, stderr: %s", err, stderr)
	}

	var resp GovParamsResponse
	if err := MustUnmarshalJSON([]byte(stdout), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (g Gov) QueryProposal(ctx context.Context, id string) (*GovProposalResponse, error) {
	stdout, stderr, err := g.CLI.RunQuery(ctx, "query", "gov", "proposal", id)
	if err != nil {
		return nil, fmt.Errorf("query gov proposal failed: %w, stderr: %s", err, stderr)
	}
	var resp GovProposalResponse
	if err := MustUnmarshalJSON([]byte(stdout), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (g Gov) VoteYes(ctx context.Context, id, from string) error {
	stdout, stderr, err := g.CLI.RunTx(ctx, "tx", "gov", "vote", id, "yes", "--from", from)
	if err != nil {
		return fmt.Errorf("vote yes failed: %w, stderr: %s", err, stderr)
	}
	var txResp TxResponse
	if err := MustUnmarshalJSON([]byte(stdout), &txResp); err == nil && txResp.TxHash != "" {
		if _, _, err := g.CLI.WaitForTx(ctx, txResp.TxHash); err != nil {
			return fmt.Errorf("vote tx not found: %w", err)
		}
	}
	return nil
}

func (g Gov) WaitForStatus(ctx context.Context, id string, wantStatus string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastStatus := ""
	for {
		select {
		case <-waitCtx.Done():
			if lastStatus != "" {
				return fmt.Errorf("timeout waiting for proposal %s status %s (last: %s)", id, wantStatus, lastStatus)
			}
			return fmt.Errorf("timeout waiting for proposal %s status %s", id, wantStatus)
		case <-ticker.C:
			resp, err := g.QueryProposal(waitCtx, id)
			if err != nil {
				continue
			}
			lastStatus = resp.Proposal.Status
			if strings.EqualFold(resp.Proposal.Status, "PROPOSAL_STATUS_FAILED") && !strings.EqualFold(wantStatus, "PROPOSAL_STATUS_FAILED") {
				if resp.Proposal.FailedReason != "" {
					return fmt.Errorf("proposal %s failed: %s", id, resp.Proposal.FailedReason)
				}
				return fmt.Errorf("proposal %s failed", id)
			}
			if strings.EqualFold(resp.Proposal.Status, wantStatus) {
				return nil
			}
		}
	}
}

func (g Gov) SubmitProposalJSON(ctx context.Context, jsonBytes []byte, from string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "gov-proposal-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "proposal.json")
	if err := os.WriteFile(filePath, jsonBytes, 0o600); err != nil {
		return "", err
	}

	stdout, stderr, err := g.CLI.RunTx(ctx, "tx", "gov", "submit-proposal", filePath, "--from", from)
	if err != nil {
		return "", fmt.Errorf("submit proposal failed: %w, stderr: %s", err, stderr)
	}

	var txResp TxResponse
	if err := MustUnmarshalJSON([]byte(stdout), &txResp); err != nil {
		return "", err
	}
	if txResp.Code != 0 {
		return "", fmt.Errorf("submit proposal failed: code=%d raw_log=%s", txResp.Code, txResp.RawLog)
	}

	proposalID := extractProposalID(txResp)
	if proposalID == "" {
		if txResp.TxHash == "" {
			return "", fmt.Errorf("failed to find proposal_id in tx response")
		}
		queryOut, _, err := g.CLI.WaitForTx(ctx, txResp.TxHash)
		if err != nil {
			return "", fmt.Errorf("failed to query tx %s: %w", txResp.TxHash, err)
		}
		proposalID = extractProposalIDFromJSON(queryOut)
		if proposalID == "" {
			return "", fmt.Errorf("failed to find proposal_id in tx response")
		}
	}
	return proposalID, nil
}

func extractProposalID(txResp TxResponse) string {
	for _, log := range txResp.Logs {
		for _, ev := range log.Events {
			if ev.Type != "submit_proposal" {
				continue
			}
			for _, attr := range ev.Attributes {
				if attr.Key == "proposal_id" {
					return attr.Value
				}
			}
		}
	}
	return ""
}

func extractProposalIDFromJSON(raw string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return findEventAttribute(payload, "submit_proposal", "proposal_id")
}

func findEventAttribute(v any, eventType, key string) string {
	switch vv := v.(type) {
	case map[string]any:
		if t, ok := vv["type"].(string); ok && t == eventType {
			if attrs, ok := vv["attributes"].([]any); ok {
				for _, attr := range attrs {
					if attrMap, ok := attr.(map[string]any); ok {
						k, _ := attrMap["key"].(string)
						val, _ := attrMap["value"].(string)
						if k == key && val != "" {
							return val
						}
					}
				}
			}
		}
		// Iterate over map keys in a stable order to avoid non-determinism.
		keys := make([]string, 0, len(vv))
		for k := range vv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if found := findEventAttribute(vv[k], eventType, key); found != "" {
				return found
			}
		}
	case []any:
		for _, val := range vv {
			if found := findEventAttribute(val, eventType, key); found != "" {
				return found
			}
		}
	}
	return ""
}
