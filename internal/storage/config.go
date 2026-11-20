package storage

import (
	"errors"
	"io/fs"
	"os"
)

// Theme names accepted by the TUI.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Settings is the persisted user configuration (config.json).
type Settings struct {
	Theme      string `json:"theme"`
	LogLevel   string `json:"log_level"`
	AutoUpdate bool   `json:"auto_update"`
	// UpdateIntervalSeconds controls the background profile refresh cycle.
	UpdateIntervalSeconds int `json:"update_interval_seconds"`
	// CheckCoreUpdates asks the core manager to look for newer releases.
	CheckCoreUpdates bool `json:"check_core_updates"`
	// SubscriptionUserAgent is sent when fetching profiles. Many providers
	// return different formats depending on this header.
	SubscriptionUserAgent string `json:"subscription_user_agent"`
	// ConfirmQuit asks before leaving the TUI.
	ConfirmQuit bool `json:"confirm_quit"`
	// TrafficMode is the traffic takeover mode: off, system-proxy, tun, manual.
	TrafficMode string `json:"traffic_mode"`
	// TunCore is the core used for TUN when none is running.
	TunCore string `json:"tun_core"`
	// TunStack is the TUN stack: mixed, system or gvisor.
	TunStack string `json:"tun_stack"`
	// TunMTU is the TUN device MTU.
	TunMTU int `json:"tun_mtu"`
	// TunDNSHijack asks the core to hijack DNS through TUN.
	TunDNSHijack bool `json:"tun_dns_hijack"`
	// TunIPv6 enables the IPv6 TUN address.
	TunIPv6 bool `json:"tun_ipv6"`
}

// DefaultSettings returns the settings used on first launch.
func DefaultSettings() Settings {
	return Settings{
		Theme:                 ThemeDark,
		LogLevel:              "info",
		AutoUpdate:            true,
		UpdateIntervalSeconds: 12 * 60 * 60,
		CheckCoreUpdates:      true,
		SubscriptionUserAgent: "clash.meta/v1.19.0 proxy-tui/0.2.0",
		ConfirmQuit:           false,
		TrafficMode:           "off",
		TunCore:               "mihomo",
		TunStack:              "mixed",
		TunMTU:                9000,
		TunDNSHijack:          true,
		TunIPv6:               false,
	}
}

// Normalize repairs zero values so a hand-edited config cannot disable the TUI.
func (s *Settings) Normalize() {
	def := DefaultSettings()
	if s.Theme != ThemeDark && s.Theme != ThemeLight {
		s.Theme = def.Theme
	}
	if s.LogLevel == "" {
		s.LogLevel = def.LogLevel
	}
	if s.UpdateIntervalSeconds <= 0 {
		s.UpdateIntervalSeconds = def.UpdateIntervalSeconds
	}
	if s.SubscriptionUserAgent == "" {
		s.SubscriptionUserAgent = def.SubscriptionUserAgent
	}
	switch s.TrafficMode {
	case "off", "system-proxy", "tun", "manual":
	default:
		s.TrafficMode = def.TrafficMode
	}
	switch s.TunCore {
	case "mihomo", "sing-box":
	default:
		s.TunCore = def.TunCore
	}
	switch s.TunStack {
	case "mixed", "system", "gvisor":
	default:
		s.TunStack = def.TunStack
	}
	if s.TunMTU < 1280 || s.TunMTU > 65535 {
		s.TunMTU = def.TunMTU
	}
}

// LoadSettings reads config.json, falling back to defaults when absent.
func LoadSettings(paths Paths) (Settings, error) {
	settings := DefaultSettings()
	err := ReadJSON(paths.ConfigFile(), &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return DefaultSettings(), err
	}
	settings.Normalize()
	return settings, nil
}

// SaveSettings persists settings atomically with user-only permissions.
func SaveSettings(paths Paths, settings Settings) error {
	settings.Normalize()
	return WriteJSONAtomic(paths.ConfigFile(), settings, 0o600)
}

// Keymap overrides individual bindings. Empty values fall back to defaults.
type Keymap struct {
	Bindings map[string]string `json:"bindings"`
}

// LoadKeymap reads keymap.json, returning an empty map when absent.
func LoadKeymap(paths Paths) (Keymap, error) {
	km := Keymap{Bindings: map[string]string{}}
	err := ReadJSON(paths.KeymapFile(), &km)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Keymap{Bindings: map[string]string{}}, err
	}
	if km.Bindings == nil {
		km.Bindings = map[string]string{}
	}
	return km, nil
}

// SaveKeymap writes keymap.json.
func SaveKeymap(paths Paths, km Keymap) error {
	if km.Bindings == nil {
		km.Bindings = map[string]string{}
	}
	return WriteJSONAtomic(paths.KeymapFile(), km, 0o600)
}

// AppendLogLine appends a line to the application log file.
func AppendLogLine(paths Paths, line string) error {
	if err := os.MkdirAll(paths.LogsDir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(paths.AppLogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
