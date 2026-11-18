package tun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

type fakeCores struct {
	mu           sync.Mutex
	installed    bool
	version      string
	running      bool
	pid          int
	startedAt    time.Time
	binary       string
	startResults []error
	starts       []core.StartOptions
	stops        int
	validateErr  error
	validated    [][]byte
}

func (f *fakeCores) Kinds() []model.CoreKind {
	return []model.CoreKind{model.CoreMihomo, model.CoreSingBox}
}

func (f *fakeCores) Status(ctx context.Context, kind model.CoreKind) core.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return core.Status{
		Core:       kind,
		Installed:  f.installed,
		Version:    f.version,
		Running:    f.running,
		PID:        f.pid,
		StartedAt:  f.startedAt,
		BinaryPath: f.binary,
	}
}

func (f *fakeCores) Start(ctx context.Context, kind model.CoreKind, opts core.StartOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, opts)
	var err error
	if len(f.startResults) > 0 {
		err = f.startResults[0]
		f.startResults = f.startResults[1:]
	}
	if err != nil {
		return err
	}
	f.running = true
	return nil
}

func (f *fakeCores) Stop(ctx context.Context, kind model.CoreKind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.running = false
	return nil
}

func (f *fakeCores) Validate(ctx context.Context, kind model.CoreKind, name string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validated = append(f.validated, append([]byte(nil), data...))
	return f.validateErr
}

type fakeSubs struct {
	profile model.Profile
	config  []byte
	err     error
}

func (f *fakeSubs) ActiveFor(kind model.CoreKind) (model.Profile, bool) {
	if f.profile.Core != kind {
		return model.Profile{}, false
	}
	return f.profile, true
}

func (f *fakeSubs) ActiveConfig(id string) ([]byte, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return f.config, "active.yaml", nil
}

type fakeProbe struct {
	mu      sync.Mutex
	current Facts
	queue   []Facts
	err     error
}

func (f *fakeProbe) Facts(ctx context.Context) (Facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return Facts{}, f.err
	}
	if len(f.queue) > 0 {
		f.current = f.queue[0]
		f.queue = f.queue[1:]
	}
	return f.current, nil
}

func (f *fakeProbe) push(facts ...Facts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, facts...)
}

func (f *fakeProbe) set(facts Facts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = facts
}

func lifecycleFacts(tun bool) Facts {
	facts := Facts{
		Platform:            "windows",
		Elevated:            true,
		ElevationDetail:     "elevated",
		Interfaces:          []InterfaceInfo{{Name: "WLAN", Addresses: []string{"192.168.1.10/24"}}},
		DefaultEgress:       "WLAN",
		DefaultRouteKnown:   true,
		IPv4:                true,
		DNSKnown:            true,
		DNSServers:          []string{"1.1.1.1"},
		DNSSource:           "registry",
		DesktopProxyKnown:   true,
		DesktopProxyEnabled: false,
		WinHTTPKnown:        true,
		FirewallKnown:       true,
		FirewallAvailable:   true,
	}
	if tun {
		facts.Interfaces = append(facts.Interfaces, InterfaceInfo{Name: "Meta", Addresses: []string{"198.18.0.1/16"}})
		facts.DefaultEgress = "Meta"
		facts.TUNDevices = []string{"Meta"}
	}
	return facts
}

const lifecycleProfile = `
mixed-port: 7890
dns:
  enable: true
  nameserver: [223.5.5.5]
proxies:
  - {name: Tokyo, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
proxy-groups:
  - {name: PROXY, type: select, proxies: [Tokyo, DIRECT]}
rules:
  - MATCH,PROXY
`

func testPaths(t *testing.T) storage.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return paths
}

