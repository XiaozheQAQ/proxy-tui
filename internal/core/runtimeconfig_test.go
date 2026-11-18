package core

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPrepareMihomoConfig(t *testing.T) {
	input := `
mixed-port: 1080
allow-lan: true
dns:
  enable: true
proxies:
  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
rules:
  - MATCH,PROXY
`
	opts := RuntimeOptions{ProxyPort: 7897, APIPort: 9101, Secret: "s3cret", LogLevel: "debug"}
	out, err := PrepareMihomoConfig([]byte(input), opts)
	if err != nil {
		t.Fatalf("PrepareMihomoConfig: %v", err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not YAML: %v", err)
	}
	if doc["mixed-port"] != 7897 {
		t.Errorf("mixed-port = %v", doc["mixed-port"])
	}
	if doc["external-controller"] != "127.0.0.1:9101" {
		t.Errorf("external-controller = %v", doc["external-controller"])
	}
	if doc["secret"] != "s3cret" {
		t.Errorf("secret = %v", doc["secret"])
	}
	if doc["allow-lan"] != false {
		t.Errorf("allow-lan = %v", doc["allow-lan"])
	}
	if doc["log-level"] != "debug" {
		t.Errorf("log-level = %v", doc["log-level"])
	}
	if _, ok := doc["dns"]; !ok {
		t.Error("dns section was dropped")
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != 1 {
		t.Errorf("proxies = %d", len(proxies))
	}
	rules, _ := doc["rules"].([]any)
	if len(rules) != 1 {
		t.Errorf("rules = %d", len(rules))
	}
	if _, err := PrepareMihomoConfig([]byte("proxies: ["), opts); err == nil {
		t.Error("expected an error for invalid YAML")
	}
}

func TestPrepareSingBoxConfig(t *testing.T) {
	input := `
{
  "log": {"level": "warn"},
  "inbounds": [{"type": "mixed", "tag": "in", "listen": "0.0.0.0", "listen_port": 1080}],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}
`
	opts := RuntimeOptions{ProxyPort: 7897, APIPort: 9101, Secret: "s3cret"}
	out, err := PrepareSingBoxConfig([]byte(input), opts)
	if err != nil {
		t.Fatalf("PrepareSingBoxConfig: %v", err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("inbounds = %d", len(inbounds))
	}
	inbound, _ := inbounds[0].(map[string]any)
	if inbound["listen"] != "127.0.0.1" {
		t.Errorf("listen = %v", inbound["listen"])
	}
	if inbound["listen_port"] != float64(7897) {
		t.Errorf("listen_port = %v", inbound["listen_port"])
	}
	experimental, _ := doc["experimental"].(map[string]any)
	clashAPI, _ := experimental["clash_api"].(map[string]any)
	if clashAPI["external_controller"] != "127.0.0.1:9101" {
		t.Errorf("external_controller = %v", clashAPI["external_controller"])
	}
	if clashAPI["secret"] != "s3cret" {
		t.Errorf("secret = %v", clashAPI["secret"])
	}
	route, _ := doc["route"].(map[string]any)
	if route["final"] != "direct" {
		t.Errorf("route.final = %v", route["final"])
	}

	added, err := PrepareSingBoxConfig([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`), opts)
	if err != nil {
		t.Fatalf("PrepareSingBoxConfig without inbounds: %v", err)
	}
	if !strings.Contains(string(added), "mixed-in") {
		t.Errorf("a mixed inbound was not injected: %s", added)
	}
	if _, err := PrepareSingBoxConfig([]byte("{not json"), opts); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}
