// Package app wires storage, subscriptions and cores together.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/core/mihomo"
	"github.com/proxytui/proxy-tui/internal/core/singbox"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/systemproxy"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// Version is replaced at build time via -ldflags.
var Version = "0.2.0-dev"

// App is the wired application.
type App struct {
	Paths     storage.Paths
	Settings  storage.Settings
	Keymap    storage.Keymap
	Logs      *logging.Buffer
	Subs      *subscription.Manager
	Cores     *core.Manager
	Network   *network.Manager
	TUN       *tun.Manager
	Runtime   core.RuntimeOptions
	BuildInfo string
	Started   time.Time
}

// New opens (and creates) every managed directory and wires the services.
func New(paths storage.Paths) (*App, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	settings, err := storage.LoadSettings(paths)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	keymap, err := storage.LoadKeymap(paths)
	if err != nil {
		keymap = storage.Keymap{Bindings: map[string]string{}}
	}

	logs := logging.NewBuffer(5000)
	logs.SetSink(func(entry logging.Entry) {
		_ = storage.AppendLogLine(paths, entry.Format())
	})

	fetcher := subscription.NewFetcher(settings.SubscriptionUserAgent)
	subs, err := subscription.NewManager(paths, fetcher)
	if err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}
	subs.SetUserAgent(settings.SubscriptionUserAgent)
	subs.SetDefaultInterval(settings.UpdateIntervalSeconds)
	subs.SetLogger(logs.Logger("subs"))

	runtimeOpts := core.DefaultRuntimeOptions()
	runtimeOpts.LogLevel = settings.LogLevel

	mihomoAdapter := mihomo.New(paths, runtimeOpts, logs.Logger("mihomo"))
	singboxAdapter := singbox.New(paths, runtimeOpts, logs.Logger("sing-box"))
	cores := core.NewManager(paths, logs, mihomoAdapter, singboxAdapter)

	subs.SetValidator(func(ctx context.Context, kind model.CoreKind, name string, data []byte) error {
		err := cores.Validate(ctx, kind, name, data)
		if errors.Is(err, core.ErrNotInstalled) {
			return subscription.ErrCoreUnavailable
		}
		return err
	})

	networkManager := network.NewManager(paths, logs.Logger("network"), systemproxy.ForPlatform()...)
	tunManager := tun.NewManager(paths, cores, subs, tun.NewSystemProbe(), logs.Logger("tun"))

	app := &App{
		Paths:     paths,
		Settings:  settings,
		Keymap:    keymap,
		Logs:      logs,
		Subs:      subs,
		Cores:     cores,
		Network:   networkManager,
		TUN:       tunManager,
		Runtime:   runtimeOpts,
		BuildInfo: BuildInfo(),
		Started:   time.Now(),
	}
	logs.Add("info", "app", "proxy-tui "+Version+" started")
	logs.Add("info", "app", "config "+paths.Config)
	logs.Add("info", "app", "data "+paths.Data)

	// Repair network state left behind by a crash or a stopped core.
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coreRunning := false
	for _, kind := range cores.Kinds() {
		if cores.Status(recoveryCtx, kind).Running {
			coreRunning = true
			break
		}
	}
	if recovery, err := networkManager.Recover(recoveryCtx, coreRunning); err != nil {
		logs.Add("error", "network", "recover: "+err.Error())
	} else if recovery.Action != "" {
		logs.Add("warn", "network", recovery.Action)
	}

	// Repair a TUN state left behind by a crash: only when the core is gone.
	if tunStatus, tunErr := tunManager.Status(recoveryCtx); tunErr == nil && tunStatus.Enabled && !tunStatus.CoreRunning {
		if recovery, recoverErr := tunManager.Recover(recoveryCtx); recoverErr != nil {
			logs.Add("error", "tun", "recover: "+recoverErr.Error())
		} else if recovery.Action != "" {
			logs.Add("warn", "tun", recovery.Action)
		}
	}
	return app, nil
}

