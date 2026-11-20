package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// ConfigInfo is the client-visible configuration snapshot.
type ConfigInfo struct {
	Paths    storage.Paths    `json:"paths"`
	Settings storage.Settings `json:"settings"`
}

// Facade exposes the operations clients may invoke through the supervisor.
// It intentionally contains no free-form path or command parameters.
type Facade struct {
	app *App
}

// NewFacade wraps the local application.
func NewFacade(application *App) *Facade { return &Facade{app: application} }

func (f *Facade) Ping(ctx context.Context) error { return nil }

func (f *Facade) ConfigInfo() ConfigInfo {
	return ConfigInfo{Paths: f.app.Paths, Settings: f.app.settingsCopy()}
}

func (f *Facade) SettingsSnapshot(ctx context.Context) storage.Settings {
	return f.app.SettingsSnapshot(ctx)
}

func (f *Facade) UpdateSettings(ctx context.Context, settings storage.Settings) error {
	return f.app.UpdateSettings(ctx, settings)
}

func (f *Facade) PatchSettings(ctx context.Context, patch map[string]json.RawMessage) error {
	return f.app.PatchSettings(ctx, patch)
}

func (f *Facade) CoreKinds() []model.CoreKind { return f.app.Cores.Kinds() }
func (f *Facade) CoreStatus(ctx context.Context, kind model.CoreKind) core.Status {
	return f.app.Cores.Status(ctx, kind)
}
func (f *Facade) CoreInstall(ctx context.Context, kind model.CoreKind) (core.InstallResult, error) {
	return f.app.Cores.Install(ctx, kind, nil)
}
func (f *Facade) CoreStart(ctx context.Context, kind model.CoreKind) error {
	if f.app.Cores.Status(ctx, kind).Running {
		return fmt.Errorf("CORE_ALREADY_RUNNING: %s is already running", kind.DisplayName())
	}
	return f.app.StartCore(ctx, kind)
}
func (f *Facade) CoreStop(ctx context.Context, kind model.CoreKind) error {
	if !f.app.Cores.Status(ctx, kind).Running {
		return nil
	}
	return f.app.StopCore(ctx, kind)
}
func (f *Facade) CoreRestart(ctx context.Context, kind model.CoreKind) error {
	return f.app.RestartCore(ctx, kind)
}
func (f *Facade) CoreProxies(ctx context.Context, kind model.CoreKind) ([]core.Proxy, error) {
	return f.app.Cores.Proxies(ctx, kind)
}
func (f *Facade) CoreSelect(ctx context.Context, kind model.CoreKind, group, name string) error {
	return f.app.Cores.SelectProxy(ctx, kind, group, name)
}
func (f *Facade) CoreTestDelay(ctx context.Context, kind model.CoreKind, name string, timeout time.Duration) (int, error) {
	return f.app.Cores.TestDelay(ctx, kind, name, timeout)
}
func (f *Facade) CoreConnections(ctx context.Context, kind model.CoreKind) ([]core.Connection, error) {
	return f.app.Cores.Connections(ctx, kind)
}
func (f *Facade) CoreTraffic(ctx context.Context, kind model.CoreKind) (core.Traffic, error) {
	return f.app.Cores.Traffic(ctx, kind)
}
func (f *Facade) CoreBinaryPath(kind model.CoreKind) string { return f.app.CoreBinaryPath(kind) }

// ProfileLookup preserves the found flag across IPC.
type ProfileLookup struct {
	Profile model.Profile `json:"profile"`
	Found   bool          `json:"found"`
}

// ActiveConfigInfo preserves the config format across IPC.
type ActiveConfigInfo struct {
	Data   []byte `json:"data"`
	Format string `json:"format"`
}

