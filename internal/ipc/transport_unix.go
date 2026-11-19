//go:build !windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EndpointPath returns the local socket path inside the runtime directory.
func EndpointPath(runtimeDir string) (string, error) {
	return filepath.Join(runtimeDir, "supervisor.sock"), nil
}

// Listen binds the local IPC endpoint. It refuses to replace a live socket.
func Listen(endpoint string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, fmt.Errorf("create IPC directory: %w", err)
	}
	if conn, err := net.DialTimeout("unix", endpoint, 300*time.Millisecond); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("supervisor is already listening at %s", endpoint)
	}
	if err := os.Remove(endpoint); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
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

// DialConn connects to the supervisor endpoint.
func DialConn(ctx context.Context, endpoint string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return dialer.DialContext(ctx, "unix", endpoint)
}
