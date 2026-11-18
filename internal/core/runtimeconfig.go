package core

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// RuntimeOptions are the ports and secret injected into every runtime config.
type RuntimeOptions struct {
	ProxyPort int
	APIPort   int
	Secret    string
	LogLevel  string
}

// DefaultRuntimeOptions returns the default local ports.
func DefaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{ProxyPort: 7890, APIPort: 9090, LogLevel: "info"}
}

// PrepareMihomoConfig forces the local ports and control API of a Clash config
// while keeping the user's rules, DNS and groups intact.
func PrepareMihomoConfig(data []byte, opts RuntimeOptions) ([]byte, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("mihomo config is not valid YAML: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("mihomo config is empty")
	}
	doc["mixed-port"] = opts.ProxyPort
	doc["allow-lan"] = false
	doc["external-controller"] = fmt.Sprintf("127.0.0.1:%d", opts.APIPort)
	doc["secret"] = opts.Secret
	if opts.LogLevel != "" {
		doc["log-level"] = opts.LogLevel
	}
	if _, ok := doc["mode"]; !ok {
		doc["mode"] = "rule"
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("render mihomo config: %w", err)
	}
	return out, nil
}

// PrepareSingBoxConfig forces a local mixed inbound and the Clash compatible
// API so the TUI can read connections and switch outbounds.
func PrepareSingBoxConfig(data []byte, opts RuntimeOptions) ([]byte, error) {
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("sing-box config is not valid JSON: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("sing-box config is empty")
	}
	if opts.LogLevel != "" {
		doc["log"] = mergeMap(doc["log"], map[string]any{"level": opts.LogLevel})
	}

	inbounds := sliceOfMaps(doc["inbounds"])
	patched := false
	for _, inbound := range inbounds {
		kind, _ := inbound["type"].(string)
		if kind != "mixed" && kind != "http" && kind != "socks" {
			continue
		}
		inbound["listen"] = "127.0.0.1"
		inbound["listen_port"] = opts.ProxyPort
		patched = true
	}
	if !patched {
		inbounds = append(inbounds, map[string]any{
			"type":        "mixed",
			"tag":         "mixed-in",
			"listen":      "127.0.0.1",
			"listen_port": opts.ProxyPort,
		})
	}
	doc["inbounds"] = inbounds

	experimental := mergeMap(doc["experimental"], map[string]any{})
	clashAPI := mergeMap(experimental["clash_api"], map[string]any{})
	clashAPI["external_controller"] = fmt.Sprintf("127.0.0.1:%d", opts.APIPort)
	clashAPI["secret"] = opts.Secret
	experimental["clash_api"] = clashAPI
	doc["experimental"] = experimental

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render sing-box config: %w", err)
	}
	return append(out, '\n'), nil
}

func mergeMap(existing any, defaults map[string]any) map[string]any {
	merged := map[string]any{}
	if m, ok := existing.(map[string]any); ok {
		for k, v := range m {
			merged[k] = v
		}
	}
	for k, v := range defaults {
		merged[k] = v
	}
	return merged
}

func sliceOfMaps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
