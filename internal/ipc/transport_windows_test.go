//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testPipeSequence atomic.Uint64

func testPipeName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf(`\\.\pipe\proxy-tui-test-%d-%d`, os.Getpid(), testPipeSequence.Add(1))
	return name
}

func echoHandler(_ context.Context, request Request) Response {
	return Response{Version: Version, RequestID: request.RequestID, Success: true}
}

func startPipeServer(t *testing.T, name string) (net.Listener, context.CancelFunc, chan error) {
	t.Helper()
	listener, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(listener, echoHandler)
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		server.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return listener, cancel, done
}

func dialClient(t *testing.T, name string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialConn(ctx, name)
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	return NewClient(conn, 5*time.Second)
}

func TestWindowsPipeRoundTrip(t *testing.T) {
	name := testPipeName(t)
	startPipeServer(t, name)
	client := dialClient(t, name)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Call(ctx, OpPing, nil, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestWindowsPipe32Clients(t *testing.T) {
	name := testPipeName(t)
	startPipeServer(t, name)
	const clients = 32
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := DialConn(context.Background(), name)
			if err != nil {
				errs <- err
				return
			}
			client := NewClient(conn, 5*time.Second)
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
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

func TestWindowsPipeRepeatedStartStop(t *testing.T) {
	name := testPipeName(t)
	for i := 0; i < 100; i++ {
		listener, err := Listen(name)
		if err != nil {
			t.Fatalf("iteration %d: Listen: %v", i, err)
		}
		if err := listener.Close(); err != nil {
			t.Fatalf("iteration %d: Close: %v", i, err)
		}
	}
}

func TestWindowsPipeCloseUnblocksAccept(t *testing.T) {
	name := testPipeName(t)
	listener, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	accepted := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		accepted <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if err := listener.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-accepted:
		if err == nil {
			t.Fatal("Accept should return an error after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after Close")
	}
}

func TestWindowsPipePartialFrameAndReconnect(t *testing.T) {
	name := testPipeName(t)
	startPipeServer(t, name)
	conn, err := DialConn(context.Background(), name)
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	_, _ = conn.Write([]byte{0, 0})
	_ = conn.Close()
	client := dialClient(t, name)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Call(ctx, OpPing, nil, nil); err != nil {
		t.Fatalf("call after partial frame: %v", err)
	}
}

func TestWindowsPipeServerCloseErrorsClients(t *testing.T) {
	name := testPipeName(t)
	listener, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(listener, echoHandler)
	go func() { _ = server.Serve(ctx) }()
	client := dialClient(t, name)
	defer client.Close()
	server.Close()
	cancel()
	callCtx, callCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer callCancel()
	if err := client.Call(callCtx, OpPing, nil, nil); err == nil {
		t.Fatal("client call should fail after server close")
	}
}

func TestWindowsPipeNameConflict(t *testing.T) {
	name := testPipeName(t)
	first, err := Listen(name)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer func() { _ = first.Close() }()
	if second, err := Listen(name); err == nil {
		_ = second.Close()
		t.Fatal("second Listen must fail while the pipe exists")
	}
}

func TestWindowsPipeDACL(t *testing.T) {
	sddl, err := securityDescriptor()
	if err != nil {
		t.Fatalf("securityDescriptor: %v", err)
	}
	sid, err := UserSID()
	if err != nil {
		t.Fatalf("UserSID: %v", err)
	}
	if !strings.Contains(sddl, sid) {
		t.Fatalf("DACL does not contain the logon SID: %s", sddl)
	}
	for _, broad := range []string{"S-1-1-0", "S-1-5-11", "S-1-5-18", "S-1-5-32-544"} {
		if strings.Contains(sddl, broad) {
			t.Fatalf("DACL contains broad principal %s: %s", broad, sddl)
		}
	}
	if !strings.HasPrefix(sddl, "D:P") {
		t.Fatalf("DACL is not protected: %s", sddl)
	}
}

func TestWindowsEndpointIsolatesRuntimeDirectories(t *testing.T) {
	t.Setenv("PROXY_TUI_HOME", `C:\one`)
	a, err := EndpointPath(`C:\one\runtime`)
	if err != nil {
		t.Fatalf("endpoint A: %v", err)
	}
	t.Setenv("PROXY_TUI_HOME", `C:\two`)
	b, err := EndpointPath(`C:\two\runtime`)
	if err != nil {
		t.Fatalf("endpoint B: %v", err)
	}
	if a == b {
		t.Fatal("different runtime directories share one pipe")
	}
	sid, err := UserSID()
	if err != nil {
		t.Fatalf("sid: %v", err)
	}
	if !strings.Contains(a, strings.ReplaceAll(sid, "\\", "-")) {
		t.Fatalf("endpoint %q does not contain the user SID", a)
	}
}
