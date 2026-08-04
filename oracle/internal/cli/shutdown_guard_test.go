package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

func TestShutdownGuardHardExitsAtDeadline(t *testing.T) {
	t.Parallel()
	var signalsStopped atomic.Bool
	exited := make(chan int, 1)
	guard := newShutdownGuard(20*time.Millisecond, func() {
		signalsStopped.Store(true)
	}, func(code int) {
		exited <- code
	})
	guard.Start()

	select {
	case code := <-exited:
		if code != 1 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown guard did not hard exit")
	}
	if !signalsStopped.Load() {
		t.Fatal("signal defaults were not restored")
	}
	guard.Complete()
}

func TestShutdownGuardCompletionCancelsHardExit(t *testing.T) {
	t.Parallel()
	exited := make(chan int, 1)
	guard := newShutdownGuard(20*time.Millisecond, func() {}, func(code int) {
		exited <- code
	})
	guard.Start()
	guard.Complete()

	select {
	case code := <-exited:
		t.Fatalf("unexpected hard exit %d", code)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestShutdownGuardCompletionBeforeStartIsSafe(t *testing.T) {
	t.Parallel()
	var signalsStopped atomic.Bool
	exited := make(chan int, 1)
	guard := newShutdownGuard(time.Millisecond, func() {
		signalsStopped.Store(true)
	}, func(code int) {
		exited <- code
	})
	guard.Complete()
	guard.Start()

	if signalsStopped.Load() {
		t.Fatal("completed guard changed signal handling")
	}
	select {
	case code := <-exited:
		t.Fatalf("unexpected hard exit %d", code)
	case <-time.After(10 * time.Millisecond):
	}
}

func TestShutdownGuardProcessExitReleasesHomeLock(t *testing.T) {
	if os.Getenv("GURU_ORACLE_SHUTDOWN_GUARD_HELPER") == "1" {
		lock, err := storage.AcquireHomeLock(os.Getenv("GURU_ORACLE_SHUTDOWN_GUARD_LOCK"))
		if err != nil {
			os.Exit(2)
		}
		// Keep the lock held until os.Exit; process cleanup is the behavior under test.
		defer func() { _ = lock.Close() }()
		guard := newShutdownGuard(20*time.Millisecond, func() {}, os.Exit)
		guard.Start()
		select {}
	}

	lockPath := filepath.Join(t.TempDir(), "oracled.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownGuardProcessExitReleasesHomeLock$")
	command.Env = append(
		os.Environ(),
		"GURU_ORACLE_SHUTDOWN_GUARD_HELPER=1",
		"GURU_ORACLE_SHUTDOWN_GUARD_LOCK="+lockPath,
	)
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("helper exit error = %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("helper exceeded hard deadline: %v", ctx.Err())
	}

	lock, err := storage.AcquireHomeLock(lockPath)
	if err != nil {
		t.Fatalf("home lock was not released by process exit: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
