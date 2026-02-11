package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type CLI struct {
	Home    string
	Node    string
	ChainID string
}

func (c CLI) Run(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "gurud", args...)
	cmd.Env = append(os.Environ(), "HOME="+c.Home)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func (c CLI) RunTx(ctx context.Context, args ...string) (string, string, error) {
	txArgs := append([]string{}, args...)
	txArgs = append(txArgs,
		"--home", c.Home,
		"--node", c.Node,
		"--chain-id", c.ChainID,
		"--keyring-backend", "test",
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", "100000000000000000agxn",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	return c.Run(ctx, txArgs...)
}

func (c CLI) RunQuery(ctx context.Context, args ...string) (string, string, error) {
	queryArgs := append([]string{}, args...)
	queryArgs = append(queryArgs,
		"--home", c.Home,
		"--node", c.Node,
		"--output", "json",
	)
	return c.Run(ctx, queryArgs...)
}

func (c CLI) WaitForTx(ctx context.Context, txHash string) (string, string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return "", "", fmt.Errorf("timeout waiting for tx %s", txHash)
		case <-ticker.C:
			stdout, stderr, err := c.RunQuery(waitCtx, "query", "tx", txHash)
			if err == nil && stdout != "" {
				return stdout, stderr, nil
			}
		}
	}
}
