package app

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/ipc"
)

func TestWaitOperationPollsUntilSucceeded(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	go func() {
		defer func() { _ = server.Close() }()
		for i := 0; i < 2; i++ {
			var request ipc.Request
			if err := ipc.ReadMessage(server, &request); err != nil {
				return
			}
			status := map[string]any{"operation_id": "op", "status": "RUNNING"}
			if i == 1 {
				status["status"] = "SUCCEEDED"
				status["payload"] = map[string]any{"core": "mihomo"}
			}
			response := ipc.Response{Version: ipc.Version, RequestID: request.RequestID, Success: true, Payload: json.RawMessage(mustJSON(status))}
			_ = ipc.WriteMessage(server, response)
		}
	}()
	client := ipc.NewClient(clientConn, time.Second)
	r := &remote{client: client}
	var out struct {
		Core string `json:"core"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.waitOperation(ctx, "op", &out); err != nil {
		t.Fatalf("waitOperation: %v", err)
	}
	if out.Core != "mihomo" {
		t.Fatalf("out = %+v", out)
	}
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func TestWaitOperationReportsStillRunningOnDeadline(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	go func() {
		defer func() { _ = server.Close() }()
		for {
			var request ipc.Request
			if err := ipc.ReadMessage(server, &request); err != nil {
				return
			}
			status := map[string]any{"operation_id": "op", "status": "RUNNING"}
			response := ipc.Response{Version: ipc.Version, RequestID: request.RequestID, Success: true, Payload: json.RawMessage(mustJSON(status))}
			if err := ipc.WriteMessage(server, response); err != nil {
				return
			}
		}
	}()
	client := ipc.NewClient(clientConn, time.Second)
	r := &remote{client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := r.waitOperation(ctx, "op", nil)
	rpcErr, ok := err.(*ipc.RPCError)
	if !ok || rpcErr.Code != ipc.CodeOperationInProgress {
		t.Fatalf("error = %v, want OPERATION_IN_PROGRESS", err)
	}
}
