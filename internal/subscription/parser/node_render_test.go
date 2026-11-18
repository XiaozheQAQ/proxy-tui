package parser

import (
	"strings"
	"testing"
)

func TestNodeToMihomo(t *testing.T) {
	node := Node{
		Name:             "HK",
		Protocol:         "vless",
		Server:           "1.2.3.4",
		Port:             443,
		UUID:             "uuid",
		TLS:              true,
		SNI:              "www.example.com",
		Network:          "ws",
		Path:             "/ws",
		Host:             "cdn.example.com",
		Fingerprint:      "chrome",
		RealityPublicKey: "PUB",
		RealityShortID:   "sid",
	}
	proxy, err := node.ToMihomo()
	if err != nil {
		t.Fatalf("ToMihomo: %v", err)
	}
	if proxy["type"] != "vless" || proxy["uuid"] != "uuid" {
		t.Errorf("proxy = %#v", proxy)
	}
	if proxy["servername"] != "www.example.com" {
		t.Errorf("servername = %v", proxy["servername"])
	}
	ws, ok := proxy["ws-opts"].(map[string]any)
	if !ok {
		t.Fatalf("ws-opts missing: %#v", proxy)
	}
	if ws["path"] != "/ws" {
		t.Errorf("ws path = %v", ws["path"])
	}
	reality, ok := proxy["reality-opts"].(map[string]any)
	if !ok || reality["public-key"] != "PUB" || reality["short-id"] != "sid" {
		t.Errorf("reality-opts = %#v", proxy["reality-opts"])
	}
}

func TestNodeToSingBox(t *testing.T) {
	node := Node{
		Name:         "HY2",
		Protocol:     "hysteria2",
		Server:       "1.2.3.4",
		Port:         8443,
		Password:     "pw",
		TLS:          true,
		SNI:          "example.com",
		Insecure:     true,
		Obfs:         "salamander",
		ObfsPassword: "xyz",
		UpMbps:       50,
		DownMbps:     100,
	}
	out, err := node.ToSingBoxOutbound()
	if err != nil {
		t.Fatalf("ToSingBoxOutbound: %v", err)
	}
	if out["type"] != "hysteria2" || out["tag"] != "HY2" {
		t.Errorf("out = %#v", out)
	}
	if out["password"] != "pw" || out["up_mbps"] != 50 || out["down_mbps"] != 100 {
		t.Errorf("out = %#v", out)
	}
	tls, ok := out["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["insecure"] != true {
		t.Errorf("tls = %#v", out["tls"])
	}
	obfs, ok := out["obfs"].(map[string]any)
	if !ok || obfs["type"] != "salamander" {
		t.Errorf("obfs = %#v", out["obfs"])
	}
}

func TestNodeRejectsUnknownProtocol(t *testing.T) {
	node := Node{Name: "wg", Protocol: "wireguard", Server: "1.2.3.4", Port: 51820}
	if _, err := node.ToMihomo(); err == nil {
		t.Fatal("expected an error for wireguard")
	}
	if _, err := node.ToSingBoxOutbound(); err == nil {
		t.Fatal("expected an error for wireguard")
	}
}

func TestEnsureUniqueNames(t *testing.T) {
	nodes := []Node{{Name: "A"}, {Name: "A"}, {Name: ""}, {Name: "A"}}
	EnsureUniqueNames(nodes)
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Name == "" {
			t.Fatal("empty name survived")
		}
		if seen[n.Name] {
			t.Fatalf("duplicate name %q", n.Name)
		}
		seen[n.Name] = true
	}
}

func TestSanitizeName(t *testing.T) {
	got := SanitizeName("  hello\nworld\t  ")
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("control characters survived: %q", got)
	}
}
