package tun

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

const mihomoProfile = `
mixed-port: 7890
dns:
  enable: true
  nameserver:
    - 223.5.5.5
proxies:
  - {name: Tokyo, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
proxy-groups:
  - {name: PROXY, type: select, proxies: [Tokyo, DIRECT]}
rules:
  - GEOIP,LAN,DIRECT,no-resolve
  - MATCH,PROXY
`

func TestRenderMihomoPreservesRouting(t *testing.T) {
	settings := Defaults()
	result, err := Render(model.CoreMihomo, []byte(mihomoProfile), settings, "1.19.34")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(result.Config, &doc); err != nil {
		t.Fatalf("rendered yaml: %v", err)
	}
	tunBlock := asMap(doc["tun"])
	if tunBlock == nil {
		t.Fatal("no tun block rendered")
	}
	if tunBlock["enable"] != true {
		t.Errorf("tun.enable = %v", tunBlock["enable"])
	}
	if tunBlock["stack"] != "mixed" {
		t.Errorf("tun.stack = %v", tunBlock["stack"])
	}
	if tunBlock["auto-route"] != true || tunBlock["auto-detect-interface"] != true || tunBlock["strict-route"] != true {
		t.Errorf("routing flags = %+v", tunBlock)
	}
	if len(asSlice(tunBlock["dns-hijack"])) != 2 {
		t.Errorf("dns-hijack = %v", tunBlock["dns-hijack"])
	}
	if len(asSlice(doc["proxies"])) != 1 || len(asSlice(doc["proxy-groups"])) != 1 || len(asSlice(doc["rules"])) != 2 {
		t.Errorf("routing was not preserved")
	}
	dns := asMap(doc["dns"])
	if dns["enable"] != true || len(asSlice(dns["nameserver"])) != 1 {
		t.Errorf("dns section was not preserved: %+v", dns)
	}
	if len(result.Overrides) != 0 {
		t.Errorf("unexpected overrides: %v", result.Overrides)
	}
	if !strings.Contains(string([]byte(mihomoProfile)), "mixed-port: 7890") {
		t.Fatal("input constant changed")
	}
}

func TestRenderMihomoOverrideExistingTUN(t *testing.T) {
	profile := strings.Replace(mihomoProfile, "mixed-port: 7890", `
mixed-port: 7890
tun:
  enable: true
  stack: gvisor
  mtu: 1500
  include-uid: [1000]
`, 1)
	settings := Defaults()
	settings.Stack = StackSystem
	result, err := Render(model.CoreMihomo, []byte(profile), settings, "1.19.34")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	doc := map[string]any{}
	_ = yaml.Unmarshal(result.Config, &doc)
	tunBlock := asMap(doc["tun"])
	if tunBlock["stack"] != "system" {
		t.Errorf("stack = %v", tunBlock["stack"])
	}
	if tunBlock["mtu"] != 9000 {
		t.Errorf("mtu = %v", tunBlock["mtu"])
	}
	if len(asSlice(tunBlock["include-uid"])) != 1 {
		t.Errorf("unknown tun fields must be preserved: %+v", tunBlock)
	}
	if len(result.Overrides) < 2 {
		t.Errorf("overrides = %v", result.Overrides)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about overriding an existing TUN block")
	}
}

func TestRenderMihomoDNSDisabledWarning(t *testing.T) {
	profile := strings.Replace(mihomoProfile, "dns:\n  enable: true", "dns:\n  enable: false", 1)
	result, err := Render(model.CoreMihomo, []byte(profile), Defaults(), "1.19.34")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a DNS warning")
	}
}

func TestRenderMihomoRejects(t *testing.T) {
	if _, err := Render(model.CoreMihomo, []byte("proxies: ["), Defaults(), "1.19.34"); err == nil {
		t.Error("expected an error for invalid YAML")
	}
	if _, err := Render(model.CoreMihomo, []byte("mode: rule\n"), Defaults(), "1.19.34"); err == nil {
		t.Error("expected an error for a config without proxies")
	}
}

const singBoxProfile = `
{
  "log": {"level": "info"},
  "dns": {
    "servers": [{"tag": "dns", "address": "1.1.1.1"}],
    "final": "dns"
  },
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}],
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["ss-1", "direct"]},
    {"type": "shadowsocks", "tag": "ss-1", "server": "1.2.3.4", "server_port": 8388, "method": "aes-128-gcm", "password": "p"},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {
    "rules": [{"ip_is_private": true, "outbound": "direct"}],
    "final": "proxy"
  }
}
`

