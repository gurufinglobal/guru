package oracle

import (
	"fmt"
	"os"
	"strings"
)

func SocketPath(socket string) string {
	socket = strings.TrimSpace(socket)
	if strings.HasPrefix(socket, "unix://") {
		return strings.TrimPrefix(socket, "unix://")
	}

	return socket
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket file at %s", path)
	}

	return os.Remove(path)
}
