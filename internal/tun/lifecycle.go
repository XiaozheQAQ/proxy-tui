package tun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// CoreController is the subset of core.Manager the TUN lifecycle needs.
type CoreController interface {
	Kinds() []model.CoreKind
	Status(ctx context.Context, kind model.CoreKind) core.Status
	Start(ctx context.Context, kind model.CoreKind, opts core.StartOptions) error
	Stop(ctx context.Context, kind model.CoreKind) error
	Validate(ctx context.Context, kind model.CoreKind, name string, data []byte) error
}

// ConfigSource provides the active profile configuration.
type ConfigSource interface {
	ActiveFor(kind model.CoreKind) (model.Profile, bool)
	ActiveConfig(id string) ([]byte, string, error)
}

// Manager drives the TUN lifecycle through the proxy core.
type Manager struct {
	paths storage.Paths
	cores CoreController
	subs  ConfigSource
	probe Probe
	logf  func(level, msg string)

	now          func() time.Time
	pollInterval time.Duration
	pollTimeout  time.Duration
	writeState   func(runtimeState) error

	mu sync.Mutex
}

// NewManager wires the lifecycle manager.
func NewManager(paths storage.Paths, cores CoreController, subs ConfigSource, probe Probe, logf func(level, msg string)) *Manager {
	if probe == nil {
		probe = NewSystemProbe()
	}
	if logf == nil {
		logf = func(string, string) {}
	}
	return &Manager{
		paths:        paths,
		cores:        cores,
		subs:         subs,
		probe:        probe,
		logf:         logf,
		now:          time.Now,
		pollInterval: 300 * time.Millisecond,
		pollTimeout:  10 * time.Second,
	}
}

