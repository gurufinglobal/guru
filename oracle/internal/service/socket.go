package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

func listenPrivateUnix(path string) (net.Listener, error) {
	return listenPrivateUnixWithDial(path, net.DialTimeout)
}

func listenPrivateUnixWithDial(
	path string,
	dial func(network, address string, timeout time.Duration) (net.Conn, error),
) (net.Listener, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("socket path is a symlink")
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("socket path exists and is not a socket")
		}
		connection, dialErr := dial("unix", path, 150*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return nil, errors.New("socket path has a live listener")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("inspect existing socket listener: %w", dialErr)
		}
		currentInfo, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("recheck stale socket: %w", err)
		}
		if currentInfo.Mode()&os.ModeSymlink != 0 || currentInfo.Mode()&os.ModeSocket == 0 ||
			!os.SameFile(info, currentInfo) {
			return nil, errors.New("socket path changed while checking staleness")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return nil, errors.New("unix listener has an unexpected implementation")
	}
	unixListener.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return listener, nil
}

type limitedListener struct {
	net.Listener
	sem chan struct{}
}

type closeOnceListener struct {
	net.Listener
	once sync.Once
	err  error
}

func newCloseOnceListener(listener net.Listener) *closeOnceListener {
	return &closeOnceListener{Listener: listener}
}

func (l *closeOnceListener) Close() error {
	l.once.Do(func() {
		l.err = l.Listener.Close()
	})
	return l.err
}

func limitListener(listener net.Listener, maximum int) net.Listener {
	return &limitedListener{Listener: listener, sem: make(chan struct{}, maximum)}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.sem <- struct{}{}:
			return &limitedConnection{Conn: connection, release: func() { <-l.sem }}, nil
		default:
			_ = connection.Close()
		}
	}
}

type limitedConnection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConnection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
