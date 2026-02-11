package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Node struct {
	Home   string
	RPCURL string
	cmd    *exec.Cmd
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func StartLocalNode(ctx context.Context, repoRoot, homeDir string) (*Node, error) {
	scriptPath := filepath.Join(repoRoot, "local_node.sh")

	cmd := exec.CommandContext(ctx, "bash", scriptPath, "-y", "--no-install")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	node := &Node{
		Home:   filepath.Join(homeDir, ".gurud"),
		RPCURL: "http://127.0.0.1:26657",
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start local node: %w", err)
	}

	if err := waitForRPC(ctx, node.RPCURL); err != nil {
		_ = node.Stop()
		return nil, err
	}
	if err := waitForFirstBlock(ctx, node.RPCURL); err != nil {
		_ = node.Stop()
		return nil, err
	}

	return node, nil
}

func (n *Node) Stop() error {
	if n == nil || n.cmd == nil || n.cmd.Process == nil {
		return nil
	}

	if err := n.cmd.Process.Signal(os.Interrupt); err != nil {
		_ = n.cmd.Process.Kill()
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- n.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		_ = n.cmd.Process.Kill()
		return fmt.Errorf("forced to kill node process")
	}
}

func waitForRPC(ctx context.Context, rpcURL string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("rpc not ready: %w", waitCtx.Err())
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(waitCtx, http.MethodGet, rpcURL+"/health", nil)
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

func waitForFirstBlock(ctx context.Context, rpcURL string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("first block not produced: %w", waitCtx.Err())
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(waitCtx, http.MethodGet, rpcURL+"/status", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil || resp == nil {
				continue
			}
			var body struct {
				Result struct {
					SyncInfo struct {
						LatestBlockHeight string `json:"latest_block_height"`
					} `json:"sync_info"`
				} `json:"result"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if body.Result.SyncInfo.LatestBlockHeight != "" && body.Result.SyncInfo.LatestBlockHeight != "0" {
				return nil
			}
		}
	}
}
