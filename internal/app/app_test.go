package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestProbeThroughProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ip":"203.0.113.7"}`)
	}))
	defer proxy.Close()
	addr := strings.TrimPrefix(proxy.URL, "http://")
	ip, latency, err := probeThroughProxy(context.Background(), addr, "http://example.test/ip")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if ip != "203.0.113.7" {
		t.Fatalf("ip = %q", ip)
	}
	if latency <= 0 {
		t.Fatalf("latency = %v", latency)
	}
}

func TestProbeThroughProxyClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	if _, _, err := probeThroughProxy(context.Background(), addr, "http://example.test/ip"); err == nil {
		t.Fatal("closed proxy port must fail")
	}
}
