package storage

import (
	"encoding/json"
	"fmt"
)

// ApplySettingsPatch applies only the named fields, preserving every other
// value. This is the concurrency-safe write path used by multiple TUI/CLI
// clients: a stale client cannot clobber fields it did not edit.
func ApplySettingsPatch(current Settings, patch map[string]json.RawMessage) (Settings, error) {
	next := current
	for key, raw := range patch {
		switch key {
		case "theme":
			if err := json.Unmarshal(raw, &next.Theme); err != nil {
				return current, fmt.Errorf("theme: %w", err)
			}
		case "log_level":
			if err := json.Unmarshal(raw, &next.LogLevel); err != nil {
				return current, fmt.Errorf("log_level: %w", err)
			}
		case "auto_update":
			if err := json.Unmarshal(raw, &next.AutoUpdate); err != nil {
				return current, fmt.Errorf("auto_update: %w", err)
			}
		case "update_interval_seconds":
			if err := json.Unmarshal(raw, &next.UpdateIntervalSeconds); err != nil {
				return current, fmt.Errorf("update_interval_seconds: %w", err)
			}
		case "check_core_updates":
			if err := json.Unmarshal(raw, &next.CheckCoreUpdates); err != nil {
				return current, fmt.Errorf("check_core_updates: %w", err)
			}
		case "subscription_user_agent":
			if err := json.Unmarshal(raw, &next.SubscriptionUserAgent); err != nil {
				return current, fmt.Errorf("subscription_user_agent: %w", err)
			}
		case "confirm_quit":
			if err := json.Unmarshal(raw, &next.ConfirmQuit); err != nil {
				return current, fmt.Errorf("confirm_quit: %w", err)
			}
		case "traffic_mode":
			if err := json.Unmarshal(raw, &next.TrafficMode); err != nil {
				return current, fmt.Errorf("traffic_mode: %w", err)
			}
		case "tun_core":
			if err := json.Unmarshal(raw, &next.TunCore); err != nil {
				return current, fmt.Errorf("tun_core: %w", err)
			}
		case "tun_stack":
			if err := json.Unmarshal(raw, &next.TunStack); err != nil {
				return current, fmt.Errorf("tun_stack: %w", err)
			}
		case "tun_mtu":
			if err := json.Unmarshal(raw, &next.TunMTU); err != nil {
				return current, fmt.Errorf("tun_mtu: %w", err)
			}
		case "tun_dns_hijack":
			if err := json.Unmarshal(raw, &next.TunDNSHijack); err != nil {
				return current, fmt.Errorf("tun_dns_hijack: %w", err)
			}
		case "tun_ipv6":
			if err := json.Unmarshal(raw, &next.TunIPv6); err != nil {
				return current, fmt.Errorf("tun_ipv6: %w", err)
			}
		default:
			return current, fmt.Errorf("unknown settings field %q", key)
		}
	}
	next.Normalize()
	return next, nil
}
