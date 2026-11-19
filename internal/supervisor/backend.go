package supervisor

import (
	"context"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// Backend is the supervisor's view of the application. Tests use fakes.
type Backend interface {
	Statuses(ctx context.Context) map[model.CoreKind]core.Status
	StartCore(ctx context.Context, kind model.CoreKind) error
	StopCore(ctx context.Context, kind model.CoreKind) error
	RestartCore(ctx context.Context, kind model.CoreKind) error
	Proxies(ctx context.Context, kind model.CoreKind) ([]core.Proxy, error)
	SelectProxy(ctx context.Context, kind model.CoreKind, group, name string) error
	NetworkReport(ctx context.Context) (network.Report, error)
	TUNStatus(ctx context.Context) (tun.Status, error)
	LogsTail(n int) []logging.Entry
}

// AppBackend adapts the application layer.
type AppBackend struct {
	app *app.App
}

// NewAppBackend wraps an App.
func NewAppBackend(application *app.App) *AppBackend {
	return &AppBackend{app: application}
}

func (b *AppBackend) Statuses(ctx context.Context) map[model.CoreKind]core.Status {
	out := map[model.CoreKind]core.Status{}
	for _, kind := range b.app.Cores.Kinds() {
		out[kind] = b.app.Cores.Status(ctx, kind)
	}
	return out
}

func (b *AppBackend) StartCore(ctx context.Context, kind model.CoreKind) error {
	return b.app.StartCore(ctx, kind)
}

func (b *AppBackend) StopCore(ctx context.Context, kind model.CoreKind) error {
	return b.app.StopCore(ctx, kind)
}

func (b *AppBackend) RestartCore(ctx context.Context, kind model.CoreKind) error {
	return b.app.RestartCore(ctx, kind)
}

func (b *AppBackend) Proxies(ctx context.Context, kind model.CoreKind) ([]core.Proxy, error) {
	return b.app.Cores.Proxies(ctx, kind)
}

func (b *AppBackend) SelectProxy(ctx context.Context, kind model.CoreKind, group, name string) error {
	return b.app.Cores.SelectProxy(ctx, kind, group, name)
}

func (b *AppBackend) NetworkReport(ctx context.Context) (network.Report, error) {
	return b.app.NetworkReport(ctx)
}

func (b *AppBackend) TUNStatus(ctx context.Context) (tun.Status, error) {
	return b.app.TUNStatus(ctx)
}

func (b *AppBackend) LogsTail(n int) []logging.Entry {
	return b.app.Logs.Tail(n)
}