// TunSettingsFrom converts persisted settings into TUN settings.
func TunSettingsFrom(settings storage.Settings) tun.Settings {
	stack, err := tun.ParseStack(settings.TunStack)
	if err != nil {
		stack = tun.StackMixed
	}
	mtu := settings.TunMTU
	if mtu <= 0 {
		mtu = 9000
	}
	return tun.Settings{
		Stack:       stack,
		MTU:         mtu,
		DNSHijack:   settings.TunDNSHijack,
		IPv6:        settings.TunIPv6,
		AutoRoute:   true,
		StrictRoute: true,
	}
}

// DefaultTunCore picks the core used for TUN: the running one, otherwise the
// configured preference, otherwise Mihomo.
func (a *App) DefaultTunCore(ctx context.Context) model.CoreKind {
	for _, kind := range a.Cores.Kinds() {
		if a.Cores.Status(ctx, kind).Running {
			return kind
		}
	}
	if kind, err := model.ParseCoreKind(a.Settings.TunCore); err == nil {
		return kind
	}
	return model.CoreMihomo
}

// TrafficResult reports one takeover mode change.
type TrafficResult struct {
	Mode     network.Mode
	Network  network.Result
	TUN      tun.Result
	Detail   string
	Warnings []string
}

// ApplyTrafficMode switches the traffic takeover mode for a specific core and
// TUN settings. TUN and the desktop system proxy are mutually exclusive.
func (a *App) ApplyTrafficMode(ctx context.Context, mode network.Mode, kind model.CoreKind, settings tun.Settings) (TrafficResult, error) {
	result := TrafficResult{Mode: mode}
	if !kind.Valid() {
		kind = a.DefaultTunCore(ctx)
	}
	switch mode {
	case network.ModeTUN:
		if report, err := a.NetworkReport(ctx); err == nil {
			switch {
			case report.BackupActive:
				if _, disableErr := a.DisableSystemProxy(ctx); disableErr != nil {
					result.Warnings = append(result.Warnings, "restoring the desktop proxy before TUN failed: "+disableErr.Error())
				}
			case report.Desktop.Enabled:
				result.Warnings = append(result.Warnings,
					"another desktop proxy is active ("+report.Desktop.Address()+"); TUN will coexist with it and may double-proxy traffic")
			}
		}
		tunResult, err := a.TUN.Enable(ctx, tun.EnableOptions{Core: kind, Settings: settings})
		result.TUN = tunResult
		result.Warnings = append(result.Warnings, tunResult.Warnings...)
		result.Warnings = append(result.Warnings, tunResult.Verify.Notes...)
		if err != nil {
			return result, err
		}
		result.Detail = fmt.Sprintf("TUN active on %s via %s", tunResult.Status.Interface, kind.DisplayName())
		return result, nil

	case network.ModeSystemProxy:
		if status, err := a.TUN.Status(ctx); err == nil && status.Enabled {
			disable, disableErr := a.TUN.Disable(ctx)
			result.Warnings = append(result.Warnings, disable.Warnings...)
			if disableErr != nil {
				return result, fmt.Errorf("disable TUN before enabling the system proxy: %w", disableErr)
			}
			if !disable.Released {
				return result, fmt.Errorf("TUN did not release its interfaces; recover the network first")
			}
		}
		running := false
		for _, candidate := range a.Cores.Kinds() {
			if a.Cores.Status(ctx, candidate).Running {
				running = true
				break
			}
		}
		if !running {
			return result, fmt.Errorf("no proxy core is running: start Mihomo or sing-box before enabling the system proxy")
		}
		target := a.OwnProxyAddress()
		if dialErr := proxyListening(target, 700*time.Millisecond); dialErr != nil {
			return result, fmt.Errorf("system proxy target %s is not listening (%v); start the core or fix its mixed-port before enabling the system proxy", target, dialErr)
		}
		networkResult, err := a.Network.SetMode(ctx, mode, a.desiredProxy())
		result.Network = networkResult
		return result, err

	case network.ModeOff, network.ModeManual:
		if status, err := a.TUN.Status(ctx); err == nil && status.Enabled {
			disable, disableErr := a.TUN.Disable(ctx)
			result.Warnings = append(result.Warnings, disable.Warnings...)
			if disableErr != nil {
				return result, fmt.Errorf("disable TUN: %w", disableErr)
			}
			if !disable.Released {
				return result, fmt.Errorf("TUN did not release its interfaces; run proxy network tun recover")
			}
		}
		networkResult, err := a.Network.SetMode(ctx, mode, a.desiredProxy())
		result.Network = networkResult
		return result, err
	default:
		return result, fmt.Errorf("unknown traffic mode %q", mode)
	}
}

