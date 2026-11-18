package mihomo

import (
	"strings"
	"testing"
)

const fullConfig = `
mixed-port: 7890
proxies:
  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
  - {name: B, type: vmess, server: 5.6.7.8, port: 443, uuid: u-1, alterId: 0, tls: true,
     network: ws, ws-opts: {path: /ws, headers: {Host: cdn.example.com}}}
  - {name: C, type: wireguard, server: 9.9.9.9, port: 51820}
proxy-groups:
  - {name: PROXY, type: select, proxies: [A, B]}
rules:
  - GEOIP,LAN,DIRECT,no-resolve
  - MATCH,PROXY
`

func TestParseFullConfig(t *testing.T) {
	res, err := Parse([]byte(fullConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (%#v)", len(res.Nodes), res.Unsupported)
	}
	if !res.HasRouting {
		t.Error("expected HasRouting for a config with rules")
	}
	if res.Groups != 1 || res.Rules != 2 {
		t.Errorf("groups/rules = %d/%d", res.Groups, res.Rules)
	}
	if len(res.Unsupported) != 1 {
		t.Fatalf("unsupported = %d, want 1", len(res.Unsupported))
	}
	if res.Unsupported[0].Scheme != "wireguard" {
		t.Errorf("unsupported scheme = %q", res.Unsupported[0].Scheme)
	}
	if res.Nodes[1].Network != "ws" || res.Nodes[1].Path != "/ws" || res.Nodes[1].Host != "cdn.example.com" {
		t.Errorf("ws node = %+v", res.Nodes[1])
	}
	if !res.Nodes[1].TLS {
		t.Error("vmess node should be TLS")
	}
}

func TestParseNodesOnly(t *testing.T) {
	yamlDoc := "proxies:\n  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}\n"
	res, err := Parse([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.HasRouting {
		t.Error("nodes-only config must not report routing")
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("nodes = %d", len(res.Nodes))
	}
}

func TestParseRejectsUnrelatedYAML(t *testing.T) {
	if _, err := Parse([]byte("foo: bar\n")); err == nil {
		t.Fatal("expected an error for unrelated YAML")
	}
	if _, err := Parse([]byte("")); err == nil {
		t.Fatal("expected an error for empty input")
	}
	if _, err := Parse([]byte("proxies: [\n")); err == nil {
		t.Fatal("expected an error for broken YAML")
	}
}

func TestParseProxyProvidersOnly(t *testing.T) {
	yamlDoc := strings.Join([]string{
		"proxy-providers:",
		"  provider1:",
		"    url: https://example.com/sub",
		"    type: http",
		"rules:",
		"  - MATCH,PROXY",
	}, "\n")
	res, err := Parse([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("nodes = %d, want 0", len(res.Nodes))
	}
	if len(res.Warnings) == 0 {
		t.Error("expected a warning about preserved providers")
	}
}
