package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Manager owns the adapters and serialises start/stop operations.
type Manager struct {
	paths    storage.Paths
	logs     *logging.Buffer
	adapters map[model.CoreKind]Core

	mu   sync.Mutex
	busy map[model.CoreKind]bool
}

// NewManager wires the supplied adapters.
func NewManager(paths storage.Paths, logs *logging.Buffer, adapters ...Core) *Manager {
	m := &Manager{
		paths:    paths,
		logs:     logs,
		adapters: make(map[model.CoreKind]Core, len(adapters)),
		busy:     map[model.CoreKind]bool{},
	}
	for _, adapter := range adapters {
		m.adapters[adapter.Kind()] = adapter
	}
	return m
}

// Paths exposes the storage layout.
func (m *Manager) Paths() storage.Paths { return m.paths }

// Adapter returns the adapter for a core.
func (m *Manager) Adapter(kind model.CoreKind) (Core, error) {
	adapter, ok := m.adapters[kind]
	if !ok {
		return nil, fmt.Errorf("core %q is not available in this build", kind)
	}
	return adapter, nil
}

// Kinds lists the wired cores in display order.
func (m *Manager) Kinds() []model.CoreKind {
	out := make([]model.CoreKind, 0, len(m.adapters))
	for _, kind := range model.AllCores {
		if _, ok := m.adapters[kind]; ok {
			out = append(out, kind)
		}
	}
	return out
}

// Status returns the status of one core.
func (m *Manager) Status(ctx context.Context, kind model.CoreKind) Status {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return Status{Core: kind, LastError: err.Error()}
	}
	return adapter.Status(ctx)
}

// Statuses returns every core status.
func (m *Manager) Statuses(ctx context.Context) []Status {
	kinds := m.Kinds()
	out := make([]Status, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, m.Status(ctx, kind))
	}
	return out
}

func (m *Manager) claim(kind model.CoreKind) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[kind] {
		return false
	}
	m.busy[kind] = true
	return true
}

func (m *Manager) release(kind model.CoreKind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.busy, kind)
}

func (m *Manager) logf(level, msg string) {
	if m.logs != nil {
		m.logs.Add(level, "core", msg)
	}
}

// Install downloads a core release.
func (m *Manager) Install(ctx context.Context, kind model.CoreKind, progress func(string)) (InstallResult, error) {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return InstallResult{}, err
	}
	if !adapter.Capabilities().Install {
		return InstallResult{}, ErrUnsupported
	}
	if !m.claim(kind) {
		return InstallResult{}, fmt.Errorf("another operation on %s is already running", kind.DisplayName())
	}
	defer m.release(kind)
	return adapter.Install(ctx, progress)
}

// Start launches a core with the supplied config, stopping the other core
// first because both cores would otherwise fight over the same local ports.
func (m *Manager) Start(ctx context.Context, kind model.CoreKind, opts StartOptions) error {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return err
	}
	if !m.claim(kind) {
		return fmt.Errorf("another operation on %s is already running", kind.DisplayName())
	}
	defer m.release(kind)

	for otherKind, other := range m.adapters {
		if otherKind == kind {
			continue
		}
		if other.Status(ctx).Running {
			m.logf("info", fmt.Sprintf("stopping %s before starting %s", otherKind.DisplayName(), kind.DisplayName()))
			if err := other.Stop(ctx); err != nil {
				m.logf("warn", fmt.Sprintf("stop %s: %v", otherKind.DisplayName(), err))
			}
		}
	}
	return adapter.Start(ctx, opts)
}

// Stop stops a core.
func (m *Manager) Stop(ctx context.Context, kind model.CoreKind) error {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return err
	}
	if !m.claim(kind) {
		return fmt.Errorf("another operation on %s is already running", kind.DisplayName())
	}
	defer m.release(kind)
	return adapter.Stop(ctx)
}

// Restart stops and starts a core.
func (m *Manager) Restart(ctx context.Context, kind model.CoreKind, opts StartOptions) error {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return err
	}
	if err := adapter.Stop(ctx); err != nil {
		m.logf("warn", fmt.Sprintf("restart %s: stop failed: %v", kind.DisplayName(), err))
	}
	return m.Start(ctx, kind, opts)
}

// Validate checks a config with the core binary.
func (m *Manager) Validate(ctx context.Context, kind model.CoreKind, name string, data []byte) error {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return err
	}
	if !adapter.Capabilities().ValidateConfig {
		return ErrUnsupported
	}
	return adapter.ValidateConfig(ctx, name, data)
}

// Proxies returns nodes and groups of a running core.
func (m *Manager) Proxies(ctx context.Context, kind model.CoreKind) ([]Proxy, error) {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return nil, err
	}
	return adapter.Proxies(ctx)
}

// SelectProxy switches a proxy group.
func (m *Manager) SelectProxy(ctx context.Context, kind model.CoreKind, group, name string) error {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return err
	}
	return adapter.SelectProxy(ctx, group, name)
}

// TestDelay measures one node.
func (m *Manager) TestDelay(ctx context.Context, kind model.CoreKind, name string, timeout time.Duration) (int, error) {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return 0, err
	}
	return adapter.TestDelay(ctx, name, timeout)
}

// Connections lists live connections.
func (m *Manager) Connections(ctx context.Context, kind model.CoreKind) ([]Connection, error) {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return nil, err
	}
	return adapter.Connections(ctx)
}

// Traffic reads throughput counters.
func (m *Manager) Traffic(ctx context.Context, kind model.CoreKind) (Traffic, error) {
	adapter, err := m.Adapter(kind)
	if err != nil {
		return Traffic{}, err
	}
	return adapter.Traffic(ctx)
}
