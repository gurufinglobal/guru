package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFile_NotExists(t *testing.T) {
	t.Parallel()
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadFile_PathIsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := LoadFile(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadFile_ValidationMissingFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(`[chain]
chain_id = ""
endpoint = ""

[keyring]
backend = ""
name = ""

[gas]
limit = 0
adjustment = 1.0
denom = "agxn"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := LoadFile(p)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestWriteDefaultFile_CreatesDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	path := filepath.Join(base, "nested", "config.toml")
	if err := WriteDefaultFile(path); err != nil {
		t.Fatalf("WriteDefaultFile error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestLoadFile_CMCAPIKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(`[chain]
chain_id = "guru_631-1"
endpoint = "http://localhost:26657"

[keyring]
backend = "test"
name = "oracle_feeder"
passphrase = "password"

[gas]
limit = 70000
adjustment = 1.5
denom = "agxn"

cmc_api_key = "test-cmc-key"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(p)
	if err != nil {
		t.Fatalf("LoadFile error: %v", err)
	}
	if cfg.CMCAPIKey != "test-cmc-key" {
		t.Fatalf("expected CMC API key to be loaded, got %q", cfg.CMCAPIKey)
	}
}

func TestWriteDefaultFile_ContainsCMCAPIKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteDefaultFile(path); err != nil {
		t.Fatalf("WriteDefaultFile error: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(b), `cmc_api_key = ""`) {
		t.Fatalf("expected default config to include cmc_api_key")
	}
	if !strings.HasSuffix(strings.TrimSpace(string(b)), `cmc_api_key = ""`) {
		t.Fatalf("expected cmc_api_key to be the last config line")
	}
}

func TestDefaultConfig_CMCAPIKeyEmpty(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.CMCAPIKey != "" {
		t.Fatalf("expected empty CMC API key by default, got %q", cfg.CMCAPIKey)
	}
}
