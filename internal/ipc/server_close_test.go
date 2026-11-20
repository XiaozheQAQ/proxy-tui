package ipc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestServerCloseDuringAcceptIsRaceFree(t *testing.T) {
	for i := 0; i < 20; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		server := NewServer(listener, func(_ context.Context, request Request) Response {
			return Response{Version: Version, RequestID: request.RequestID, Success: true}
		})
		done := make(chan struct{})
		go func() {
			_ = server.Serve(context.Background())
			close(done)
		}()
		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, dialErr := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
				if dialErr == nil {
					_ = conn.Close()
				}
			}()
		}
		server.Close()
		wg.Wait()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Serve did not return after Close")
		}
	}
}