func TestRenderSingBoxPreservesRouting(t *testing.T) {
	result, err := Render(model.CoreSingBox, []byte(singBoxProfile), Defaults(), "1.14.4")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(result.Config, &doc); err != nil {
		t.Fatalf("rendered json: %v", err)
	}
	if len(asSlice(doc["outbounds"])) != 3 {
		t.Errorf("outbounds changed: %d", len(asSlice(doc["outbounds"])))
	}
	dns := asMap(doc["dns"])
	if dns == nil || len(asSlice(dns["servers"])) != 1 {
		t.Errorf("dns changed: %+v", dns)
	}
	inbounds := asSlice(doc["inbounds"])
	if len(inbounds) != 2 {
		t.Fatalf("inbounds = %d, want 2", len(inbounds))
	}
	tunInbound := asMap(inbounds[1])
	if tunInbound["type"] != "tun" || tunInbound["tag"] != "tun-in" {
		t.Errorf("tun inbound = %+v", tunInbound)
	}
	if tunInbound["auto_route"] != true || tunInbound["strict_route"] != true || tunInbound["stack"] != "mixed" {
		t.Errorf("tun flags = %+v", tunInbound)
	}
	route := asMap(doc["route"])
	if route["auto_detect_interface"] != true {
		t.Errorf("auto_detect_interface = %v", route["auto_detect_interface"])
	}
	rules := asSlice(route["rules"])
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(rules))
	}
	first := asMap(rules[0])
	if first["action"] != "hijack-dns" || first["protocol"] != "dns" {
		t.Errorf("first rule = %+v", first)
	}
	if route["final"] != "proxy" {
		t.Errorf("route.final = %v", route["final"])
	}
}

func TestRenderSingBoxExistingTunAndHijackRule(t *testing.T) {
	profile := strings.Replace(singBoxProfile,
		`"inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}],`,
		`"inbounds": [
      {"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890},
      {"type": "tun", "tag": "my-tun", "address": ["172.20.0.1/30"], "stack": "gvisor", "mtu": 1500}
    ],`, 1)
	profile = strings.Replace(profile,
		`"rules": [{"ip_is_private": true, "outbound": "direct"}],`,
		`"rules": [{"protocol": "dns", "action": "hijack-dns"}, {"ip_is_private": true, "outbound": "direct"}],`, 1)
	result, err := Render(model.CoreSingBox, []byte(profile), Defaults(), "1.14.4")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	doc := map[string]any{}
	_ = json.Unmarshal(result.Config, &doc)
	inbounds := asSlice(doc["inbounds"])
	if len(inbounds) != 2 {
		t.Fatalf("inbounds = %d, want 2 (reused)", len(inbounds))
	}
	tunInbound := asMap(inbounds[1])
	if tunInbound["tag"] != "my-tun" {
		t.Errorf("tag = %v", tunInbound["tag"])
	}
	if asStringSlice(tunInbound["address"])[0] != "172.20.0.1/30" {
		t.Errorf("address = %v", tunInbound["address"])
	}
	if tunInbound["stack"] != "mixed" || tunInbound["mtu"] != float64(9000) {
		t.Errorf("tun = %+v", tunInbound)
	}
	rules := asSlice(asMap(doc["route"])["rules"])
	hijack := 0
	for _, rule := range rules {
		if asMap(rule)["action"] == "hijack-dns" {
			hijack++
		}
	}
	if hijack != 1 {
		t.Errorf("hijack rules = %d, want 1", hijack)
	}
	if len(result.Overrides) < 2 {
		t.Errorf("overrides = %v", result.Overrides)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about the existing TUN inbound")
	}
}

func TestRenderSingBoxVersionGate(t *testing.T) {
	if _, err := Render(model.CoreSingBox, []byte(singBoxProfile), Defaults(), "1.10.7"); err == nil {
		t.Error("expected an error for sing-box < 1.11")
	}
	if _, err := Render(model.CoreSingBox, []byte("{not json"), Defaults(), "1.14.4"); err == nil {
		t.Error("expected an error for invalid JSON")
	}
	if _, err := Render(model.CoreSingBox, []byte(`{"route": {}}`), Defaults(), "1.14.4"); err == nil {
		t.Error("expected an error without outbounds")
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		version, minimum string
		want             bool
	}{
		{"1.14.4", "1.11.0", true},
		{"1.10.9", "1.11.0", false},
		{"v1.19.34", "1.18.0", true},
		{"1.19.0-alpha", "1.18.0", true},
		{"", "1.11.0", false},
	}
	for _, testCase := range cases {
		if got := VersionAtLeast(testCase.version, testCase.minimum); got != testCase.want {
			t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", testCase.version, testCase.minimum, got, testCase.want)
		}
	}
}

func TestSettingsValidate(t *testing.T) {
	settings := Defaults()
	if err := settings.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	settings.MTU = 100
	if err := settings.Validate(); err == nil {
		t.Error("expected an MTU error")
	}
	settings = Defaults()
	settings.Stack = "bogus"
	if err := settings.Validate(); err == nil {
		t.Error("expected a stack error")
	}
}