func newTestManager(t *testing.T) (*Manager, *fakeCores, *fakeSubs, *fakeProbe, storage.Paths) {
	t.Helper()
	paths := testPaths(t)
	cores := &fakeCores{
		installed: true,
		version:   "1.19.34",
		pid:       4242,
		startedAt: time.Now().Add(-time.Minute),
		binary: `C:akemihomo.exe`}
	subs := &fakeSubs{
		profile: model.Profile{ID: "p1", Name: "Test", Core: model.CoreMihomo},
		config:  []byte(lifecycleProfile),
	}
	probe := &fakeProbe{current: lifecycleFacts(false)}
	manager := NewManager(paths, cores, subs, probe, nil)
	manager.pollInterval = time.Millisecond
	manager.pollTimeout = 30 * time.Millisecond
	return manager, cores, subs, probe, paths
}

func TestEnableTUNSuccess(t *testing.T) {
	manager, cores, _, probe, paths := newTestManager(t)
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))

	result, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !result.Status.Enabled || !result.Status.Active {
		t.Fatalf("status = %+v", result.Status)
	}
	if result.Status.Interface != "Meta" {
		t.Errorf("interface = %q", result.Status.Interface)
	}
	if !strings.Contains(result.Status.Routing, "Meta") {
		t.Errorf("routing = %q", result.Status.Routing)
	}
	if result.Report.Level == LevelBlocked {
		t.Errorf("report blocked: %s", result.Report.Summary())
	}
	if len(cores.starts) != 1 {
		t.Fatalf("starts = %d", len(cores.starts))
	}
	if !strings.Contains(string(cores.starts[0].Config), "tun:") {
		t.Error("the core was not started with a TUN config")
	}
	if len(result.Verify.NewInterfaces) == 0 {
		t.Error("verification did not detect the new interface")
	}
	if _, err := os.Stat(paths.RuntimeDir() + string(os.PathSeparator) + "tun-state.json"); err != nil {
		t.Fatalf("state file missing: %v", err)
	}

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Active || status.Interface != "Meta" {
		t.Errorf("status = %+v", status)
	}
}

func TestEnableBlockedByPrivilege(t *testing.T) {
	manager, cores, _, probe, paths := newTestManager(t)
	blocked := lifecycleFacts(false)
	blocked.Elevated = false
	probe.push(blocked)

	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected a blocked preflight error")
	} else if !strings.Contains(err.Error(), "not activation-ready") {
		t.Errorf("error = %v", err)
	}
	if len(cores.starts) != 0 {
		t.Errorf("starts = %d, want 0", len(cores.starts))
	}
	if _, err := os.Stat(paths.RuntimeDir() + string(os.PathSeparator) + "tun-state.json"); err == nil {
		t.Error("state must not be written when preflight is blocked")
	}
}

func TestEnableStopsBeforeValidationFailure(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	cores.validateErr = core.ErrInvalidConfig
	probe.push(lifecycleFacts(false))

	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected a validation error")
	}
	if cores.stops != 0 {
		t.Errorf("the running core must not be stopped before validation succeeds (stops=%d)", cores.stops)
	}
	if len(cores.starts) != 0 {
		t.Errorf("starts = %d", len(cores.starts))
	}
}

func TestEnableStartFailureRollsBack(t *testing.T) {
	manager, cores, subs, probe, paths := newTestManager(t)
	cores.running = true
	cores.startResults = []error{errors.New("tun start failed"), nil}
	probe.push(lifecycleFacts(false), lifecycleFacts(false))

	_, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()})
	if err == nil {
		t.Fatal("expected a start error")
	}
	if cores.stops != 1 {
		t.Errorf("stops = %d", cores.stops)
	}
	if len(cores.starts) != 2 {
		t.Fatalf("starts = %d, want 2 (TUN attempt + rollback)", len(cores.starts))
	}
	if !strings.Contains(string(cores.starts[1].Config), "mixed-port: 7890") || strings.Contains(string(cores.starts[1].Config), "tun:") {
		t.Error("rollback did not restart the plain configuration")
	}
	if string(cores.starts[1].Config) != string(subs.config) {
		t.Error("rollback config differs from the original profile")
	}
	if _, err := os.Stat(paths.RuntimeDir() + string(os.PathSeparator) + "tun-state.json"); err == nil {
		t.Error("state must not be written after a failed start")
	}
}

