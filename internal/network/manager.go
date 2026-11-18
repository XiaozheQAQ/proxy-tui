package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// Manager applies and reverts network changes transactionally.
type Manager struct {
	paths    storage.Paths
	logf     func(level, msg string)
	adapters []Adapter

	mu  sync.Mutex
	now func() time.Time
}

// NewManager wires the platform adapters, most preferred first.
func NewManager(paths storage.Paths, logf func(level, msg string), adapters ...Adapter) *Manager {
	if logf == nil {
		logf = func(string, string) {}
	}
	return &Manager{paths: paths, logf: logf, adapters: adapters, now: time.Now}
}

// Result describes one completed network change.
type Result struct {
	Applied    bool
	Restored   bool
	Adapter    string
	State      ProxyState
	Original   ProxyState
	RolledBack bool
}

func (m *Manager) adapterByName(name string) Adapter {
	for _, adapter := range m.adapters {
		if adapter.Name() == name {
			return adapter
		}
	}
	return nil
}

// primary picks the first adapter that is usable on this platform.
func (m *Manager) primary(ctx context.Context) (Adapter, Capabilities, error) {
	var reasons []string
	for _, adapter := range m.adapters {
		caps := adapter.Capabilities(ctx)
		if caps.Supported {
			return adapter, caps, nil
		}
		if caps.Reason != "" {
			reasons = append(reasons, caps.Platform+": "+caps.Reason)
		}
	}
	if len(reasons) == 0 {
		return nil, Capabilities{Platform: platformName()}, ErrUnsupported
	}
	return nil, Capabilities{Platform: platformName(), Reason: joinReasons(reasons)}, fmt.Errorf("%w: %s", ErrUnsupported, joinReasons(reasons))
}

func joinReasons(reasons []string) string {
	out := ""
	for i, reason := range reasons {
		if i > 0 {
			out += "; "
		}
		out += reason
	}
	return out
}

