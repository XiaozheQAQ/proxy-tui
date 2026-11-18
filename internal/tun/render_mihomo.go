package tun

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// renderMihomo applies the TUN block to a Clash/Mihomo YAML document.
func renderMihomo(raw []byte, settings Settings) (RenderResult, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return RenderResult{}, fmt.Errorf("mihomo config is not valid YAML: %w", err)
	}
	if len(doc) == 0 {
		return RenderResult{}, fmt.Errorf("mihomo config is empty")
	}
	if len(asSlice(doc["proxies"])) == 0 && doc["proxy-providers"] == nil {
		return RenderResult{}, fmt.Errorf("mihomo config has no proxies to run with TUN")
	}

	existing := asMap(doc["tun"])
	tunBlock := map[string]any{}
	for key, value := range existing {
		tunBlock[key] = value
	}
	overrides := make([]string, 0, 8)

	set := func(key string, value any) {
		if old, ok := tunBlock[key]; ok && !reflect.DeepEqual(old, value) {
			overrides = append(overrides, fmt.Sprintf("tun.%s (was %v)", key, old))
		}
		tunBlock[key] = value
	}

	set("enable", true)
	set("stack", string(settings.Stack))
	set("auto-route", settings.AutoRoute)
	set("auto-detect-interface", true)
	set("strict-route", settings.StrictRoute)
	set("mtu", settings.MTU)
	if settings.Device != "" {
		set("device", settings.Device)
	}
	if settings.DNSHijack {
		if _, ok := tunBlock["dns-hijack"]; !ok {
			set("dns-hijack", []any{"any:53", "tcp://any:53"})
		}
	}
	doc["tun"] = tunBlock

	warnings := make([]string, 0, 4)
	if enabled, ok := existing["enable"].(bool); ok && enabled {
		warnings = append(warnings, "the profile already enabled TUN; ProxyTUI overrode the fields listed in overrides")
	}
	if settings.DNSHijack {
		dns := asMap(doc["dns"])
		if enabled, ok := dns["enable"].(bool); !ok || !enabled {
			warnings = append(warnings, "DNS is not enabled in this profile; hijacked DNS may not resolve (ProxyTUI does not change the dns section)")
		}
	} else if _, ok := tunBlock["dns-hijack"]; ok {
		warnings = append(warnings, "the profile defines its own dns-hijack; it is preserved because DNS hijack is disabled in settings")
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return RenderResult{}, fmt.Errorf("render mihomo config: %w", err)
	}
	sort.Strings(overrides)
	return RenderResult{
		Config:    out,
		Format:    model.FormatMihomoYAML,
		Overrides: overrides,
		Warnings:  warnings,
	}, nil
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprintf("%v", key)] = item
		}
		return out
	default:
		return nil
	}
}

func asSlice(value any) []any {
	if value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func listDevices(devices []string) string {
	if len(devices) == 0 {
		return "none"
	}
	return strings.Join(devices, ", ")
}
