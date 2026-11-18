//go:build windows

package systemproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/proxytui/proxy-tui/internal/network"
)

const defaultWindowsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// Windows controls the current user's WinINet proxy configuration.
type Windows struct {
	key string
}

// NewWindows builds the WinINet adapter for the current user.
func NewWindows() *Windows { return &Windows{key: defaultWindowsKey} }

// NewWindowsWithKey builds an adapter for another registry key (tests).
func NewWindowsWithKey(key string) *Windows { return &Windows{key: key} }

// Name implements network.Adapter.
func (w *Windows) Name() string { return "windows-wininet" }

type winState struct {
	Enable         uint32 `json:"enable"`
	EnableExists   bool   `json:"enable_exists"`
	Server         string `json:"server"`
	ServerExists   bool   `json:"server_exists"`
	Override       string `json:"override"`
	OverrideExists bool   `json:"override_exists"`
}

// Capabilities implements network.Adapter.
func (w *Windows) Capabilities(ctx context.Context) network.Capabilities {
	return network.Capabilities{
		Platform:  "windows",
		Supported: true,
		Method:    "WinINet registry (current user)",
	}
}

func (w *Windows) openKey(access uint32) (registry.Key, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, w.key, access)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return registry.Key(0), err
	}
	key, _, err = registry.CreateKey(registry.CURRENT_USER, w.key, access)
	if err != nil {
		return registry.Key(0), fmt.Errorf("open or create registry key %s: %w", w.key, err)
	}
	return key, nil
}

func (w *Windows) readState() (winState, error) {
	state := winState{}
	key, err := registry.OpenKey(registry.CURRENT_USER, w.key, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()

	if value, _, err := key.GetIntegerValue("ProxyEnable"); err == nil {
		state.Enable = uint32(value)
		state.EnableExists = true
	}
	if value, _, err := key.GetStringValue("ProxyServer"); err == nil {
		state.Server = value
		state.ServerExists = true
	}
	if value, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		state.Override = value
		state.OverrideExists = true
	}
	return state, nil
}

func (s winState) proxyState() network.ProxyState {
	state := network.ProxyState{Enabled: s.Enable == 1, Bypass: s.Override, Detail: s.Server}
	if s.Server != "" {
		host, port, protocol := parseWinServer(s.Server)
		state.Host = host
		state.Port = port
		state.Protocol = protocol
	}
	return state
}

func parseWinServer(server string) (host string, port int, protocol string) {
	protocol = "http"
	entries := []string{server}
	if strings.Contains(server, "=") {
		entries = strings.Split(server, ";")
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name := ""
		value := entry
		if k, v, ok := strings.Cut(entry, "="); ok {
			name = strings.ToLower(strings.TrimSpace(k))
			value = strings.TrimSpace(v)
		}
		parsedHost, parsedPort, err := net.SplitHostPort(value)
		if err != nil {
			continue
		}
		number, err := strconv.Atoi(parsedPort)
		if err != nil || number <= 0 || number > 65535 {
			continue
		}
		if name != "" {
			protocol = name
		}
		return parsedHost, number, protocol
	}
	return "", 0, protocol
}

// GetState implements network.Adapter.
func (w *Windows) GetState(ctx context.Context) (network.ProxyState, error) {
	state, err := w.readState()
	if err != nil {
		return network.ProxyState{}, err
	}
	return state.proxyState(), nil
}

// Snapshot implements network.Adapter.
func (w *Windows) Snapshot(ctx context.Context) (network.Snapshot, error) {
	state, err := w.readState()
	if err != nil {
		return network.Snapshot{}, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return network.Snapshot{}, err
	}
	return network.Snapshot{Adapter: w.Name(), Taken: time.Now(), Proxy: state.proxyState(), Data: data}, nil
}

// Apply implements network.Adapter.
func (w *Windows) Apply(ctx context.Context, desired network.Desired) error {
	key, err := w.openKey(registry.QUERY_VALUE | registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()

	server := fmt.Sprintf("http=%s;https=%s", desired.Address(), desired.Address())
	if desired.Protocol == "socks5" {
		server += ";socks=" + desired.Address()
	}
	if err := key.SetStringValue("ProxyServer", server); err != nil {
		return fmt.Errorf("write ProxyServer: %w", err)
	}
	bypass := desired.Bypass
	if bypass == "" {
		bypass = network.DefaultBypass()
	}
	if err := key.SetStringValue("ProxyOverride", bypass); err != nil {
		return fmt.Errorf("write ProxyOverride: %w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("write ProxyEnable: %w", err)
	}
	notifyWinINet()
	return nil
}

// Verify implements network.Adapter.
func (w *Windows) Verify(ctx context.Context, desired network.Desired) error {
	state, err := w.GetState(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return fmt.Errorf("ProxyEnable is 0")
	}
	if state.Host != desired.Host || state.Port != desired.Port {
		return fmt.Errorf("ProxyServer resolves to %s, want %s", state.Address(), desired.Address())
	}
	return nil
}

// Restore implements network.Adapter.
func (w *Windows) Restore(ctx context.Context, snapshot network.Snapshot) error {
	state := winState{}
	if err := json.Unmarshal(snapshot.Data, &state); err != nil {
		return fmt.Errorf("decode WinINet proxy snapshot: %w", err)
	}
	key, err := w.openKey(registry.QUERY_VALUE | registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()

	if state.EnableExists {
		if err := key.SetDWordValue("ProxyEnable", state.Enable); err != nil {
			return fmt.Errorf("restore ProxyEnable: %w", err)
		}
	} else if err := key.DeleteValue("ProxyEnable"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("clear ProxyEnable: %w", err)
	}
	if state.ServerExists {
		if err := key.SetStringValue("ProxyServer", state.Server); err != nil {
			return fmt.Errorf("restore ProxyServer: %w", err)
		}
	} else if err := key.DeleteValue("ProxyServer"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("clear ProxyServer: %w", err)
	}
	if state.OverrideExists {
		if err := key.SetStringValue("ProxyOverride", state.Override); err != nil {
			return fmt.Errorf("restore ProxyOverride: %w", err)
		}
	} else if err := key.DeleteValue("ProxyOverride"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("clear ProxyOverride: %w", err)
	}
	notifyWinINet()
	return nil
}

var (
	wininetDLL        = windows.NewLazySystemDLL("wininet.dll")
	internetSetOption = wininetDLL.NewProc("InternetSetOptionW")
)

// notifyWinINet asks WinINet clients to reload the proxy configuration.
func notifyWinINet() {
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	_, _, _ = internetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = internetSetOption.Call(0, internetOptionRefresh, 0, 0)
}