// EnableSystemProxy installs the desktop proxy and records how to undo it.
func (m *Manager) EnableSystemProxy(ctx context.Context, desired Desired) (Result, error) {
	desired.Enabled = true
	if desired.Protocol == "" {
		desired.Protocol = "http"
	}
	if desired.Bypass == "" {
		desired.Bypass = DefaultBypass()
	}
	if err := desired.Validate(); err != nil {
		return Result{}, err
	}

	adapter, _, err := m.primary(ctx)
	if err != nil {
		return Result{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, err := m.readBackup()
	if err != nil {
		return Result{}, err
	}
	hadBackup := existing != nil

	var snapshot Snapshot
	if !hadBackup {
		snapshot, err = adapter.Snapshot(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("capture original desktop proxy settings: %w", err)
		}
		if err := m.writeJournal(transactionJournal{
			Phase:     phasePrepared,
			Adapter:   adapter.Name(),
			StartedAt: m.now(),
			Desired:   desired,
			Snapshot:  snapshot,
		}); err != nil {
			return Result{}, err
		}
	}

	rollback := func(cause error) (Result, error) {
		if hadBackup {
			return Result{Adapter: adapter.Name()}, cause
		}
		restoreErr := adapter.Restore(ctx, snapshot)
		state, _ := adapter.GetState(ctx)
		entry := transactionJournal{
			Phase:     phaseRolledBack,
			Adapter:   adapter.Name(),
			StartedAt: m.now(),
			Desired:   desired,
			Snapshot:  snapshot,
			Error:     cause.Error(),
		}
		if writeErr := m.writeJournal(entry); writeErr != nil {
			m.logf("warn", "persist network journal: "+writeErr.Error())
		}
		if restoreErr != nil {
			return Result{Adapter: adapter.Name(), State: state, RolledBack: false},
				fmt.Errorf("apply desktop proxy failed: %w (rollback also failed: %v)", cause, restoreErr)
		}
		m.logf("warn", "desktop proxy rolled back after failure: "+cause.Error())
		return Result{Adapter: adapter.Name(), State: state, RolledBack: true}, cause
	}

	if err := adapter.Apply(ctx, desired); err != nil {
		return rollback(err)
	}
	if err := adapter.Verify(ctx, desired); err != nil {
		return rollback(fmt.Errorf("desktop proxy did not take effect: %w", err))
	}

	if hadBackup {
		existing.Desired = desired
		existing.At = m.now()
		if err := m.writeBackup(*existing); err != nil {
			return Result{}, err
		}
	} else {
		if err := m.writeBackup(Backup{
			At:       m.now(),
			Adapter:  adapter.Name(),
			Desired:  desired,
			Snapshot: snapshot,
			Original: snapshot.Proxy,
		}); err != nil {
			return Result{}, err
		}
	}
	if err := m.removeJournal(); err != nil {
		m.logf("warn", "remove network journal: "+err.Error())
	}

	state, err := adapter.GetState(ctx)
	if err != nil {
		state = ProxyState{Enabled: true, Protocol: desired.Protocol, Host: desired.Host, Port: desired.Port, Bypass: desired.Bypass}
	}
	if !hadBackup {
		m.logf("info", fmt.Sprintf("desktop proxy enabled: %s (%s)", desired.Address(), adapter.Name()))
	}
	return Result{Applied: true, Adapter: adapter.Name(), State: state, Original: snapshot.Proxy}, nil
}

// DisableSystemProxy restores the desktop settings captured before enabling.
func (m *Manager) DisableSystemProxy(ctx context.Context) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	backup, err := m.readBackup()
	if err != nil {
		return Result{}, err
	}
	if backup == nil {
		adapter, _, primaryErr := m.primary(ctx)
		if primaryErr != nil {
			return Result{Restored: true}, nil
		}
		state, stateErr := adapter.GetState(ctx)
		if stateErr != nil {
			return Result{Adapter: adapter.Name()}, stateErr
		}
		if state.Enabled {
			return Result{Adapter: adapter.Name(), State: state}, ErrNoBackup
		}
		return Result{Restored: true, Adapter: adapter.Name(), State: state}, nil
	}

	adapter := m.adapterByName(backup.Adapter)
	if adapter == nil {
		return Result{}, fmt.Errorf("the desktop proxy was changed by %q, which is unavailable on this platform", backup.Adapter)
	}
	if err := m.writeJournal(transactionJournal{
		Phase:     phaseRestoring,
		Adapter:   adapter.Name(),
		StartedAt: m.now(),
		Snapshot:  backup.Snapshot,
	}); err != nil {
		return Result{}, err
	}
	if err := adapter.Restore(ctx, backup.Snapshot); err != nil {
		return Result{Adapter: adapter.Name()}, fmt.Errorf("restore original desktop proxy settings: %w", err)
	}
	state, stateErr := adapter.GetState(ctx)
	if stateErr != nil {
		m.logf("warn", "read desktop proxy after restore: "+stateErr.Error())
	}
	if err := m.removeJournal(); err != nil {
		m.logf("warn", "remove network journal: "+err.Error())
	}
	if err := m.removeBackup(); err != nil {
		m.logf("warn", "remove network backup: "+err.Error())
	}
	m.logf("info", fmt.Sprintf("desktop proxy restored via %s", adapter.Name()))
	return Result{Restored: true, Adapter: adapter.Name(), State: state, Original: backup.Original}, nil
}

// SetMode applies a traffic takeover mode. TUN is declared but not wired yet.
func (m *Manager) SetMode(ctx context.Context, mode Mode, desired Desired) (Result, error) {
	switch mode {
	case ModeOff, ModeManual:
		return m.DisableSystemProxy(ctx)
	case ModeSystemProxy:
		return m.EnableSystemProxy(ctx, desired)
	case ModeTUN:
		return Result{}, ErrTUNUnsupported
	default:
		return Result{}, fmt.Errorf("unknown traffic mode %q", mode)
	}
}

// Report gathers the current state without changing anything. ownAddress is
// the address ProxyTUI would install; an active proxy at another address is
// reported as a conflict so the original settings are preserved.
func (m *Manager) Report(ctx context.Context, ownAddress string) (Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	report := Report{
		Mode:        ModeOff,
		Environment: DetectEnvironment(),
		Adapters:    make([]Capabilities, 0, len(m.adapters)),
	}
	for _, adapter := range m.adapters {
		report.Adapters = append(report.Adapters, adapter.Capabilities(ctx))
	}

	adapter, caps, err := m.primary(ctx)
	if err != nil {
		report.Capabilities = Capabilities{Platform: platformName(), Supported: false, Reason: err.Error()}
		if backup, backupErr := m.readBackup(); backupErr == nil && backup != nil {
			report.Mode = ModeSystemProxy
			report.BackupActive = true
			report.BackupAt = backup.At
			report.BackupOriginal = backup.Original
		}
		report.Notes = append(report.Notes, "TUN is provided by the proxy core (Phase 3C)")
		return report, nil
	}

	report.Capabilities = caps
	if state, stateErr := adapter.GetState(ctx); stateErr != nil {
		report.Notes = append(report.Notes, "desktop proxy state unavailable: "+stateErr.Error())
	} else {
		report.Desktop = state
		if state.Enabled && ownAddress != "" && state.Address() != ownAddress {
			report.ForeignProxy = true
			report.ForeignProxyDetail = "another proxy client owns the desktop proxy (" + state.Address() + ")"
			report.Notes = append(report.Notes, report.ForeignProxyDetail+"; enabling backs it up first and disabling restores it")
		}
	}
	if backup, backupErr := m.readBackup(); backupErr == nil && backup != nil {
		report.Mode = ModeSystemProxy
		report.BackupActive = true
		report.BackupAt = backup.At
		report.BackupOriginal = backup.Original
	}
	if !TUNAvailable() {
		report.Notes = append(report.Notes, "TUN is provided by the proxy core (Phase 3C)")
	}
	return report, nil
}

// RecoveryResult describes what Recover did.
type RecoveryResult struct {
	Action   string
	Restored bool
	Detail   string
}

// Recover repairs network state left behind by an interrupted process or a
// stopped core. It is safe to call on every start.
func (m *Manager) Recover(ctx context.Context, coreRunning bool) (RecoveryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.readJournal(); ok {
		adapter := m.adapterByName(entry.Adapter)
		if adapter == nil {
			return RecoveryResult{}, fmt.Errorf("journal references unknown adapter %q", entry.Adapter)
		}
		if err := adapter.Restore(ctx, entry.Snapshot); err != nil {
			return RecoveryResult{}, fmt.Errorf("recover interrupted network change: %w", err)
		}
		if err := m.removeJournal(); err != nil {
			m.logf("warn", "remove network journal: "+err.Error())
		}
		m.logf("warn", "restored network state after an interrupted change")
		return RecoveryResult{Action: "restored an interrupted network change", Restored: true}, nil
	}

	backup, err := m.readBackup()
	if err != nil {
		return RecoveryResult{}, err
	}
	if backup == nil {
		return RecoveryResult{}, nil
	}
	if coreRunning {
		return RecoveryResult{
			Action: "desktop proxy still points at the running core",
			Detail: "stop the core or disable the system proxy to restore the original settings",
		}, nil
	}

	adapter := m.adapterByName(backup.Adapter)
	if adapter == nil {
		return RecoveryResult{}, fmt.Errorf("backup references unknown adapter %q", backup.Adapter)
	}
	current, err := adapter.GetState(ctx)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("read desktop proxy before recovery: %w", err)
	}
	wanted := backup.Desired.Address()
	if !current.Enabled || current.Address() != wanted {
		// Another program changed the proxy after ProxyTUI applied its own; the
		// backup is stale and restoring it would overwrite someone else's
		// settings. Leave the current state untouched.
		return RecoveryResult{
			Action: "desktop proxy is no longer managed by proxy-tui",
			Detail: fmt.Sprintf("current=%s, backup=%s: leaving the current settings untouched", emptyOr(current.Address(), "disabled"), wanted),
		}, nil
	}
	if err := adapter.Restore(ctx, backup.Snapshot); err != nil {
		return RecoveryResult{}, fmt.Errorf("restore desktop proxy after core stop: %w", err)
	}
	if err := m.removeBackup(); err != nil {
		m.logf("warn", "remove network backup: "+err.Error())
	}
	m.logf("warn", "restored desktop proxy after the core stopped")
	return RecoveryResult{Action: "restored the desktop proxy after the core stopped", Restored: true}, nil
}

// Ensure errors.Is works for callers that only care about the class.
func IsUnsupported(err error) bool { return errors.Is(err, ErrUnsupported) }

func emptyOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
