package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// CoreService is the core management surface used by clients.
type CoreService interface {
	Kinds() []model.CoreKind
	Status(ctx context.Context, kind model.CoreKind) core.Status
	Install(ctx context.Context, kind model.CoreKind, progress func(string)) (core.InstallResult, error)
	Start(ctx context.Context, kind model.CoreKind, opts core.StartOptions) error
	Stop(ctx context.Context, kind model.CoreKind) error
	Proxies(ctx context.Context, kind model.CoreKind) ([]core.Proxy, error)
	SelectProxy(ctx context.Context, kind model.CoreKind, group, name string) error
	TestDelay(ctx context.Context, kind model.CoreKind, name string, timeout time.Duration) (int, error)
	Connections(ctx context.Context, kind model.CoreKind) ([]core.Connection, error)
	Traffic(ctx context.Context, kind model.CoreKind) (core.Traffic, error)
}

// ProfileService is the profile management surface used by clients.
type ProfileService interface {
	List() []model.Profile
	Get(id string) (model.Profile, bool)
	ActiveFor(kind model.CoreKind) (model.Profile, bool)
	ActiveConfig(id string) ([]byte, string, error)
	SetActive(id string) (model.Profile, error)
	Delete(id string) error
	Update(ctx context.Context, id string) (model.Profile, error)
	UpdateAll(ctx context.Context) []subscription.UpdateOutcome
	Due(now time.Time) []model.Profile
	SetDefaultInterval(seconds int)
	Snapshots(id string) ([]model.Snapshot, error)
}

// TUNService is the TUN surface used by clients.
type TUNService interface {
	Disable(ctx context.Context) (tun.DisableResult, error)
}

// LogService is the log surface used by clients.
type LogService interface {
	Tail(n int) []logging.Entry
	Clear()
}

// FrontendAPI is the application-level operation surface used by clients.
type FrontendAPI interface {
	StartCore(ctx context.Context, kind model.CoreKind) error
	StopCore(ctx context.Context, kind model.CoreKind) error
	RestartCore(ctx context.Context, kind model.CoreKind) error
	Import(ctx context.Context, req subscription.ImportRequest) ([]subscription.ImportOutcome, error)
	DefaultTunCore(ctx context.Context) model.CoreKind
	ApplyTrafficMode(ctx context.Context, mode network.Mode, kind model.CoreKind, settings tun.Settings) (TrafficResult, error)
	NetworkReport(ctx context.Context) (network.Report, error)
	TUNStatus(ctx context.Context) (tun.Status, error)
	TUNPreflight(ctx context.Context, kind model.CoreKind, settings tun.Settings) (tun.Report, error)
	RecoverTUN(ctx context.Context) (tun.RecoveryResult, error)
	RecoverNetwork(ctx context.Context) (network.RecoveryResult, error)
	Doctor(ctx context.Context) []string
	TunPlatform() string
	OwnProxyAddress() string
	VerifyProxyConnectivity(ctx context.Context) ProxyConnectivity
	CoreBinaryPath(kind model.CoreKind) string
	SettingsSnapshot(ctx context.Context) storage.Settings
	UpdateSettings(ctx context.Context, settings storage.Settings) error
	PatchSettings(ctx context.Context, patch map[string]json.RawMessage) error
	Health(ctx context.Context) error
}

// Frontend is what the TUI and CLI depend on. It never requires a concrete
// application instance; the supervisor owns the local implementation.
type Frontend struct {
	Cores    CoreService
	Subs     ProfileService
	TUN      TUNService
	Logs     LogService
	Paths    storage.Paths
	Settings storage.Settings

	api FrontendAPI
}

// NewLocalFrontend builds a frontend backed by a local application. It is for
// the supervisor only; clients must use the remote implementation.
func NewLocalFrontend(application *App) *Frontend {
	return &Frontend{
		Cores:    application.Cores,
		Subs:     application.Subs,
		TUN:      application.TUN,
		Logs:     application.Logs,
		Paths:    application.Paths,
		Settings: application.Settings,
		api:      application,
	}
}

func (f *Frontend) StartCore(ctx context.Context, kind model.CoreKind) error {
	return f.api.StartCore(ctx, kind)
}
func (f *Frontend) StopCore(ctx context.Context, kind model.CoreKind) error {
	return f.api.StopCore(ctx, kind)
}
func (f *Frontend) RestartCore(ctx context.Context, kind model.CoreKind) error {
	return f.api.RestartCore(ctx, kind)
}
func (f *Frontend) Import(ctx context.Context, req subscription.ImportRequest) ([]subscription.ImportOutcome, error) {
	return f.api.Import(ctx, req)
}
func (f *Frontend) DefaultTunCore(ctx context.Context) model.CoreKind {
	return f.api.DefaultTunCore(ctx)
}
func (f *Frontend) ApplyTrafficMode(ctx context.Context, mode network.Mode, kind model.CoreKind, settings tun.Settings) (TrafficResult, error) {
	return f.api.ApplyTrafficMode(ctx, mode, kind, settings)
}
func (f *Frontend) NetworkReport(ctx context.Context) (network.Report, error) {
	return f.api.NetworkReport(ctx)
}
func (f *Frontend) TUNStatus(ctx context.Context) (tun.Status, error) {
	return f.api.TUNStatus(ctx)
}
func (f *Frontend) TUNPreflight(ctx context.Context, kind model.CoreKind, settings tun.Settings) (tun.Report, error) {
	return f.api.TUNPreflight(ctx, kind, settings)
}
func (f *Frontend) RecoverTUN(ctx context.Context) (tun.RecoveryResult, error) {
	return f.api.RecoverTUN(ctx)
}
func (f *Frontend) RecoverNetwork(ctx context.Context) (network.RecoveryResult, error) {
	return f.api.RecoverNetwork(ctx)
}
func (f *Frontend) Doctor(ctx context.Context) []string { return f.api.Doctor(ctx) }
func (f *Frontend) TunPlatform() string                 { return f.api.TunPlatform() }
func (f *Frontend) OwnProxyAddress() string             { return f.api.OwnProxyAddress() }
func (f *Frontend) VerifyProxyConnectivity(ctx context.Context) ProxyConnectivity {
	return f.api.VerifyProxyConnectivity(ctx)
}
func (f *Frontend) CoreBinaryPath(kind model.CoreKind) string { return f.api.CoreBinaryPath(kind) }
func (f *Frontend) Health(ctx context.Context) error          { return f.api.Health(ctx) }

func (f *Frontend) SettingsSnapshot(ctx context.Context) storage.Settings {
	return f.api.SettingsSnapshot(ctx)
}

func (f *Frontend) PatchSettings(ctx context.Context, patch map[string]json.RawMessage) error {
	return f.api.PatchSettings(ctx, patch)
}

func (f *Frontend) SaveSettings(ctx context.Context, settings storage.Settings) error {
	if err := f.api.UpdateSettings(ctx, settings); err != nil {
		return err
	}
	f.Settings = settings
	return nil
}