// proxyListening verifies that the local proxy endpoint accepts TCP before the
// desktop proxy is pointed at it.
func proxyListening(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (a *App) desiredProxy() network.Desired {
	return network.Desired{
		Enabled:  true,
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     a.Runtime.ProxyPort,
		Bypass:   network.DefaultBypass(),
	}
}

// TUNStatus exposes the TUN lifecycle state.
func (a *App) TUNStatus(ctx context.Context) (tun.Status, error) {
	return a.TUN.Status(ctx)
}

// TUNPreflight runs the TUN environment checks.
func (a *App) TUNPreflight(ctx context.Context, kind model.CoreKind, settings tun.Settings) (tun.Report, error) {
	return a.TUN.Preflight(ctx, kind, settings)
}

// RecoverTUN repairs a TUN state left behind by an abnormal exit.
func (a *App) RecoverTUN(ctx context.Context) (tun.RecoveryResult, error) {
	return a.TUN.Recover(ctx)
}

// TunPlatform reports the platform used for TUN diagnostics.
func (a *App) TunPlatform() string {
	return tun.PlatformName()
}

// EnableSystemProxy points the desktop proxy at the local core.
func (a *App) EnableSystemProxy(ctx context.Context) (network.Result, error) {
	result, err := a.ApplyTrafficMode(ctx, network.ModeSystemProxy, "", TunSettingsFrom(a.Settings))
	return result.Network, err
}

// DisableSystemProxy restores the desktop proxy settings captured earlier.
func (a *App) DisableSystemProxy(ctx context.Context) (network.Result, error) {
	result, err := a.ApplyTrafficMode(ctx, network.ModeOff, "", TunSettingsFrom(a.Settings))
	return result.Network, err
}

// OwnProxyAddress is the loopback address ProxyTUI installs as system proxy.
func (a *App) OwnProxyAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", a.Runtime.ProxyPort)
}

// NetworkReport describes the local network takeover state.
func (a *App) NetworkReport(ctx context.Context) (network.Report, error) {
	return a.Network.Report(ctx, a.OwnProxyAddress())
}

// RecoverNetwork repairs leftover network state using the current core state.
func (a *App) RecoverNetwork(ctx context.Context) (network.RecoveryResult, error) {
	coreRunning := false
	for _, kind := range a.Cores.Kinds() {
		if a.Cores.Status(ctx, kind).Running {
			coreRunning = true
			break
		}
	}
	return a.Network.Recover(ctx, coreRunning)
}

// BuildInfo summarises the binary and platform.
func BuildInfo() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s %s/%s · %s", Version, osName(), osArch(), host)
}

// SaveSettings persists the current settings.
func (a *App) SaveSettings() error {
	return storage.SaveSettings(a.Paths, a.Settings)
}

// Import runs a subscription import.
func (a *App) Import(ctx context.Context, req subscription.ImportRequest) ([]subscription.ImportOutcome, error) {
	return a.Subs.Import(ctx, req)
}

// StartCore launches the active profile of a core.
func (a *App) StartCore(ctx context.Context, kind model.CoreKind) error {
	profile, ok := a.Subs.ActiveFor(kind)
	if !ok {
		return fmt.Errorf("no active %s profile: open the %s page and press enter on a profile", kind.DisplayName(), kind.DisplayName())
	}
	config, _, err := a.Subs.ActiveConfig(profile.ID)
	if err != nil {
		return err
	}
	return a.Cores.Start(ctx, kind, core.StartOptions{
		ProfileID:   profile.ID,
		ProfileName: profile.Label(),
		Config:      config,
	})
}

