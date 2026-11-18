package systemproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/execx"
)

const gsettingsSchema = "org.gnome.system.proxy"

// Linux controls the GNOME desktop proxy through GSettings. Other desktop
// environments are reported as unsupported instead of guessing.
type Linux struct {
	runner  execx.Runner
	desktop string
}

// NewLinux builds the GNOME adapter. desktop is XDG_CURRENT_DESKTOP.
func NewLinux(runner execx.Runner, desktop string) *Linux {
	return &Linux{runner: runner, desktop: desktop}
}

// Name implements network.Adapter.
func (l *Linux) Name() string { return "linux-gsettings" }

type linuxState struct {
	Mode        string   `json:"mode"`
	HTTPHost    string   `json:"http_host,omitempty"`
	HTTPPort    int      `json:"http_port,omitempty"`
	HTTPSHost   string   `json:"https_host,omitempty"`
	HTTPSPort   int      `json:"https_port,omitempty"`
	SocksHost   string   `json:"socks_host,omitempty"`
	SocksPort   int      `json:"socks_port,omitempty"`
	IgnoreHosts []string `json:"ignore_hosts,omitempty"`
}

// Capabilities implements network.Adapter.
func (l *Linux) Capabilities(ctx context.Context) network.Capabilities {
	caps := network.Capabilities{Platform: "linux", Method: "GSettings (org.gnome.system.proxy)"}
	desktop := strings.ToLower(l.desktop)
	supportedDesktop := strings.Contains(desktop, "gnome") || strings.Contains(desktop, "unity") ||
		strings.Contains(desktop, "budgie") || strings.Contains(desktop, "cinnamon") ||
		strings.Contains(desktop, "pop")
	if !supportedDesktop {
		caps.Reason = fmt.Sprintf("desktop environment %q is not supported yet (GNOME family only; KDE is a follow-up)", strings.TrimSpace(l.desktop))
		return caps
	}
	if _, stderr, err := l.runner.Run(ctx, "gsettings", "--version"); err != nil {
		caps.Reason = "gsettings is not available: " + firstNonEmpty(stderr, err.Error())
		return caps
	}
	caps.Supported = true
	return caps
}

func (l *Linux) get(ctx context.Context, schema, key string) (string, error) {
	stdout, stderr, err := l.runner.Run(ctx, "gsettings", "get", schema, key)
	if err != nil {
		return "", fmt.Errorf("gsettings get %s %s: %s", schema, key, firstNonEmpty(stderr, err.Error()))
	}
	return strings.TrimSpace(stdout), nil
}

func (l *Linux) set(ctx context.Context, schema, key, value string) error {
	if _, stderr, err := l.runner.Run(ctx, "gsettings", "set", schema, key, value); err != nil {
		return fmt.Errorf("gsettings set %s %s: %s", schema, key, firstNonEmpty(stderr, err.Error()))
	}
	return nil
}

func (l *Linux) readState(ctx context.Context) (linuxState, error) {
	state := linuxState{}
	mode, err := l.get(ctx, gsettingsSchema, "mode")
	if err != nil {
		return linuxState{}, err
	}
	state.Mode = parseGVariantString(mode)
	for _, item := range []struct {
		schema string
		host   *string
		port   *int
	}{
		{gsettingsSchema + ".http", &state.HTTPHost, &state.HTTPPort},
		{gsettingsSchema + ".https", &state.HTTPSHost, &state.HTTPSPort},
		{gsettingsSchema + ".socks", &state.SocksHost, &state.SocksPort},
	} {
		host, err := l.get(ctx, item.schema, "host")
		if err != nil {
			return linuxState{}, err
		}
		port, err := l.get(ctx, item.schema, "port")
		if err != nil {
			return linuxState{}, err
		}
		*item.host = parseGVariantString(host)
		*item.port = parseGVariantInt(port)
	}
	ignore, err := l.get(ctx, gsettingsSchema, "ignore-hosts")
	if err != nil {
		return linuxState{}, err
	}
	state.IgnoreHosts = parseGVariantStringArray(ignore)
	return state, nil
}

