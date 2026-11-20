package ipc

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestClientHonoursLongerCallerDeadline(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	go func() {
		defer func() { _ = server.Close() }()
		var request Request
		if err := ReadMessage(server, &request); err != nil {
			return
		}
		time.Sleep(80 * time.Millisecond)
		_ = WriteMessage(server, Response{Version: Version, RequestID: request.RequestID, Success: true})
	}()
	client := NewClient(clientConn, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Call(ctx, OpPing, nil, nil); err != nil {
		t.Fatalf("call with longer caller deadline failed: %v", err)
	}
}

func TestClientHonoursShorterCallerDeadline(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	go func() {
		defer func() { _ = server.Close() }()
		var request Request
		if err := ReadMessage(server, &request); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
		_ = WriteMessage(server, Response{Version: Version, RequestID: request.RequestID, Success: true})
	}()
	client := NewClient(clientConn, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, OpPing, nil, nil); err == nil {
		t.Fatal("call should respect the shorter caller deadline")
	}
}
