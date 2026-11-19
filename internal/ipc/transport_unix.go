//go:build !windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// EndpointPath returns the local socket path inside the runtime directory.
func EndpointPath(runtimeDir string) (string, error) {
	return filepath.Join(runtimeDir, "supervisor.sock"), nil
}

// Listen binds the local IPC endpoint. It refuses to replace a live socket.
func Listen(endpoint string) (net.Listener, error) {
	dir := filepath.Dir(endpoint)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create IPC directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict IPC directory: %w", err)
	}
	if err := checkDirOwner(dir); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(endpoint); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("IPC endpoint is a symlink; refusing to replace it")
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("IPC endpoint exists and is not a socket; refusing to replace it")
		}
		if owner, ok := fileOwner(info); !ok || owner != os.Geteuid() {
			return nil, fmt.Errorf("IPC endpoint is not owned by the current user")
		}
		if conn, dialErr := net.DialTimeout("unix", endpoint, 300*time.Millisecond); dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("supervisor is already listening at %s", endpoint)
		}
		if err := os.Remove(endpoint); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect IPC endpoint: %w", err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, fmt.Errorf("bind supervisor socket: %w", err)
	}
	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("restrict supervisor socket: %w", err)
	}
	return listener, nil
}

func fileOwner(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

func checkDirOwner(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspect IPC directory: %w", err)
	}
	if owner, ok := fileOwner(info); !ok || owner != os.Geteuid() {
		return fmt.Errorf("IPC directory is not owned by the current user")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("IPC directory permissions are %o, want 0700", info.Mode().Perm())
	}
	return nil
}

// DialConn connects to the supervisor endpoint.
func DialConn(ctx context.Context, endpoint string) (net.Conn, error) {
	info, err := os.Lstat(endpoint)
	if err != nil {
		return nil, fmt.Errorf("inspect supervisor socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("supervisor endpoint is not a real socket")
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("supervisor socket permissions are %o, want 0600", info.Mode().Perm())
	}
	if owner, ok := fileOwner(info); !ok || owner != os.Geteuid() {
		return nil, fmt.Errorf("supervisor socket is not owned by the current user")
	}
	if err := checkDirOwner(filepath.Dir(endpoint)); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err := verifyPeer(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("verify supervisor peer: %w", err)
	}
	return conn, nil
}