// GetState implements network.Adapter.
func (l *Linux) GetState(ctx context.Context) (network.ProxyState, error) {
	state, err := l.readState(ctx)
	if err != nil {
		return network.ProxyState{}, err
	}
	return network.ProxyState{
		Enabled:  state.Mode == "manual",
		Protocol: "http",
		Host:     state.HTTPHost,
		Port:     state.HTTPPort,
		Bypass:   strings.Join(state.IgnoreHosts, ","),
		Detail:   "mode=" + state.Mode,
	}, nil
}

// Snapshot implements network.Adapter.
func (l *Linux) Snapshot(ctx context.Context) (network.Snapshot, error) {
	state, err := l.readState(ctx)
	if err != nil {
		return network.Snapshot{}, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return network.Snapshot{}, err
	}
	proxy, err := l.GetState(ctx)
	if err != nil {
		return network.Snapshot{}, err
	}
	return network.Snapshot{Adapter: l.Name(), Taken: time.Now(), Proxy: proxy, Data: data}, nil
}

// Apply implements network.Adapter.
func (l *Linux) Apply(ctx context.Context, desired network.Desired) error {
	port := strconv.Itoa(desired.Port)
	steps := []struct {
		schema string
		key    string
		value  string
	}{
		{gsettingsSchema, "mode", "'manual'"},
		{gsettingsSchema + ".http", "host", gvariantString(desired.Host)},
		{gsettingsSchema + ".http", "port", port},
		{gsettingsSchema + ".https", "host", gvariantString(desired.Host)},
		{gsettingsSchema + ".https", "port", port},
	}
	if desired.Protocol == "socks5" {
		steps = append(steps,
			struct {
				schema string
				key    string
				value  string
			}{gsettingsSchema + ".socks", "host", gvariantString(desired.Host)},
			struct {
				schema string
				key    string
				value  string
			}{gsettingsSchema + ".socks", "port", port},
		)
	}
	if bypass := splitBypass(desired.Bypass); len(bypass) > 0 {
		steps = append(steps, struct {
			schema string
			key    string
			value  string
		}{gsettingsSchema, "ignore-hosts", gvariantStringArray(bypass)})
	}
	for _, step := range steps {
		if err := l.set(ctx, step.schema, step.key, step.value); err != nil {
			return err
		}
	}
	return nil
}

// Verify implements network.Adapter.
func (l *Linux) Verify(ctx context.Context, desired network.Desired) error {
	state, err := l.GetState(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return fmt.Errorf("the GNOME proxy mode is not manual")
	}
	if state.Host != desired.Host || state.Port != desired.Port {
		return fmt.Errorf("the GNOME http proxy is %s, want %s", state.Address(), desired.Address())
	}
	return nil
}

// Restore implements network.Adapter.
func (l *Linux) Restore(ctx context.Context, snapshot network.Snapshot) error {
	state := linuxState{}
	if err := json.Unmarshal(snapshot.Data, &state); err != nil {
		return fmt.Errorf("decode GNOME proxy snapshot: %w", err)
	}
	mode := state.Mode
	if mode == "" {
		mode = "none"
	}
	steps := []struct {
		schema string
		key    string
		value  string
	}{
		{gsettingsSchema, "mode", gvariantString(mode)},
		{gsettingsSchema + ".http", "host", gvariantString(state.HTTPHost)},
		{gsettingsSchema + ".http", "port", strconv.Itoa(state.HTTPPort)},
		{gsettingsSchema + ".https", "host", gvariantString(state.HTTPSHost)},
		{gsettingsSchema + ".https", "port", strconv.Itoa(state.HTTPSPort)},
		{gsettingsSchema + ".socks", "host", gvariantString(state.SocksHost)},
		{gsettingsSchema + ".socks", "port", strconv.Itoa(state.SocksPort)},
		{gsettingsSchema, "ignore-hosts", gvariantStringArray(state.IgnoreHosts)},
	}
	for _, step := range steps {
		if err := l.set(ctx, step.schema, step.key, step.value); err != nil {
			return err
		}
	}
	return nil
}

func parseGVariantString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "''" || value == "\"\"" {
		return ""
	}
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return strings.ReplaceAll(value[1:len(value)-1], "\\'", "'")
		}
	}
	return value
}

func parseGVariantInt(value string) int {
	value = strings.TrimSpace(value)
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func parseGVariantStringArray(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" || value == "@as []" {
		return nil
	}
	value = strings.TrimPrefix(value, "@as ")
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, parseGVariantString(part))
	}
	return out
}

func gvariantString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func gvariantStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, gvariantString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
