package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCmd_CreatesConfigIfMissing(t *testing.T) {
	base := t.TempDir()
	withHomeBase(t, base, func() {
		cfgPath := configFilePath()
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("expected config to not exist initially")
		}

		if err := initCmd.RunE(initCmd, nil); err != nil {
			t.Fatalf("init error: %v", err)
		}

		if _, err := os.Stat(cfgPath); err != nil {
			t.Fatalf("expected config to exist: %v", err)
		}
	})
}

func TestInitCmd_WhenConfigExists_Skips(t *testing.T) {
	base := t.TempDir()
	withHomeBase(t, base, func() {
		// Create config directory and config file.
		if err := os.MkdirAll(homeDir(), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		cfgPath := configFilePath()
		if err := os.WriteFile(cfgPath, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		// Should not error; should not overwrite.
		if err := initCmd.RunE(initCmd, nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		b, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(b) != "dummy" {
			t.Fatalf("expected config unchanged, got %q", string(b))
		}
	})
}

func TestHomeDir_IsUnderHomeBase(t *testing.T) {
	base := t.TempDir()
	withHomeBase(t, base, func() {
		got := homeDir()
		want := filepath.Join(base, ".oracled")
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}
