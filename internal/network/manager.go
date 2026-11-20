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

	rollbackSnapshot := snapshot
	if hadBackup {
		rollbackSnapshot = existing.Snapshot
	}
	rollback := func(cause error) (Result, error) {
		// The caller's context may already be cancelled (IPC deadline). The
		// rollback must still get a bounded chance to restore the original
		// desktop proxy.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cleanupCancel()
		restoreErr := m.restoreAndVerify(cleanupCtx, adapter, rollbackSnapshot)
		state, _ := adapter.GetState(cleanupCtx)
		entry := transactionJournal{
			Phase:     phaseRolledBack,
			Adapter:   adapter.Name(),
			StartedAt: m.now(),
			Desired:   desired,
			Snapshot:  rollbackSnapshot,
			Error:     cause.Error(),
		}
		if writeErr := m.writeJournal(entry); writeErr != nil {
			m.logf("warn", "persist network journal: "+writeErr.Error())
		}
		if restoreErr != nil {
			return Result{Adapter: adapter.Name(), State: state, RolledBack: false},
				fmt.Errorf("apply desktop proxy failed: %w (rollback also failed: %v)", cause, restoreErr)
		}
		if hadBackup {
			// The previous owned state was restored; the stale backup must
			// not keep blocking supervisor stop.
			_ = m.removeBackup()
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

	// Mark the transaction as committing before the durable backup exists.
	// If we crash after the backup, recovery treats it as committed; if we
	// crash before it, recovery restores the original snapshot.
	if err := m.writeJournal(transactionJournal{
		Phase:     phaseCommitting,
		Adapter:   adapter.Name(),
		StartedAt: m.now(),
		Desired:   desired,
		Snapshot:  snapshot,
	}); err != nil {
		return Result{}, err
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
	current, err := adapter.GetState(ctx)
	if err != nil {
		return Result{Adapter: adapter.Name()}, fmt.Errorf("read desktop proxy before restore: %w", err)
	}
	if current.Enabled && current.Address() != backup.Desired.Address() {
		return Result{Adapter: adapter.Name(), State: current},
			fmt.Errorf("desktop proxy is no longer owned by proxy-tui (current=%s, expected=%s); refusing to overwrite it", current.Address(), backup.Desired.Address())
	}
	if err := m.writeJournal(transactionJournal{
		Phase:     phaseRestoring,
		Adapter:   adapter.Name(),
		StartedAt: m.now(),
		Snapshot:  backup.Snapshot,
	}); err != nil {
		return Result{}, err
	}
	if err := m.restoreAndVerify(ctx, adapter, backup.Snapshot); err != nil {
		// Keep journal and backup: an unverified restore must never discard
		// the only copy of the user's original settings.
		return Result{Adapter: adapter.Name()}, fmt.Errorf("restore original desktop proxy settings: %w", err)
	}
	state, stateErr := adapter.GetState(ctx)
	if stateErr != nil {
		state = backup.Snapshot.Proxy
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
		report.BackupAt = backup.At
		report.BackupOriginal = backup.Original
		if _, stateErr := adapter.GetState(ctx); stateErr != nil {
			// Ownership cannot be disproved; keep the stop guard active.
			report.Mode = ModeSystemProxy
			report.BackupActive = true
			report.Notes = append(report.Notes, "desktop proxy state unavailable; backup ownership is retained")
		} else if !report.Desktop.Enabled || report.Desktop.Address() != backup.Desired.Address() {
			report.BackupStale = true
			report.Notes = append(report.Notes, "a stale desktop proxy backup is kept for inspection but is not owned by proxy-tui")
		} else {
			report.Mode = ModeSystemProxy
			report.BackupActive = true
		}
	}
	if !TUNAvailable() {
		report.Notes = append(report.Notes, "TUN is unavailable in this build")
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

	backup, backupErr := m.readBackup()
	if backupErr != nil {
		return RecoveryResult{}, backupErr
	}

	if entry, ok := m.readJournal(); ok {
		adapter := m.adapterByName(entry.Adapter)
		if adapter == nil {
			return RecoveryResult{}, fmt.Errorf("journal references unknown adapter %q", entry.Adapter)
		}
		switch entry.Phase {
		case phaseRolledBack:
			_ = m.removeJournal()
			return RecoveryResult{Action: "cleared a completed rollback journal", Restored: true}, nil
		case phaseCommitting:
			if backup != nil {
				// The backup was written after the apply was verified; this is
				// a committed enable, not an interrupted one. Never roll it
				// back just because the journal removal crashed.
				_ = m.removeJournal()
				return RecoveryResult{Action: "completed an interrupted network commit", Restored: true}, nil
			}
		}
		if err := m.restoreAndVerify(ctx, adapter, entry.Snapshot); err != nil {
			return RecoveryResult{}, fmt.Errorf("recover interrupted network change: %w", err)
		}
		if err := m.removeJournal(); err != nil {
			m.logf("warn", "remove network journal: "+err.Error())
		}
		// The original snapshot is authoritative after a successful restore;
		// do not leave a backup that would block supervisor stop forever.
		_ = m.removeBackup()
		m.logf("warn", "restored network state after an interrupted change")
		return RecoveryResult{Action: "restored an interrupted network change", Restored: true}, nil
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
		// settings. Keep the file for manual inspection but do not report an
		// active ownership that would block supervisor stop.
		return RecoveryResult{
			Action: "desktop proxy is no longer managed by proxy-tui",
			Detail: fmt.Sprintf("current=%s, backup=%s: leaving the current settings untouched", emptyOr(current.Address(), "disabled"), wanted),
		}, nil
	}
	if err := m.restoreAndVerify(ctx, adapter, backup.Snapshot); err != nil {
		return RecoveryResult{}, fmt.Errorf("restore desktop proxy after core stop: %w", err)
	}
	if err := m.removeBackup(); err != nil {
		m.logf("warn", "remove network backup: "+err.Error())
	}
	m.logf("warn", "restored desktop proxy after the core stopped")
	return RecoveryResult{Action: "restored the desktop proxy after the core stopped", Restored: true}, nil
}

func (m *Manager) restoreAndVerify(ctx context.Context, adapter Adapter, snapshot Snapshot) error {
	if err := adapter.Restore(ctx, snapshot); err != nil {
		return err
	}
	state, err := adapter.GetState(ctx)
	if err != nil {
		return fmt.Errorf("verify restored desktop proxy: %w", err)
	}
	if !sameProxyState(state, snapshot.Proxy) {
		return fmt.Errorf("restored desktop proxy does not match the original snapshot (want %+v, got %+v)", snapshot.Proxy, state)
	}
	return nil
}

func sameProxyState(a, b ProxyState) bool {
	if a.Enabled != b.Enabled || a.Protocol != b.Protocol || a.Host != b.Host || a.Port != b.Port || a.Bypass != b.Bypass {
		return false
	}
	if len(a.Services) != len(b.Services) {
		return false
	}
	for i := range a.Services {
		if a.Services[i] != b.Services[i] {
			return false
		}
	}
	return true
}

// Ensure errors.Is works for callers that only care about the class.
func IsUnsupported(err error) bool { return errors.Is(err, ErrUnsupported) }

func emptyOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
