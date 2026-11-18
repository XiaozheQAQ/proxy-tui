package singbox

import (
	"strings"
	"testing"
)

const fullConfig = `
{
  "log": {"level": "info"},
  "inbounds": [{"type": "mixed", "tag": "in", "listen": "127.0.0.1", "listen_port": 7890}],
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["ss-1", "direct"]},
    {"type": "shadowsocks", "tag": "ss-1", "server": "1.2.3.4", "server_port": 8388, "method": "aes-128-gcm", "password": "p"},
    {"type": "vmess", "tag": "vm-1", "server": "5.6.7.8", "server_port": 443, "uuid": "u-1",
     "transport": {"type": "ws", "path": "/ws", "headers": {"Host": "cdn.example.com"}},
     "tls": {"enabled": true, "server_name": "cdn.example.com"}},
    {"type": "hysteria2", "tag": "hy-1", "server": "9.9.9.9", "server_port": 8443, "password": "pw",
     "obfs": {"type": "salamander", "password": "x"}, "up_mbps": 50, "down_mbps": 100,
     "tls": {"enabled": true, "insecure": true}},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"rules": [{"ip_is_private": true, "outbound": "direct"}], "final": "proxy"},
  "dns": {"servers": [{"tag": "dns", "address": "1.1.1.1"}]}
}
`

func TestParse(t *testing.T) {
	res, err := Parse([]byte(fullConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (%#v)", len(res.Nodes), res.Unsupported)
	}
	if !res.HasRouting {
		t.Error("expected HasRouting")
	}
	if res.Groups != 2 {
		t.Errorf("groups = %d, want 2", res.Groups)
	}
	vm := res.Nodes[1]
	if vm.Protocol != "vmess" || vm.Network != "ws" || vm.Path != "/ws" {
		t.Errorf("vmess node = %+v", vm)
	}
	if !vm.TLS || vm.SNI != "cdn.example.com" {
		t.Errorf("vmess tls = %+v", vm)
	}
	hy := res.Nodes[2]
	if hy.Obfs != "salamander" || hy.UpMbps != 50 || hy.DownMbps != 100 {
		t.Errorf("hysteria2 node = %+v", hy)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	if _, err := Parse([]byte("{}")); err == nil {
		t.Fatal("expected an error for an empty object")
	}
	if _, err := Parse([]byte(`{"outbounds":[{"type":"direct","tag":"d"}]}`)); err == nil {
		t.Fatal("expected an error when no proxy outbounds exist")
	}
}

func TestParseUnsupportedOutboundOnly(t *testing.T) {
	doc := `{"outbounds":[{"type":"wireguard","tag":"wg","server":"1.2.3.4","server_port":51820},{"type":"direct","tag":"d"}]}`
	if _, err := Parse([]byte(doc)); err == nil {
		t.Fatal("expected an error when every outbound is unsupported")
	} else if !strings.Contains(err.Error(), "wireguard") {
		t.Errorf("error should name the unsupported type: %v", err)
	}
}

func TestParseMixedSupportedAndUnsupported(t *testing.T) {
	doc := `{"outbounds":[{"type":"wireguard","tag":"wg","server":"1.2.3.4","server_port":51820},{"type":"shadowsocks","tag":"ok","server":"5.6.7.8","server_port":8388,"method":"aes-128-gcm","password":"p"}]}`
	res, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Name != "ok" {
		t.Errorf("nodes = %#v", res.Nodes)
	}
	if len(res.Unsupported) != 1 || res.Unsupported[0].Scheme != "wireguard" {
		t.Errorf("unsupported = %#v", res.Unsupported)
	}
}