func TestEnableBlockedDoesNotStopRunningCore(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	blocked := lifecycleFacts(false)
	blocked.Elevated = false
	probe.push(blocked)

	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected a blocked preflight")
	}
	if cores.stops != 0 {
		t.Errorf("a blocked preflight must not stop the running core (stops=%d)", cores.stops)
	}
	if len(cores.starts) != 0 {
		t.Errorf("starts = %d", len(cores.starts))
	}
}

func TestEnableAbortsWhenPreviousTUNNotReleased(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = false
	leftover := lifecycleFacts(true)
	leftover.DefaultEgress = "WLAN"
	leftover.DefaultRouteKnown = true
	probe.set(leftover)
	if err := manager.saveState(runtimeState{
		Enabled:       true,
		Core:          model.CoreMihomo,
		NewInterfaces: []string{"Meta"},
		Settings:      Defaults(),
		CorePID:       cores.pid,
		CoreStartedAt: cores.startedAt,
		CoreBinary:    cores.binary,
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected an error when the previous TUN was not released")
	} else if !strings.Contains(err.Error(), "not released") {
		t.Errorf("error = %v", err)
	}
	if len(cores.starts) != 0 {
		t.Errorf("starts = %d; a leftover TUN must not be layered", len(cores.starts))
	}
}

func TestEnablePersistsIdentityAndDigest(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	state, err := manager.loadState()
	if err != nil || state == nil {
		t.Fatalf("state: %v", err)
	}
	if state.CorePID != cores.pid {
		t.Errorf("CorePID = %d, want %d", state.CorePID, cores.pid)
	}
	if !state.CoreStartedAt.Equal(cores.startedAt) {
		t.Errorf("CoreStartedAt = %v", state.CoreStartedAt)
	}
	if state.CoreBinary != cores.binary {
		t.Errorf("CoreBinary = %q", state.CoreBinary)
	}
	if len(state.ConfigSHA256) != 64 {
		t.Errorf("ConfigSHA256 = %q", state.ConfigSHA256)
	}
}

func saveForeignState(t *testing.T, manager *Manager, interfaces []string) {
	t.Helper()
	if err := manager.saveState(runtimeState{
		Enabled:       true,
		Core:          model.CoreMihomo,
		NewInterfaces: interfaces,
		Settings:      Defaults(),
		CorePID:       111,
		CoreStartedAt: time.Now().Add(-time.Hour),
		CoreBinary:    `C:othermihomo.exe`,
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
}

func TestEnableRefusesSafetyUnknownWithoutStopping(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	unknown := lifecycleFacts(false)
	unknown.UnclassifiedVirtual = []string{"mystery0"}
	probe.push(unknown)

	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected the safety UNKNOWN to block activation")
	} else if !strings.Contains(err.Error(), "not activation-ready") {
		t.Errorf("error = %v", err)
	}
	if cores.stops != 0 || len(cores.starts) != 0 {
		t.Errorf("blocked activation touched the core: stops=%d starts=%d", cores.stops, len(cores.starts))
	}
}

func TestIdentityBinaryDigestChangeRefusesStop(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	binary := filepath.Join(t.TempDir(), "mihomo.exe")
	if err := os.WriteFile(binary, []byte("binary-v1"), 0o600); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	cores.binary = binary
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	state, _ := manager.loadState()
	if state == nil || len(state.CoreBinarySHA256) != 64 {
		t.Fatalf("binary digest not persisted: %+v", state)
	}
	if err := os.WriteFile(binary, []byte("binary-v2"), 0o600); err != nil {
		t.Fatalf("rewrite fake binary: %v", err)
	}
	probe.set(lifecycleFacts(true))
	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if cores.stops != 0 {
		t.Errorf("must not stop a core whose binary identity changed (stops=%d)", cores.stops)
	}
	if result.Released {
		t.Error("release must not be reported")
	}
	if !strings.Contains(strings.Join(result.Warnings, " "), "refusing to stop") {
		t.Errorf("warnings = %v", result.Warnings)
	}
}

func TestDisableRefusesForeignCore(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(true))
	saveForeignState(t, manager, []string{"Meta"})

	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if cores.stops != 0 {
		t.Errorf("must not stop a core it does not own (stops=%d)", cores.stops)
	}
	if result.Released {
		t.Error("release must not be reported while a foreign core runs")
	}
	if state, _ := manager.loadState(); state == nil {
		t.Error("state should be kept for diagnosis")
	}
	if !strings.Contains(strings.Join(result.Warnings, " "), "refusing to stop") {
		t.Errorf("warnings = %v", result.Warnings)
	}
}

func TestDisableClearsStaleStateWhenReleased(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(false))
	saveForeignState(t, manager, []string{"Meta"})

	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if cores.stops != 0 {
		t.Errorf("must not stop a foreign core (stops=%d)", cores.stops)
	}
	if !result.Released {
		t.Error("a provably released stale state should be released")
	}
	if state, _ := manager.loadState(); state != nil {
		t.Error("stale state should be removed")
	}
}

