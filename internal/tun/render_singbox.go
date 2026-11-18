package tun

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

const (
	singBoxTunTag      = "tun-in"
	singBoxIPv4Address = "172.19.0.1/30"
	singBoxIPv6Address = "fdfe:dcba:9876::1/126"
)

// renderSingBox applies the TUN inbound and routing hooks to a sing-box JSON
// document. Only the 1.11+ schema is supported (address/action based).
func renderSingBox(raw []byte, settings Settings, coreVersion string) (RenderResult, error) {
	if coreVersion != "" && !VersionAtLeast(coreVersion, MinimumVersion(model.CoreSingBox)) {
		return RenderResult{}, fmt.Errorf("sing-box %s is too old for TUN: ProxyTUI requires 1.11.0 or newer", coreVersion)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return RenderResult{}, fmt.Errorf("sing-box config is not valid JSON: %w", err)
	}
	if len(doc) == 0 {
		return RenderResult{}, fmt.Errorf("sing-box config is empty")
	}
	if len(asSlice(doc["outbounds"])) == 0 {
		return RenderResult{}, fmt.Errorf("sing-box config has no outbounds to run with TUN")
	}

	overrides := make([]string, 0, 8)
	warnings := make([]string, 0, 4)

	inbounds := asSlice(doc["inbounds"])
	var existing map[string]any
	index := -1
	for i, item := range inbounds {
		entry := asMap(item)
		if entry == nil {
			continue
		}
		if entry["type"] == "tun" {
			existing = entry
			index = i
			break
		}
	}

	tun := map[string]any{}
	for key, value := range existing {
		tun[key] = value
	}
	set := func(key string, value any) {
		if old, ok := tun[key]; ok && !reflect.DeepEqual(old, value) {
			overrides = append(overrides, fmt.Sprintf("inbounds[type=tun].%s (was %v)", key, old))
		}
		tun[key] = value
	}
	if _, ok := tun["tag"]; !ok {
		tun["tag"] = singBoxTunTag
	}
	set("type", "tun")
	addresses := asStringSlice(tun["address"])
	if len(addresses) == 0 {
		addresses = []string{singBoxIPv4Address}
		if settings.IPv6 {
			addresses = append(addresses, singBoxIPv6Address)
		}
	} else if settings.IPv6 && !containsIPv6(addresses) {
		addresses = append(addresses, singBoxIPv6Address)
		warnings = append(warnings, "the profile TUN inbound had no IPv6 address; ProxyTUI appended "+singBoxIPv6Address)
	}
	tun["address"] = toAnySlice(addresses)
	set("auto_route", settings.AutoRoute)
	set("strict_route", settings.StrictRoute)
	set("stack", string(settings.Stack))
	set("mtu", settings.MTU)
	if settings.Device != "" {
		set("interface_name", settings.Device)
	}

	if index >= 0 {
		inbounds[index] = tun
	} else {
		inbounds = append(inbounds, tun)
	}
	doc["inbounds"] = inbounds
	if existing != nil {
		warnings = append(warnings, "the profile already had a TUN inbound; ProxyTUI reused it and overrode the fields listed in overrides")
	}

	route := asMap(doc["route"])
	if route == nil {
		route = map[string]any{}
	}
	if _, ok := route["auto_detect_interface"]; !ok || route["auto_detect_interface"] != true {
		route["auto_detect_interface"] = true
	}
	if settings.DNSHijack {
		rules := asSlice(route["rules"])
		if !hasDNSHijackRule(rules) {
			hijack := map[string]any{"protocol": "dns", "action": "hijack-dns"}
			rules = append([]any{hijack}, rules...)
			warnings = append(warnings, "prepended a DNS hijack route rule; review it if your rules must run before DNS interception")
		}
		route["rules"] = rules
	}
	doc["route"] = route

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return RenderResult{}, fmt.Errorf("render sing-box config: %w", err)
	}
	sort.Strings(overrides)
	return RenderResult{
		Config:    append(out, '\n'),
		Format:    model.FormatSingBoxJSON,
		Overrides: overrides,
		Warnings:  warnings,
	}, nil
}

func hasDNSHijackRule(rules []any) bool {
	for _, item := range rules {
		rule := asMap(item)
		if rule == nil {
			continue
		}
		if action, _ := rule["action"].(string); action == "hijack-dns" {
			return true
		}
	}
	return false
}

func asStringSlice(value any) []string {
	items := asSlice(value)
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func containsIPv6(values []string) bool {
	for _, value := range values {
		for _, r := range value {
			if r == ':' {
				return true
			}
		}
	}
	return false
}