// runtimeState is persisted while TUN is enabled.
type runtimeState struct {
	Enabled          bool           `json:"enabled"`
	Core             model.CoreKind `json:"core"`
	ProfileID        string         `json:"profile_id"`
	ProfileName      string         `json:"profile_name,omitempty"`
	CoreVersion      string         `json:"core_version,omitempty"`
	CorePID          int            `json:"core_pid,omitempty"`
	CoreStartedAt    time.Time      `json:"core_started_at,omitempty"`
	CoreBinary       string         `json:"core_binary,omitempty"`
	CoreBinarySHA256 string         `json:"core_binary_sha256,omitempty"`
	CoreConfigPath   string         `json:"core_config_path,omitempty"`
	ConfigSHA256     string         `json:"config_sha256,omitempty"`
	Settings         Settings       `json:"settings"`
	Baseline         Facts          `json:"baseline"`
	NewInterfaces    []string       `json:"new_interfaces"`
	Overrides        []string       `json:"overrides,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
	CoreWasRunning   bool           `json:"core_was_running"`
	StartedAt        time.Time      `json:"started_at"`
	ReleasePending   bool           `json:"release_pending,omitempty"`
	Leftover         []string       `json:"leftover,omitempty"`
}

func (m *Manager) statePath() string {
	return filepath.Join(m.paths.RuntimeDir(), "tun-state.json")
}

func (m *Manager) loadState() (*runtimeState, error) {
	state := runtimeState{}
	err := storage.ReadJSON(m.statePath(), &state)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read TUN state: %w", err)
	}
	if !state.Enabled && len(state.NewInterfaces) == 0 {
		return nil, nil
	}
	return &state, nil
}

func (m *Manager) saveState(state runtimeState) error {
	if m.writeState != nil {
		return m.writeState(state)
	}
	if err := os.MkdirAll(m.paths.RuntimeDir(), 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	return storage.WriteJSONAtomic(m.statePath(), state, 0o600)
}

func (m *Manager) removeState() error {
	err := os.Remove(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// coreIdentity matches the persisted TUN owner against the live core process.
// A legacy state without a pid can never be proven to be ours.
func (m *Manager) coreIdentity(ctx context.Context, state *runtimeState, verifyDigests bool) (core.Status, bool, bool) {
	status := m.cores.Status(ctx, state.Core)
	if !status.Running {
		return status, false, false
	}
	if state.CorePID <= 0 {
		return status, true, false
	}
	if status.PID != state.CorePID {
		return status, true, false
	}
	if !state.CoreStartedAt.IsZero() && !status.StartedAt.IsZero() {
		delta := status.StartedAt.Sub(state.CoreStartedAt)
		if delta < 0 {
			delta = -delta
		}
		if delta > 5*time.Second {
			return status, true, false
		}
	}
	if state.CoreBinary != "" && status.BinaryPath != "" && !strings.EqualFold(state.CoreBinary, status.BinaryPath) {
		return status, true, false
	}
	if verifyDigests && state.CoreBinarySHA256 != "" && status.BinaryPath != "" {
		if current := hashFile(status.BinaryPath); current != "" && current != state.CoreBinarySHA256 {
			return status, true, false
		}
	}
	// Config identity is only meaningful when both the path and the digest
	// are present. A state that recorded a digest without a path is legacy
	// and cannot be proven to be ours. Hashing is deferred to destructive
	// paths (stop/recover/enable); frequent status polling stays cheap.
	if verifyDigests && state.ConfigSHA256 != "" {
		if state.CoreConfigPath == "" {
			return status, true, false
		}
		if current := hashFile(state.CoreConfigPath); current == "" || current != state.ConfigSHA256 {
			return status, true, false
		}
	}
	return status, true, true
}

func hashFile(path string) string {
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, 512<<20)); err != nil {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func configDigest(config []byte) string {
	sum := sha256.Sum256(config)
	return hex.EncodeToString(sum[:])
}

// Status is the current TUN state.
type Status struct {
	Enabled        bool           `json:"enabled"`
	Active         bool           `json:"active"`
	Core           model.CoreKind `json:"core,omitempty"`
	CoreRunning    bool           `json:"core_running"`
	Interface      string         `json:"interface,omitempty"`
	Interfaces     []string       `json:"interfaces,omitempty"`
	Outbound       string         `json:"outbound,omitempty"`
	DNSMode        string         `json:"dns_mode,omitempty"`
	Routing        string         `json:"routing,omitempty"`
	Level          Level          `json:"level"`
	Detail         string         `json:"detail,omitempty"`
	Settings       Settings       `json:"settings"`
	Since          time.Time      `json:"since,omitempty"`
	Leftover       []string       `json:"leftover,omitempty"`
	ReleasePending bool           `json:"release_pending,omitempty"`
}

// EnableOptions selects the core and configuration for TUN.
type EnableOptions struct {
	Core     model.CoreKind
	Settings Settings
}

// Result reports one enable/change attempt.
type Result struct {
	Status        Status
	Report        Report
	Overrides     []string
	Warnings      []string
	CoreRestarted bool
	Verify        VerifyResult
}

// DisableResult reports a disable/release attempt.
type DisableResult struct {
	Status   Status
	Released bool
	Leftover []string
	Warnings []string
}

// RecoveryResult reports a recovery attempt.
type RecoveryResult struct {
	Action   string
	Released bool
	Status   Status
	Warnings []string
}

// VerifyResult compares the network before and after the core started.
type VerifyResult struct {
	Level         Level
	NewInterfaces []string
	Candidates    []string
	DefaultBefore string
	DefaultAfter  string
	RouteViaTUN   bool
	RouteKnown    bool
	DNSBefore     []string
	DNSAfter      []string
	DNSKnown      bool
	DNSChanged    bool
	Notes         []string
}

// Status observes the current TUN state without changing anything.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadState()
	if err != nil {
		return Status{Level: LevelUnknown, Detail: err.Error()}, err
	}
	if state == nil {
		return Status{Level: LevelReady, Detail: "TUN is not enabled"}, nil
	}
	facts, factsErr := m.probe.Facts(ctx)
	_, running, owned := m.coreIdentity(ctx, state, false)
	status := Status{
		Enabled:        true,
		Core:           state.Core,
		CoreRunning:    running,
		Settings:       state.Settings,
		Since:          state.StartedAt,
		Leftover:       state.Leftover,
		ReleasePending: state.ReleasePending,
		Level:          LevelReady,
	}
	if factsErr != nil {
		status.Level = LevelUnknown
		status.Detail = "network facts unavailable: " + factsErr.Error()
		return status, nil
	}
	present := presentInterfaces(state.NewInterfaces, facts)
	status.Interfaces = present
	if len(present) > 0 {
		status.Interface = present[0]
	}
	status.Outbound = facts.DefaultEgress
	if state.Settings.DNSHijack {
		status.DNSMode = "hijack (core-managed)"
	} else {
		status.DNSMode = "system"
	}
	switch {
	case len(present) > 0 && facts.DefaultRouteKnown && containsString(present, facts.DefaultEgress):
		status.Routing = "default route via " + facts.DefaultEgress
	case len(present) > 0:
		status.Routing = "automatic (default route unchanged or unknown)"
	default:
		status.Routing = "released"
	}
	switch {
	case status.CoreRunning && !owned:
		status.Level = LevelWarning
		status.Detail = "a different core instance is running; the recorded TUN owner is gone"
	case !status.CoreRunning && len(present) > 0:
		status.Level = LevelWarning
		status.Detail = "core stopped but the TUN interface is still present"
	case !status.CoreRunning:
		status.Level = LevelWarning
		status.Detail = "TUN state exists but the core is not running"
	case len(present) == 0:
		status.Level = LevelWarning
		status.Detail = "core is running but no TUN interface was observed"
	default:
		status.Active = true
		status.Level = LevelReady
		status.Detail = "TUN is active"
	}
	if state.ReleasePending {
		status.Level = LevelWarning
		status.Detail = "previous release left interfaces behind"
	}
	return status, nil
}

// buildEnvironment collects ownership context shared by preflight and enable.
func (m *Manager) buildEnvironment(ctx context.Context, kind model.CoreKind, settings Settings, facts Facts, coreStatus core.Status, hasProfile bool, profileErr string, overrides, warnings []string) Environment {
	env := Environment{
		Facts:            facts,
		Core:             kind,
		CoreStatus:       coreStatus,
		HasActiveProfile: hasProfile,
		ProfileError:     profileErr,
		Settings:         settings,
		Overrides:        overrides,
		Warnings:         warnings,
		OwnCorePIDs:      map[int]bool{},
	}
	for _, candidate := range m.cores.Kinds() {
		status := m.cores.Status(ctx, candidate)
		if status.Running && status.PID > 0 {
			env.OwnCorePIDs[status.PID] = true
		}
	}
	if state, stateErr := m.loadState(); stateErr == nil && state != nil {
		env.TUNStateFound = true
		_, running, owned := m.coreIdentity(ctx, state, true)
		env.RecordedTUN = presentInterfaces(state.NewInterfaces, facts)
		env.TUNStateOwned = owned
		env.TUNStateActive = running && owned
		if owned {
			env.OwnedTUN = env.RecordedTUN
			env.TUNStateDetail = "recorded TUN owner matches the running core"
		} else {
			env.TUNStateDetail = "recorded TUN state cannot be verified against the running core"
		}
	}
	return env
}

// Preflight evaluates the environment for TUN without enabling it.
func (m *Manager) Preflight(ctx context.Context, kind model.CoreKind, settings Settings) (Report, error) {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return Report{}, err
	}
	if !kind.Valid() {
		return Report{}, fmt.Errorf("choose Mihomo or sing-box for TUN")
	}
	facts, err := m.probe.Facts(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("collect network facts: %w", err)
	}
	env := m.buildEnvironment(ctx, kind, settings, facts, m.cores.Status(ctx, kind), false, "", nil, nil)
	if profile, ok := m.subs.ActiveFor(kind); ok {
		env.HasActiveProfile = true
		raw, _, configErr := m.subs.ActiveConfig(profile.ID)
		if configErr != nil {
			env.ProfileError = configErr.Error()
		} else {
			rendered, renderErr := Render(kind, raw, settings, env.CoreStatus.Version)
			if renderErr != nil {
				env.Warnings = append(env.Warnings, "render: "+renderErr.Error())
			} else {
				env.Overrides = rendered.Overrides
				env.Warnings = append(env.Warnings, rendered.Warnings...)
			}
		}
	}
	return Evaluate(env), nil
}

// Enable renders the TUN config, validates it, checks the environment and
// restarts the core with it. The previous core configuration is restored if
// anything fails.
func (m *Manager) Enable(ctx context.Context, opts EnableOptions) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kind := opts.Core
	settings := opts.Settings
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return Result{}, err
	}
	if !kind.Valid() {
		return Result{}, fmt.Errorf("choose Mihomo or sing-box for TUN")
	}

	coreStatus := m.cores.Status(ctx, kind)
	if !coreStatus.Installed {
		return Result{}, fmt.Errorf("%s is not installed: run proxy core install %s", kind.DisplayName(), kind)
	}
	minimum := MinimumVersion(kind)
	if !VersionAtLeast(coreStatus.Version, minimum) {
		return Result{}, fmt.Errorf("%s %s is too old for TUN (need %s or newer)", kind.DisplayName(), coreStatus.Version, minimum)
	}
	profile, ok := m.subs.ActiveFor(kind)
	if !ok {
		return Result{}, fmt.Errorf("no active %s profile: import and activate a subscription first", kind.DisplayName())
	}
	raw, _, err := m.subs.ActiveConfig(profile.ID)
	if err != nil {
		return Result{}, fmt.Errorf("read active config: %w", err)
	}

	rendered, err := Render(kind, raw, settings, coreStatus.Version)
	if err != nil {
		return Result{}, err
	}
	if err := m.cores.Validate(ctx, kind, profile.Label(), rendered.Config); err != nil {
		return Result{}, fmt.Errorf("rendered TUN config was rejected by %s: %w", kind.DisplayName(), err)
	}

	previousState, err := m.loadState()
	if err != nil {
		return Result{}, err
	}
	if previousState != nil && previousState.Core != kind {
		if _, disableErr := m.disableLocked(ctx); disableErr != nil {
			return Result{}, fmt.Errorf("release the existing TUN on %s first: %w", previousState.Core.DisplayName(), disableErr)
		}
		previousState = nil
	}
	// Ownership guard: never stop a core instance the state does not own.
	if previousState != nil {
		if _, running, owned := m.coreIdentity(ctx, previousState, true); running && !owned {
			return Result{}, fmt.Errorf("the recorded TUN state belongs to a different %s process; run proxy network tun recover first", kind.DisplayName())
		}
	}

	// Early preflight: a BLOCKED environment must not stop a working core.
	preFacts, err := m.probe.Facts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("collect network facts: %w", err)
	}
	preReport := Evaluate(m.buildEnvironment(ctx, kind, settings, preFacts, coreStatus, true, "", rendered.Overrides, rendered.Warnings))
	if !preReport.ActivationReady {
		return Result{Report: preReport, Overrides: rendered.Overrides, Warnings: rendered.Warnings},
			fmt.Errorf("preflight not activation-ready: %s", activationBlockers(preReport))
	}

	wasRunning := m.cores.Status(ctx, kind).Running
	if wasRunning {
		m.logf("info", fmt.Sprintf("stopping %s to apply TUN", kind.DisplayName()))
		if err := m.cores.Stop(ctx, kind); err != nil {
			return Result{}, fmt.Errorf("stop %s before enabling TUN: %w", kind.DisplayName(), err)
		}
	}
	// A previous TUN must be fully released before layering a new one.
	if previousState != nil {
		if leftover := m.waitInterfacesReleased(ctx, previousState.NewInterfaces); len(leftover) > 0 {
			if wasRunning {
				m.restartPlainCleanup(ctx, kind, profile)
			}
			return Result{Report: preReport, Overrides: rendered.Overrides, Warnings: rendered.Warnings},
				fmt.Errorf("previous TUN interfaces were not released (%s); run proxy network tun recover first", strings.Join(leftover, ", "))
		}
	}

	baseline, err := m.probe.Facts(ctx)
	if err != nil {
		if wasRunning {
			m.restartPlainCleanup(ctx, kind, profile)
		}
		return Result{}, fmt.Errorf("collect baseline network state: %w", err)
	}
	report := Evaluate(m.buildEnvironment(ctx, kind, settings, baseline, coreStatus, true, "", rendered.Overrides, rendered.Warnings))
	if !report.ActivationReady {
		if wasRunning {
			m.restartPlainCleanup(ctx, kind, profile)
		}
		return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings},
			fmt.Errorf("preflight not activation-ready: %s", activationBlockers(report))
	}

	startOptions := core.StartOptions{
		ProfileID:   profile.ID,
		ProfileName: profile.Label(),
		Config:      rendered.Config,
	}
	if err := m.cores.Start(ctx, kind, startOptions); err != nil {
		rollbackErr := m.rollbackStartedTUN(ctx, wasRunning, kind, profile)
		if rollbackErr != nil {
			return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings},
				fmt.Errorf("start %s with TUN failed: %w (BLOCKED: restoring the previous config also failed: %v)", kind.DisplayName(), err, rollbackErr)
		}
		return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings},
			fmt.Errorf("start %s with TUN failed: %w", kind.DisplayName(), err)
	}

	current, verify := m.waitForTUN(ctx, baseline)
	if len(verify.NewInterfaces) == 0 {
		cleanupErr := m.rollbackStartedTUN(ctx, wasRunning, kind, profile)
		message := fmt.Sprintf("%s did not create a TUN interface; refusing to report TUN as active", kind.DisplayName())
		if cleanupErr != nil {
			message += fmt.Sprintf(" (BLOCKED: cleanup failed: %v; inspect the core process and run proxy network tun recover)", cleanupErr)
		}
		return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings, Verify: verify},
			fmt.Errorf("%s", message)
	}
	live := m.cores.Status(ctx, kind)
	configSHA := hashFile(live.ConfigPath)
	if live.ConfigPath == "" || configSHA == "" || live.PID <= 0 {
		cleanupErr := m.rollbackStartedTUN(ctx, wasRunning, kind, profile)
		message := "cannot record core identity/config digest for the TUN process; refusing to leave it unmanaged"
		if cleanupErr != nil {
			message += fmt.Sprintf(" (BLOCKED: cleanup failed: %v)", cleanupErr)
		}
		return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings, Verify: verify},
			fmt.Errorf("%s", message)
	}
	state := runtimeState{
		Enabled:          true,
		Core:             kind,
		ProfileID:        profile.ID,
		ProfileName:      profile.Label(),
		CoreVersion:      coreStatus.Version,
		CorePID:          live.PID,
		CoreStartedAt:    live.StartedAt,
		CoreBinary:       live.BinaryPath,
		CoreBinarySHA256: hashFile(live.BinaryPath),
		CoreConfigPath:   live.ConfigPath,
		ConfigSHA256:     configSHA,
		Settings:         settings,
		Baseline:         baseline,
		NewInterfaces:    verify.NewInterfaces,
		Overrides:        rendered.Overrides,
		Warnings:         rendered.Warnings,
		CoreWasRunning:   wasRunning,
		StartedAt:        m.now(),
	}
	if err := m.saveState(state); err != nil {
		m.logf("error", fmt.Sprintf("persist TUN state failed: %v; core pid=%d interfaces=%v", err, live.PID, verify.NewInterfaces))
		cleanupErr := m.rollbackStartedTUN(ctx, wasRunning, kind, profile)
		message := fmt.Sprintf("persist TUN state: %v (core pid=%d interfaces=%v)", err, live.PID, verify.NewInterfaces)
		if cleanupErr != nil {
			message += fmt.Sprintf(" (BLOCKED: cleanup failed: %v; the core may still run with TUN)", cleanupErr)
		}
		return Result{Report: report, Overrides: rendered.Overrides, Warnings: rendered.Warnings, Verify: verify},
			fmt.Errorf("%s", message)
	}
	status := m.statusFromState(state, current)
	m.logf("info", fmt.Sprintf("TUN enabled on %s with %s (%s)", kind.DisplayName(), status.Interface, status.Routing))
	return Result{
		Status:        status,
		Report:        report,
		Overrides:     rendered.Overrides,
		Warnings:      rendered.Warnings,
		CoreRestarted: wasRunning,
		Verify:        verify,
	}, nil
}

// Disable stops the core it owns, verifies that it released the TUN resources
// and restarts the plain configuration if the core was running before TUN.
func (m *Manager) Disable(ctx context.Context) (DisableResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disableLocked(ctx)
}

func (m *Manager) disableLocked(ctx context.Context) (DisableResult, error) {
	state, err := m.loadState()
	if err != nil {
		return DisableResult{}, err
	}
	if state == nil {
		return DisableResult{Status: Status{Level: LevelReady, Detail: "TUN is not enabled"}}, nil
	}

	warnings := make([]string, 0, 2)
	_, running, owned := m.coreIdentity(ctx, state, true)
	if running && !owned {
		// A different process owns the core now. Never stop it; only clear a
		// provably released state or report the leftover interfaces.
		facts, factsErr := m.probe.Facts(ctx)
		var present []string
		if factsErr == nil {
			present = presentInterfaces(state.NewInterfaces, facts)
		}
		if len(present) == 0 {
			_ = m.removeState()
			return DisableResult{
				Status:   Status{Level: LevelWarning, Detail: "stale TUN state cleared; another core instance keeps running"},
				Released: true,
				Warnings: []string{"the recorded TUN owner process is gone; a different core instance is running and was left untouched"},
			}, nil
		}
		state.ReleasePending = true
		state.Leftover = present
		if saveErr := m.saveState(*state); saveErr != nil {
			return DisableResult{}, saveErr
		}
		return DisableResult{
			Status: Status{
				Enabled:        true,
				Core:           state.Core,
				CoreRunning:    true,
				Settings:       state.Settings,
				Level:          LevelWarning,
				Detail:         "TUN state is not owned by the running core",
				Leftover:       present,
				ReleasePending: true,
			},
			Released: false,
			Leftover: present,
			Warnings: []string{
				"a different " + state.Core.DisplayName() + " process is running; refusing to stop it",
				"interfaces from the recorded TUN run are still present: " + strings.Join(present, ", "),
			},
		}, nil
	}

	if running {
		if err := m.cores.Stop(ctx, state.Core); err != nil {
			return DisableResult{}, fmt.Errorf("stop %s to release TUN: %w", state.Core.DisplayName(), err)
		}
	}

	leftover := m.waitInterfacesReleased(ctx, state.NewInterfaces)
	released := len(leftover) == 0
	if !released {
		warnings = append(warnings, "the core did not release: "+strings.Join(leftover, ", "))
		state.ReleasePending = true
		state.Leftover = leftover
		if err := m.saveState(*state); err != nil {
			return DisableResult{}, err
		}
	} else {
		_ = m.removeState()
	}

	if released && state.CoreWasRunning {
		profile, ok := m.subs.ActiveFor(state.Core)
		if ok {
			if err := m.restartPlain(ctx, state.Core, profile); err != nil {
				warnings = append(warnings, "core restart with the original config failed: "+err.Error())
			}
		} else {
			warnings = append(warnings, "the core was running before TUN but no active profile remains; leaving it stopped")
		}
	}
	if released {
		m.logf("info", "TUN disabled and core resources released")
	} else {
		m.logf("warn", "TUN disabled but some interfaces were not released: "+strings.Join(leftover, ", "))
	}

	status := Status{Enabled: !released, Core: state.Core, Settings: state.Settings, Level: LevelReady, Detail: "TUN disabled"}
	if !released {
		status = Status{
			Enabled:        true,
			Core:           state.Core,
			CoreRunning:    m.cores.Status(ctx, state.Core).Running,
			Settings:       state.Settings,
			Level:          LevelWarning,
			Detail:         "release incomplete",
			Leftover:       leftover,
			ReleasePending: true,
		}
	}
	return DisableResult{Status: status, Released: released, Leftover: leftover, Warnings: warnings}, nil
}

// Recover repairs a TUN state left behind by a crash or an abnormal exit.
// It only touches a core process that matches the recorded TUN owner.
func (m *Manager) Recover(ctx context.Context) (RecoveryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadState()
	if err != nil {
		return RecoveryResult{}, err
	}
	if state == nil {
		return RecoveryResult{}, nil
	}
	warnings := make([]string, 0, 2)
	_, running, owned := m.coreIdentity(ctx, state, true)

	if running && !owned {
		facts, factsErr := m.probe.Facts(ctx)
		var present []string
		if factsErr == nil {
			present = presentInterfaces(state.NewInterfaces, facts)
		}
		if len(present) == 0 {
			_ = m.removeState()
			return RecoveryResult{
				Action:   "stale TUN state cleared; another core instance keeps running",
				Released: true,
				Status:   Status{Level: LevelWarning, Detail: "TUN state removed; running core was not touched"},
				Warnings: []string{"the recorded TUN owner process is gone; the running core was left running"},
			}, nil
		}
		state.ReleasePending = true
		state.Leftover = present
		if saveErr := m.saveState(*state); saveErr != nil {
			return RecoveryResult{}, saveErr
		}
		warnings = append(warnings, "a different "+state.Core.DisplayName()+" process is running; refusing to stop it")
		warnings = append(warnings, "interfaces from the recorded run are still present: "+strings.Join(present, ", "))
		warnings = append(warnings, "stop the owning process manually, then rerun proxy network tun recover")
		return RecoveryResult{
			Action:   "TUN state kept: the running core is not the recorded owner",
			Released: false,
			Status:   Status{Enabled: true, Core: state.Core, CoreRunning: true, Level: LevelWarning, Detail: "not owned by the running core", Leftover: present, ReleasePending: true},
			Warnings: warnings,
		}, nil
	}

	if running {
		m.logf("warn", "TUN recovery: stopping "+state.Core.DisplayName())
		if err := m.cores.Stop(ctx, state.Core); err != nil {
			return RecoveryResult{}, fmt.Errorf("stop core during TUN recovery: %w", err)
		}
	}
	leftover := m.waitInterfacesReleased(ctx, state.NewInterfaces)
	if len(leftover) > 0 {
		state.ReleasePending = true
		state.Leftover = leftover
		if err := m.saveState(*state); err != nil {
			return RecoveryResult{}, err
		}
		warnings = append(warnings, "interfaces still present: "+strings.Join(leftover, ", "))
		warnings = append(warnings, "reboot or remove the adapter manually; proxy-tui never edits routes itself")
		return RecoveryResult{
			Action:   "TUN state kept: resources were not released",
			Released: false,
			Status:   Status{Enabled: true, Core: state.Core, Level: LevelWarning, Detail: "release incomplete", Leftover: leftover, ReleasePending: true},
			Warnings: warnings,
		}, nil
	}

	if state.CoreWasRunning {
		if profile, ok := m.subs.ActiveFor(state.Core); ok {
			if err := m.restartPlain(ctx, state.Core, profile); err != nil {
				warnings = append(warnings, "core restart failed: "+err.Error())
			}
		}
	}
	_ = m.removeState()
	m.logf("warn", "TUN recovery completed: core stopped and interfaces released")
	return RecoveryResult{
		Action:   "TUN recovered: core stopped and interfaces released",
		Released: true,
		Status:   Status{Level: LevelReady, Detail: "TUN is not enabled"},
		Warnings: warnings,
	}, nil
}

// rollbackStartedTUN stops a core that was already launched with the TUN
// config and, when it was running before, restarts it with the plain config.
// Cleanup uses a fresh, bounded context: the original RPC context may already
// be cancelled by an IPC deadline.
func (m *Manager) rollbackStartedTUN(parent context.Context, wasRunning bool, kind model.CoreKind, profile model.Profile) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 45*time.Second)
	defer cancel()
	if m.cores.Status(ctx, kind).Running {
		if err := m.cores.Stop(ctx, kind); err != nil {
			return fmt.Errorf("stop TUN core during rollback: %w", err)
		}
	}
	if !wasRunning {
		return nil
	}
	if err := m.restartPlain(ctx, kind, profile); err != nil {
		return fmt.Errorf("restart plain core during rollback: %w", err)
	}
	return nil
}

func (m *Manager) restartPlainCleanup(parent context.Context, kind model.CoreKind, profile model.Profile) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 45*time.Second)
	defer cancel()
	return m.restartPlain(ctx, kind, profile)
}

func (m *Manager) restartPlain(ctx context.Context, kind model.CoreKind, profile model.Profile) error {
	config, _, err := m.subs.ActiveConfig(profile.ID)
	if err != nil {
		return err
	}
	return m.cores.Start(ctx, kind, core.StartOptions{
		ProfileID:   profile.ID,
		ProfileName: profile.Label(),
		Config:      config,
	})
}

func (m *Manager) waitInterfacesReleased(ctx context.Context, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	deadline := m.now().Add(m.pollTimeout)
	for {
		facts, err := m.probe.Facts(ctx)
		if err == nil {
			leftover := presentInterfaces(names, facts)
			if len(leftover) == 0 {
				return nil
			}
			if m.now().After(deadline) {
				return leftover
			}
		} else if m.now().After(deadline) {
			return names
		}
		if !m.sleep(ctx, m.pollInterval) {
			return names
		}
	}
}

func (m *Manager) waitForTUN(ctx context.Context, baseline Facts) (Facts, VerifyResult) {
	deadline := m.now().Add(m.pollTimeout)
	current, err := m.probe.Facts(ctx)
	if err != nil {
		current = baseline
	}
	for {
		verify := verifyAgainstBaseline(baseline, current)
		if len(verify.NewInterfaces) > 0 || m.now().After(deadline) {
			return current, verify
		}
		if !m.sleep(ctx, m.pollInterval) {
			return current, verify
		}
		next, err := m.probe.Facts(ctx)
		if err == nil {
			current = next
		}
	}
}

func (m *Manager) sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (m *Manager) statusFromState(state runtimeState, facts Facts) Status {
	present := presentInterfaces(state.NewInterfaces, facts)
	status := Status{
		Enabled:     true,
		Active:      len(present) > 0,
		Core:        state.Core,
		CoreRunning: true,
		Interfaces:  present,
		Settings:    state.Settings,
		Since:       state.StartedAt,
		Level:       LevelReady,
		Outbound:    facts.DefaultEgress,
	}
	if len(present) > 0 {
		status.Interface = present[0]
	}
	if facts.DefaultRouteKnown && containsString(present, facts.DefaultEgress) {
		status.Routing = "default route via " + facts.DefaultEgress
	} else if len(present) > 0 {
		status.Routing = "automatic (default route unchanged or unknown)"
	}
	if state.Settings.DNSHijack {
		status.DNSMode = "hijack (core-managed)"
	} else {
		status.DNSMode = "system"
	}
	switch {
	case len(present) == 0:
		status.Level = LevelWarning
		status.Detail = "core is running but no TUN interface was observed"
	case status.Routing != "" && facts.DefaultRouteKnown && containsString(present, facts.DefaultEgress):
		status.Detail = "TUN is active"
	default:
		status.Detail = "TUN is active; default route not observed through it"
	}
	return status
}

func verifyAgainstBaseline(baseline, current Facts) VerifyResult {
	verify := VerifyResult{
		Level:         LevelReady,
		NewInterfaces: NewInterfaces(baseline, current),
		DefaultBefore: baseline.DefaultEgress,
		DefaultAfter:  current.DefaultEgress,
		RouteKnown:    current.DefaultRouteKnown,
		DNSBefore:     baseline.DNSServers,
		DNSAfter:      current.DNSServers,
		DNSKnown:      current.DNSKnown && baseline.DNSKnown,
	}
	verify.Candidates = TUNCandidates(verify.NewInterfaces)
	verify.RouteViaTUN = containsString(verify.NewInterfaces, current.DefaultEgress)
	verify.DNSChanged = !sameStrings(baseline.DNSServers, current.DNSServers)
	if len(verify.NewInterfaces) == 0 {
		verify.Level = LevelWarning
		verify.Notes = append(verify.Notes, "no new network interface appeared after starting the core")
	}
	if verify.RouteKnown && !verify.RouteViaTUN {
		verify.Level = LevelWarning
		verify.Notes = append(verify.Notes, "the default route does not use the new interface (auto-route may be disabled or blocked)")
	}
	if !verify.RouteKnown {
		verify.Level = LevelWarning
		verify.Notes = append(verify.Notes, "default route could not be determined after start")
	}
	if !verify.DNSKnown {
		verify.Notes = append(verify.Notes, "DNS state could not be compared")
	}
	return verify
}

func presentInterfaces(names []string, facts Facts) []string {
	var present []string
	for _, name := range names {
		if facts.HasInterface(name) {
			present = append(present, name)
		}
	}
	sort.Strings(present)
	return present
}

func containsString(values []string, wanted string) bool {
	if wanted == "" {
		return false
	}
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// activationBlockers lists everything that prevents activation: BLOCKED checks
// and safety-related UNKNOWN facts. Warnings are intentionally not listed.
func activationBlockers(report Report) string {
	parts := append([]string{}, report.BlockingIssues...)
	for _, check := range report.Checks {
		if check.Level == LevelUnknown && check.Safety {
			parts = append(parts, check.Title+": "+check.Detail+" (unknown)")
		}
	}
	if len(parts) == 0 {
		return "preflight is not ready"
	}
	return strings.Join(parts, "; ")
}
