package config

import (
	"os"
	"path/filepath"
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
`), 0o644); err != nil {
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