// RestartCore restarts a core with the currently active profile.
func (a *App) RestartCore(ctx context.Context, kind model.CoreKind) error {
	profile, ok := a.Subs.ActiveFor(kind)
	if !ok {
		return fmt.Errorf("no active %s profile", kind.DisplayName())
	}
	config, _, err := a.Subs.ActiveConfig(profile.ID)
	if err != nil {
		return err
	}
	return a.Cores.Restart(ctx, kind, core.StartOptions{
		ProfileID:   profile.ID,
		ProfileName: profile.Label(),
		Config:      config,
	})
}

// Doctor collects local diagnostics for the CLI and the TUI.
func (a *App) Doctor(ctx context.Context) []string {
	lines := []string{
		"proxy-tui " + Version,
		"platform: " + osName() + "/" + osArch(),
		"config:   " + a.Paths.Config,
		"data:     " + a.Paths.Data,
		"state:    " + a.Paths.State,
	}
	for _, path := range []string{a.Paths.Config, a.Paths.Data, a.Paths.State} {
		if writable(path) {
			lines = append(lines, "ok: "+path+" is writable")
		} else {
			lines = append(lines, "FAIL: "+path+" is not writable")
		}
	}
	for _, kind := range a.Cores.Kinds() {
		status := a.Cores.Status(ctx, kind)
		state := "not installed"
		switch {
		case status.Running:
			state = "running (pid " + fmt.Sprintf("%d", status.PID) + ")"
		case status.Installed:
			state = "installed"
		}
		line := fmt.Sprintf("%s: %s, version %s, %s", kind.DisplayName(), state, emptyDash(status.Version), emptyDash(status.BinaryPath))
		lines = append(lines, line)
	}
	profiles := a.Subs.List()
	lines = append(lines, fmt.Sprintf("profiles: %d", len(profiles)))
	for _, kind := range a.Cores.Kinds() {
		active, ok := a.Subs.ActiveFor(kind)
		if ok {
			lines = append(lines, fmt.Sprintf("%s active profile: %s (revision %d, %d nodes)", kind.DisplayName(), active.Label(), active.Revision, active.NodeCount))
		} else {
			lines = append(lines, fmt.Sprintf("%s active profile: none", kind.DisplayName()))
		}
	}
	for _, profile := range profiles {
		if profile.LastUpdate != nil && !profile.LastUpdate.OK {
			lines = append(lines, fmt.Sprintf("warning: %s last update failed: %s", profile.Label(), profile.LastUpdate.Error))
		}
	}
	if report, err := a.NetworkReport(ctx); err == nil {
		if report.Capabilities.Supported {
			state := "disabled"
			if report.Desktop.Enabled {
				state = "enabled " + report.Desktop.Address()
			}
			lines = append(lines, fmt.Sprintf("desktop proxy (%s): %s", report.Capabilities.Platform, state))
		} else {
			lines = append(lines, "desktop proxy: unsupported ("+report.Capabilities.Reason+")")
		}
		if report.Environment.Detected() {
			lines = append(lines, "shell proxy environment: set (HTTP_PROXY/HTTPS_PROXY); not managed by proxy-tui")
		}
		lines = append(lines, fmt.Sprintf("network backup: %v", report.BackupActive))
	}
	lines = append(lines, "note: TUN and DNS takeover are handled by the core and are not implemented yet (Phase 3C)")
	return lines
}

func writable(path string) bool {
	file, err := os.CreateTemp(path, ".write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func emptyDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// CoreBinaryPath exposes the installed core path for diagnostics.
func (a *App) CoreBinaryPath(kind model.CoreKind) string {
	_, binary, ok := core.NewLayout(a.Paths, kind).Installed()
	if !ok {
		return ""
	}
	return binary
}

// ProfileCount returns the number of stored profiles.
func (a *App) ProfileCount() int { return len(a.Subs.List()) }

// RuntimeDir is where generated runtime configs live.
func (a *App) RuntimeDir() string { return filepath.Join(a.Paths.RuntimeDir()) }