func TestRecoverRefusesForeignCore(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(true))
	saveForeignState(t, manager, []string{"Meta"})

	result, err := manager.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if cores.stops != 0 {
		t.Errorf("must not stop a foreign core (stops=%d)", cores.stops)
	}
	if result.Released {
		t.Error("recovery must report the foreign ownership")
	}
	if state, _ := manager.loadState(); state == nil {
		t.Error("state should be kept")
	}
}

func TestRecoverClearsStaleStateWithoutStopping(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(false))
	saveForeignState(t, manager, []string{"Meta"})

	result, err := manager.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if cores.stops != 0 {
		t.Errorf("must not stop a foreign core (stops=%d)", cores.stops)
	}
	if !result.Released || !strings.Contains(result.Action, "stale") {
		t.Errorf("result = %+v", result)
	}
	if state, _ := manager.loadState(); state != nil {
		t.Error("stale state should be removed")
	}
}

func TestStatusReportsForeignCore(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(true))
	saveForeignState(t, manager, []string{"Meta"})

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Active {
		t.Error("a foreign core must not be reported as an active TUN")
	}
	if status.Level != LevelWarning || !strings.Contains(status.Detail, "different core instance") {
		t.Errorf("status = %+v", status)
	}
}

func TestDisableReleasesAndRestartsPlain(t *testing.T) {
	manager, cores, _, probe, paths := newTestManager(t)
	cores.running = true
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !cores.running {
		t.Fatal("core should run after enable")
	}

	probe.set(lifecycleFacts(true))
	probe.push(lifecycleFacts(false))
	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if !result.Released || len(result.Leftover) != 0 {
		t.Errorf("result = %+v", result)
	}
	if len(cores.starts) != 2 {
		t.Fatalf("starts = %d, want 2 (tun + plain restart)", len(cores.starts))
	}
	if strings.Contains(string(cores.starts[1].Config), "tun:") {
		t.Error("the plain restart used a TUN config")
	}
	if _, err := os.Stat(paths.RuntimeDir() + string(os.PathSeparator) + "tun-state.json"); err == nil {
		t.Error("state file should be removed after a clean disable")
	}
}

func TestDisableLeftoverKeepsState(t *testing.T) {
	manager, _, _, probe, paths := newTestManager(t)
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	probe.set(lifecycleFacts(true))

	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if result.Released {
		t.Fatal("release should have been reported as incomplete")
	}
	if !containsString(result.Leftover, "Meta") {
		t.Errorf("leftover = %v", result.Leftover)
	}
	state, err := manager.loadState()
	if err != nil || state == nil {
		t.Fatalf("state should be kept for recovery: %v", err)
	}
	if !state.ReleasePending {
		t.Error("state should be marked release_pending")
	}
	if _, err := os.Stat(paths.RuntimeDir() + string(os.PathSeparator) + "tun-state.json"); err != nil {
		t.Error("state file should exist")
	}
}

