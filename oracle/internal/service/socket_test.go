package service

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestListenPrivateUnixPreservesLiveSocket(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "live.sock")
	live, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, live)

	if _, err := listenPrivateUnix(path); err == nil {
		t.Fatal("second listener unexpectedly replaced the live socket")
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("live socket path was removed: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("live socket path mode = %v", info.Mode())
	}

	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("original listener is no longer reachable: %v", err)
	}
	_ = connection.Close()
}

func TestListenPrivateUnixFailsClosedOnNonRefusedDialError(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "datagram.sock")
	packet, err := net.ListenPacket("unixgram", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, packet)

	listener, err := listenPrivateUnix(path)
	if listener != nil {
		_ = listener.Close()
		t.Fatal("stream listener replaced a non-stream Unix socket")
	}
	if err == nil {
		t.Fatal("expected existing Unix datagram socket to be rejected")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected socket inspection error: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("existing Unix socket path was removed: %v", err)
	}
}

func TestRemoveOwnedSocketPreservesReplacement(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "replacement.sock")
	first, err := listenPrivateUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, replacement)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	if err := removeOwnedSocket(path, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
}

func TestRemoveOwnedSocketReturnsInspectionError(t *testing.T) {
	t.Parallel()
	if err := removeOwnedSocket("\x00", nil); err == nil {
		t.Fatal("invalid socket path inspection succeeded")
	}
}

func TestListenPrivateUnixReplacesUnchangedStaleSocket(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "stale.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	unixStale, ok := stale.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener type = %T", stale)
	}
	unixStale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	replacement, err := listenPrivateUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, replacement)
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("replacement listener is not reachable: %v", err)
	}
	_ = connection.Close()
}

func TestListenPrivateUnixPreservesSocketSwappedAfterRefusedDial(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "swapped.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	unixStale, ok := stale.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener type = %T", stale)
	}
	unixStale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	var replacement net.Listener
	listener, err := listenPrivateUnixWithDial(path, func(_, _ string, _ time.Duration) (net.Conn, error) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		replacement, err = net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		return nil, syscall.ECONNREFUSED
	})
	if listener != nil {
		_ = listener.Close()
		t.Fatal("listener unexpectedly replaced swapped socket")
	}
	if err == nil {
		t.Fatal("socket inode swap was accepted")
	}
	if replacement == nil {
		t.Fatal("replacement listener was not created")
	}
	defer closeTestResource(t, replacement)
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket path was removed: %v", err)
	}
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("replacement listener is no longer reachable: %v", err)
	}
	_ = connection.Close()
}

func TestLimitListenerBoundsAcceptedConnections(t *testing.T) {
	t.Parallel()
	path := shortSocketPath(t, "limited.sock")
	base, err := listenPrivateUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, base)
	listener := limitListener(base, 1)

	firstClient, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, firstClient)
	firstServer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}

	nextAccepted := make(chan net.Conn, 1)
	acceptErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			acceptErrors <- err
			return
		}
		nextAccepted <- connection
	}()
	secondClient, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, secondClient)
	if err := secondClient.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if _, err := secondClient.Read(buffer); err == nil {
		t.Fatal("connection above the limit remained open")
	}

	if err := firstServer.Close(); err != nil {
		t.Fatal(err)
	}
	thirdClient, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, thirdClient)
	select {
	case connection := <-nextAccepted:
		_ = connection.Close()
	case err := <-acceptErrors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("listener did not admit a connection after capacity was released")
	}
}

func TestCloseOnceListenerClosesUnderlyingListenerOnce(t *testing.T) {
	t.Parallel()
	closeErr := errors.New("close failed")
	underlying := &countingListener{closeErr: closeErr}
	listener := newCloseOnceListener(underlying)

	const callers = 32
	errorsSeen := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsSeen <- listener.Close()
		}()
	}
	group.Wait()
	close(errorsSeen)

	for err := range errorsSeen {
		if !errors.Is(err, closeErr) {
			t.Fatalf("Close error = %v, want %v", err, closeErr)
		}
	}
	if calls := underlying.closeCalls.Load(); calls != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", calls)
	}
}

type countingListener struct {
	closeCalls atomic.Int32
	closeErr   error
}

func (l *countingListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l *countingListener) Close() error {
	l.closeCalls.Add(1)
	return l.closeErr
}

func (l *countingListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "test", Net: "unix"}
}

func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(tempRoot, "oracled-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return filepath.Join(directory, name)
}

func closeTestResource(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}
