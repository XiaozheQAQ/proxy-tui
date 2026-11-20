//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func unixTestServer(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	endpoint := filepath.Join(dir, "supervisor.sock")
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(listener, func(_ context.Context, request Request) Response {
		return Response{Version: Version, RequestID: request.RequestID, Success: true}
	})
	done := make(chan struct{})
	go func() {
		_ = server.Serve(ctx)
		close(done)
	}()
	cleanup := func() {
		cancel()
		server.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	}
	return endpoint, cleanup
}

func TestUnixPeerRoundTrip(t *testing.T) {
	endpoint, cleanup := unixTestServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialConn(ctx, endpoint)
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	client := NewClient(conn, 5*time.Second)
	defer client.Close()
	if err := client.Call(ctx, OpPing, nil, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestUnixPeer32Clients(t *testing.T) {
	endpoint, cleanup := unixTestServer(t)
	defer cleanup()
	const clients = 32
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := DialConn(ctx, endpoint)
			if err != nil {
				errs <- err
				return
			}
			client := NewClient(conn, 5*time.Second)
			defer client.Close()
			errs <- client.Call(ctx, OpPing, nil, nil)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("client: %v", err)
		}
	}
}

func TestUnixEndpointPermissions(t *testing.T) {
	endpoint, cleanup := unixTestServer(t)
	defer cleanup()
	info, err := os.Stat(endpoint)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(endpoint))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
	if err := os.Chmod(endpoint, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := DialConn(ctx, endpoint); err == nil {
		_ = conn.Close()
		t.Fatal("DialConn accepted a world-readable socket")
	}
}

func TestUnixEndpointSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	endpoint := filepath.Join(dir, "supervisor.sock")
	target := filepath.Join(dir, "target.sock")
	if err := os.Symlink(target, endpoint); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if listener, err := Listen(endpoint); err == nil {
		_ = listener.Close()
		t.Fatal("Listen accepted a symlink endpoint")
	}
}

func TestUnixStaleSocketRecovery(t *testing.T) {
	dir := t.TempDir()
	endpoint := filepath.Join(dir, "supervisor.sock")
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(endpoint); err == nil {
		t.Log("socket file remains after close (platform-specific)")
	}
	listener2, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen after stale socket: %v", err)
	}
	defer func() { _ = listener2.Close() }()
}

func TestUnixPeerRejectsNonUnixConnection(t *testing.T) {
	left, right := net.Pipe()
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()
	if err := verifyPeer(left); err == nil {
		t.Fatal("verifyPeer accepted a non-Unix connection")
	}
}

var _ = fmt.Sprintf