func TestRecoverStopsAndReleases(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = true
	probe.set(lifecycleFacts(true))
	if err := manager.saveState(runtimeState{
		Enabled:       true,
		Core:          model.CoreMihomo,
		NewInterfaces: []string{"Meta"},
		Settings:      Defaults(),
		StartedAt:     time.Now(),
		CorePID:       cores.pid,
		CoreStartedAt: cores.startedAt,
		CoreBinary:    cores.binary,
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	probe.push(lifecycleFacts(false))
	result, err := manager.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !result.Released {
		t.Errorf("result = %+v", result)
	}
	if cores.stops != 1 {
		t.Errorf("stops = %d", cores.stops)
	}
	if state, _ := manager.loadState(); state != nil {
		t.Error("state should be removed after a successful recovery")
	}
}

func TestRecoverLeftoverKeepsState(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = false
	probe.set(lifecycleFacts(true))
	if err := manager.saveState(runtimeState{
		Enabled:       true,
		Core:          model.CoreMihomo,
		NewInterfaces: []string{"Meta"},
		Settings:      Defaults(),
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	result, err := manager.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Released {
		t.Fatal("recovery must report the leftover interfaces")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected actionable warnings")
	}
	if state, _ := manager.loadState(); state == nil {
		t.Error("state should be kept when resources are not released")
	}
}

func TestStatusDegradedWhenCoreStopped(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	cores.running = false
	probe.set(lifecycleFacts(true))
	if err := manager.saveState(runtimeState{
		Enabled:       true,
		Core:          model.CoreMihomo,
		NewInterfaces: []string{"Meta"},
		Settings:      Defaults(),
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Level != LevelWarning || status.Active {
		t.Errorf("status = %+v", status)
	}
	if !strings.Contains(status.Detail, "core stopped") {
		t.Errorf("detail = %q", status.Detail)
	}
}

func TestEnableVersionGate(t *testing.T) {
	manager, cores, _, _, _ := newTestManager(t)
	cores.version = "1.10.2"
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err == nil {
		t.Fatal("expected an old-core error")
	}
}

func TestChangeTunSettingsRestartsCore(t *testing.T) {
	manager, cores, _, probe, _ := newTestManager(t)
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	changed := Defaults()
	changed.Stack = StackSystem
	changed.MTU = 1500
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: changed}); err != nil {
		t.Fatalf("Enable (change): %v", err)
	}
	if len(cores.starts) != 2 {
		t.Fatalf("starts = %d, want 2", len(cores.starts))
	}
	if !strings.Contains(string(cores.starts[1].Config), "stack: system") {
		t.Error("the second start did not use the new TUN settings")
	}
	state, err := manager.loadState()
	if err != nil || state == nil {
		t.Fatalf("state: %v", err)
	}
	if state.Settings.Stack != StackSystem || state.Settings.MTU != 1500 {
		t.Errorf("persisted settings = %+v", state.Settings)
	}
}

func TestStatusAdoptsTunAcrossManagers(t *testing.T) {
	manager, cores, subs, probe, paths := newTestManager(t)
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	if _, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	// A new process (new manager) must still see the active TUN state.
	adopted := NewManager(paths, cores, subs, probe, nil)
	status, err := adopted.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Enabled || !status.Active || status.Interface != "Meta" {
		t.Errorf("status = %+v", status)
	}
}

func TestEnableReportsOverrides(t *testing.T) {
	manager, _, subs, probe, _ := newTestManager(t)
	subs.config = []byte(strings.Replace(lifecycleProfile, "mixed-port: 7890", `
mixed-port: 7890
tun:
  enable: true
  stack: gvisor
`, 1))
	probe.push(lifecycleFacts(false), lifecycleFacts(false), lifecycleFacts(true))
	result, err := manager.Enable(context.Background(), EnableOptions{Core: model.CoreMihomo, Settings: Defaults()})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if len(result.Overrides) == 0 {
		t.Error("expected overrides for the existing TUN block")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for the existing TUN block")
	}
}