func (f *Facade) SubsList() []model.Profile { return f.app.Subs.List() }
func (f *Facade) SubsGetLookup(id string) ProfileLookup {
	profile, found := f.app.Subs.Get(id)
	return ProfileLookup{Profile: profile, Found: found}
}
func (f *Facade) SubsActiveLookup(kind model.CoreKind) ProfileLookup {
	profile, found := f.app.Subs.ActiveFor(kind)
	return ProfileLookup{Profile: profile, Found: found}
}
func (f *Facade) SubsActiveConfigInfo(id string) (ActiveConfigInfo, error) {
	data, format, err := f.app.Subs.ActiveConfig(id)
	return ActiveConfigInfo{Data: data, Format: format}, err
}
func (f *Facade) SubsGet(id string) (model.Profile, bool) {
	return f.app.Subs.Get(id)
}
func (f *Facade) SubsActiveFor(kind model.CoreKind) (model.Profile, bool) {
	return f.app.Subs.ActiveFor(kind)
}
func (f *Facade) SubsActiveConfig(id string) ([]byte, string, error) {
	data, format, err := f.app.Subs.ActiveConfig(id)
	return data, format, err
}
func (f *Facade) SubsSetActive(id string) (model.Profile, error) { return f.app.Subs.SetActive(id) }
func (f *Facade) SubsDelete(id string) error                     { return f.app.Subs.Delete(id) }
func (f *Facade) SubsUpdate(ctx context.Context, id string) (model.Profile, error) {
	return f.app.Subs.Update(ctx, id)
}
func (f *Facade) SubsUpdateAll(ctx context.Context) []subscription.UpdateOutcome {
	return f.app.Subs.UpdateAll(ctx)
}
func (f *Facade) SubsDue(now time.Time) []model.Profile { return f.app.Subs.Due(now) }
func (f *Facade) SubsSetDefaultInterval(seconds int)    { f.app.Subs.SetDefaultInterval(seconds) }
func (f *Facade) SubsSnapshots(id string) ([]model.Snapshot, error) {
	return f.app.Subs.Snapshots(id)
}

func (f *Facade) Import(ctx context.Context, req subscription.ImportRequest) ([]subscription.ImportOutcome, error) {
	return f.app.Import(ctx, req)
}
func (f *Facade) DefaultTunCore(ctx context.Context) model.CoreKind {
	return f.app.DefaultTunCore(ctx)
}
func (f *Facade) ApplyTrafficMode(ctx context.Context, mode network.Mode, kind model.CoreKind, settings tun.Settings) (TrafficResult, error) {
	return f.app.ApplyTrafficMode(ctx, mode, kind, settings)
}
func (f *Facade) NetworkReport(ctx context.Context) (network.Report, error) {
	return f.app.NetworkReport(ctx)
}
func (f *Facade) TUNStatus(ctx context.Context) (tun.Status, error) { return f.app.TUNStatus(ctx) }
func (f *Facade) TUNPreflight(ctx context.Context, kind model.CoreKind, settings tun.Settings) (tun.Report, error) {
	return f.app.TUNPreflight(ctx, kind, settings)
}
func (f *Facade) RecoverTUN(ctx context.Context) (tun.RecoveryResult, error) {
	return f.app.RecoverTUN(ctx)
}
func (f *Facade) RecoverNetwork(ctx context.Context) (network.RecoveryResult, error) {
	return f.app.RecoverNetwork(ctx)
}
func (f *Facade) TUNDisable(ctx context.Context) (tun.DisableResult, error) {
	return f.app.TUN.Disable(ctx)
}
func (f *Facade) Doctor(ctx context.Context) []string { return f.app.Doctor(ctx) }
func (f *Facade) TunPlatform() string                 { return f.app.TunPlatform() }
func (f *Facade) OwnProxyAddress() string             { return f.app.OwnProxyAddress() }
func (f *Facade) VerifyProxyConnectivity(ctx context.Context) ProxyConnectivity {
	return f.app.VerifyProxyConnectivity(ctx)
}
func (f *Facade) LogsTail(n int) []logging.Entry { return f.app.Logs.Tail(n) }
func (f *Facade) LogsClear()                     { f.app.Logs.Clear() }
