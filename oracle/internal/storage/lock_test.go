package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAcquireHomeLockRejectsNonRegularAndSymlinkPaths(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()

	fifo := filepath.Join(directory, "lock.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireHomeLock(fifo); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("FIFO lock error = %v", err)
	}

	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "lock.link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireHomeLock(link); err == nil {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatal("symlink lock unexpectedly succeeded")
	}
}
