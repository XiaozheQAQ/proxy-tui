// Package network owns the operating system network state that ProxyTUI
// changes: the desktop system proxy today, TUN integration later.
//
// Every change is transactional: the original configuration is snapshotted,
// applied, verified and either committed or rolled back. The previous state is
// kept on disk so a crash or a later process can restore it.
package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Mode is the traffic takeover mode selected by the user.
type Mode string

// Traffic takeover modes.
const (
	ModeOff         Mode = "off"
	ModeSystemProxy Mode = "system-proxy"
	ModeTUN         Mode = "tun"
	ModeManual      Mode = "manual"
)

// Modes lists the supported modes for the UI.
func Modes() []Mode { return []Mode{ModeOff, ModeSystemProxy, ModeTUN, ModeManual} }

// ParseMode accepts the CLI spellings.
func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "none", "direct":
		return ModeOff, nil
	case "system-proxy", "system", "systemproxy":
		return ModeSystemProxy, nil
	case "tun":
		return ModeTUN, nil
	case "manual":
		return ModeManual, nil
	}
	return "", fmt.Errorf("unknown traffic mode %q (want off, system-proxy, tun or manual)", value)
}

// Display renders the mode for the TUI.
func (m Mode) Display() string {
	switch m {
	case ModeOff:
		return "Off"
	case ModeSystemProxy:
		return "System Proxy"
	case ModeTUN:
		return "TUN"
	case ModeManual:
		return "Manual"
	default:
		return string(m)
	}
}

// Errors reported by the network manager.
var (
	ErrUnsupported    = errors.New("system proxy control is not supported on this platform")
	ErrTUNUnsupported = errors.New("TUN is managed by the core-native TUN lifecycle; use the TUN API")
	ErrNoBackup       = errors.New("the desktop proxy is enabled but proxy-tui has no backup of the original settings")
	ErrInvalidDesired = errors.New("invalid system proxy configuration")
)

// Desired is the proxy configuration ProxyTUI wants to install.
type Desired struct {
	Enabled  bool   `json:"enabled"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Bypass   string `json:"bypass"`
}

// DefaultBypass is used when the user has no bypass list yet.
func DefaultBypass() string {
	return "localhost,127.0.0.1,::1,<local>,10.*,172.16.*,172.17.*,172.18.*,172.19.*,172.20.*,172.21.*,172.22.*,172.23.*,172.24.*,172.25.*,172.26.*,172.27.*,172.28.*,172.29.*,172.30.*,172.31.*,192.168.*"
}

// Validate checks the desired configuration.
func (d Desired) Validate() error {
	if !d.Enabled {
		return nil
	}
	if d.Host == "" || d.Port <= 0 || d.Port > 65535 {
		return fmt.Errorf("%w: host and port are required", ErrInvalidDesired)
	}
	switch d.Protocol {
	case "", "http", "https", "socks5":
	default:
		return fmt.Errorf("%w: unsupported protocol %q", ErrInvalidDesired, d.Protocol)
	}
	return nil
}

// Address renders host:port.
func (d Desired) Address() string { return fmt.Sprintf("%s:%d", d.Host, d.Port) }

// ServiceProxy is the state of one network service (macOS) or sub-setting.
type ServiceProxy struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
}

// ProxyState is the observed desktop proxy configuration.
type ProxyState struct {
	Enabled  bool           `json:"enabled"`
	Protocol string         `json:"protocol,omitempty"`
	Host     string         `json:"host,omitempty"`
	Port     int            `json:"port,omitempty"`
	Bypass   string         `json:"bypass,omitempty"`
	Services []ServiceProxy `json:"services,omitempty"`
	Detail   string         `json:"detail,omitempty"`
}

// Address renders host:port when available.
func (s ProxyState) Address() string {
	if s.Host == "" || s.Port == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// Capabilities describes what an adapter can do on this platform.
type Capabilities struct {
	Platform       string `json:"platform"`
	Supported      bool   `json:"supported"`
	Method         string `json:"method,omitempty"`
	Reason         string `json:"reason,omitempty"`
	NeedsElevation bool   `json:"needs_elevation,omitempty"`
}

// Snapshot is a full fidelity backup of one adapter's configuration.
type Snapshot struct {
	Adapter string          `json:"adapter"`
	Taken   time.Time       `json:"taken"`
	Proxy   ProxyState      `json:"proxy"`
	Data    json.RawMessage `json:"data"`
}

// Adapter controls one platform's desktop proxy implementation.
type Adapter interface {
	Name() string
	Capabilities(ctx context.Context) Capabilities
	GetState(ctx context.Context) (ProxyState, error)
	Snapshot(ctx context.Context) (Snapshot, error)
	Apply(ctx context.Context, desired Desired) error
	Verify(ctx context.Context, desired Desired) error
	Restore(ctx context.Context, snapshot Snapshot) error
}

// EnvironmentProxy is the shell level proxy configuration. It is detected for
// display only: ProxyTUI never edits the user's shell environment.
type EnvironmentProxy struct {
	HTTP    string `json:"http,omitempty"`
	HTTPS   string `json:"https,omitempty"`
	All     string `json:"all,omitempty"`
	NoProxy string `json:"no_proxy,omitempty"`
}

// Detected reports whether any shell proxy variable is set.
func (e EnvironmentProxy) Detected() bool {
	return e.HTTP != "" || e.HTTPS != "" || e.All != ""
}

// DetectEnvironment reads the current process environment (case insensitive).
func DetectEnvironment() EnvironmentProxy {
	get := func(names ...string) string {
		environ := os.Environ()
		for _, name := range names {
			prefix := strings.ToUpper(name) + "="
			for _, entry := range environ {
				if strings.HasPrefix(strings.ToUpper(entry), prefix) {
					return strings.TrimSpace(entry[len(prefix):])
				}
			}
		}
		return ""
	}
	return EnvironmentProxy{
		HTTP:    get("HTTP_PROXY", "http_proxy"),
		HTTPS:   get("HTTPS_PROXY", "https_proxy"),
		All:     get("ALL_PROXY", "all_proxy"),
		NoProxy: get("NO_PROXY", "no_proxy"),
	}
}

// Report is a full picture of the local network takeover state.
type Report struct {
	Mode           Mode             `json:"mode"`
	Capabilities   Capabilities     `json:"capabilities"`
	Desktop        ProxyState       `json:"desktop"`
	Environment    EnvironmentProxy `json:"environment"`
	BackupActive   bool             `json:"backup_active"`
	BackupStale    bool             `json:"backup_stale,omitempty"`
	BackupAt       time.Time        `json:"backup_at,omitempty"`
	BackupOriginal ProxyState       `json:"backup_original"`
	Adapters       []Capabilities   `json:"adapters"`
	Notes          []string         `json:"notes,omitempty"`
	// ForeignProxy is true when another local proxy already owns the desktop
	// proxy settings; those settings are backed up and restored, never trusted.
	ForeignProxy       bool   `json:"foreign_proxy"`
	ForeignProxyDetail string `json:"foreign_proxy_detail,omitempty"`
}

// TUNAvailable reports whether core-native TUN is available. The concrete
// lifecycle lives in internal/tun; network.Manager no longer owns TUN writes.
func TUNAvailable() bool { return true }

// SortedAdapters returns capabilities sorted by adapter name.
func SortedAdapters(caps []Capabilities) []Capabilities {
	out := append([]Capabilities(nil), caps...)
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out
}

// platformName is used when an adapter reports no platform.
func platformName() string {
	if name := strings.TrimSpace(runtime.GOOS); name != "" {
		return name
	}
	return "unknown"
}
