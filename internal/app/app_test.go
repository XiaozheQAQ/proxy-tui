package app

import (
	"net"
	"testing"
	"time"
)

func TestProxyListening(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := proxyListening(addr, 500*time.Millisecond); err != nil {
		t.Fatalf("open listener not detected: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := proxyListening(addr, 200*time.Millisecond); err == nil {
		t.Fatal("closed listener must not be reported as reachable")
	}
}
