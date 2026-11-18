package clashapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientAgainstFakeAPI(t *testing.T) {
	selected := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.19.0", "meta": true})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{
				"PROXY":  map[string]any{"type": "Selector", "now": "NodeA", "all": []string{"NodeA", "NodeB", "DIRECT"}},
				"NodeA":  map[string]any{"type": "Shadowsocks", "history": []map[string]any{{"delay": 42}}, "alive": true},
				"NodeB":  map[string]any{"type": "Trojan", "history": []any{}},
				"DIRECT": map[string]any{"type": "Direct"},
			}})
		case r.URL.Path == "/proxies/PROXY" && r.Method == http.MethodPut:
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			selected = payload["name"]
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/delay"):
			if r.URL.Query().Get("url") == "" || r.URL.Query().Get("timeout") == "" {
				t.Errorf("delay request is missing query parameters: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"delay": 77})
		case r.URL.Path == "/connections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"downloadTotal": 2048,
				"uploadTotal":   1024,
				"connections": []map[string]any{{
					"id": "c1", "upload": 10, "download": 20, "start": "2026-11-17T10:00:00Z",
					"rule": "MATCH", "chains": []string{"PROXY", "NodeA"},
					"metadata": map[string]any{
						"host": "example.com", "destinationIP": "1.1.1.1",
						"network": "tcp", "type": "HTTP",
					},
				}},
			})
		case r.URL.Path == "/traffic":
			flusher, _ := w.(http.Flusher)
			_, _ = io.WriteString(w, "{\"up\":100,\"down\":200}\n")
			flusher.Flush()
			_, _ = io.WriteString(w, "{\"up\":150,\"down\":300}\n")
			flusher.Flush()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(strings.TrimPrefix(server.URL, "http://"), "secret")
	ctx := context.Background()

	version, err := client.Version(ctx)
	if err != nil || version != "1.19.0" {
		t.Fatalf("Version = %q, %v", version, err)
	}

	proxies, err := client.Proxies(ctx)
	if err != nil {
		t.Fatalf("Proxies: %v", err)
	}
	if len(proxies) != 4 {
		t.Fatalf("proxies = %d, want 4", len(proxies))
	}
	var group string
	groupFound, nodeFound := false, false
	for _, proxy := range proxies {
		switch proxy.Name {
		case "PROXY":
			group = proxy.Now
			groupFound = proxy.Group && proxy.Now != ""
		case "NodeA":
			nodeFound = proxy.Delay == 42 && !proxy.Group
		}
	}
	if !groupFound || group != "NodeA" {
		t.Errorf("group parsing failed: %+v", proxies)
	}
	if !nodeFound {
		t.Errorf("node delay parsing failed: %+v", proxies)
	}

	if err := client.Select(ctx, "PROXY", "NodeB"); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selected != "NodeB" {
		t.Errorf("selected = %q", selected)
	}

	delay, err := client.TestDelay(ctx, "NodeA", 2*time.Second)
	if err != nil || delay != 77 {
		t.Fatalf("TestDelay = %d, %v", delay, err)
	}

	connections, traffic, err := client.Connections(ctx)
	if err != nil {
		t.Fatalf("Connections: %v", err)
	}
	if len(connections) != 1 || connections[0].Host != "example.com" {
		t.Fatalf("connections = %+v", connections)
	}
	if connections[0].Type != "HTTP" || connections[0].Upload != 10 || connections[0].Download != 20 {
		t.Errorf("connection fields = %+v", connections[0])
	}
	if traffic.UpTotal != 1024 || traffic.DownTotal != 2048 {
		t.Errorf("traffic totals = %+v", traffic)
	}

	body, err := client.OpenTraffic(ctx)
	if err != nil {
		t.Fatalf("OpenTraffic: %v", err)
	}
	var samples []int64
	err = ReadTraffic(body, func(up, down int64) { samples = append(samples, up+down) })
	_ = body.Close()
	if err != nil && err != io.EOF {
		t.Fatalf("ReadTraffic: %v", err)
	}
	if len(samples) != 2 || samples[0] != 300 || samples[1] != 450 {
		t.Errorf("traffic samples = %v", samples)
	}
}

func TestClientRejectsBadSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer server.Close()
	client := New(strings.TrimPrefix(server.URL, "http://"), "wrong")
	if _, err := client.Version(context.Background()); err == nil {
		t.Fatal("expected an error for a rejected secret")
	}
}
