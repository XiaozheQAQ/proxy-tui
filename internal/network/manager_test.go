package network_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
)

type fakeAdapter struct {
	name        string
	supported   bool
	state       network.ProxyState
	snapshot    network.Snapshot
	applyErr    error
	verifyErr   error
	restoreErr  error
	restored    []network.Snapshot
	applied     []network.Desired
	restoreCall int
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) Capabilities(ctx context.Context) network.Capabilities {
	if !f.supported {
		return network.Capabilities{Platform: "test", Reason: "not supported in this test"}
	}
	return network.Capabilities{Platform: "test", Supported: true, Method: "fake"}
}

func (f *fakeAdapter) GetState(ctx context.Context) (network.ProxyState, error) {
	return f.state, nil
}

func (f *fakeAdapter) Snapshot(ctx context.Context) (network.Snapshot, error) {
	data, err := json.Marshal(f.state)
	if err != nil {
		return network.Snapshot{}, err
	}
	snapshot := network.Snapshot{Adapter: f.name, Taken: time.Now(), Proxy: f.state, Data: data}
	if f.snapshot.Adapter != "" {
		snapshot = f.snapshot
	}
	return snapshot, nil
}

func (f *fakeAdapter) Apply(ctx context.Context, desired network.Desired) error {
	f.applied = append(f.applied, desired)
	if f.applyErr != nil {
		return f.applyErr
	}
	f.state = network.ProxyState{Enabled: true, Protocol: desired.Protocol, Host: desired.Host, Port: desired.Port, Bypass: desired.Bypass}
	return nil
}

func (f *fakeAdapter) Verify(ctx context.Context, desired network.Desired) error {
	return f.verifyErr
}

func (f *fakeAdapter) Restore(ctx context.Context, snapshot network.Snapshot) error {
	f.restoreCall++
	f.restored = append(f.restored, snapshot)
	if f.restoreErr != nil {
		return f.restoreErr
	}
	state := network.ProxyState{}
	_ = json.Unmarshal(snapshot.Data, &state)
	f.state = state
	return nil
}

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

func desired() network.Desired {
	return network.Desired{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 7890, Bypass: "localhost"}
}

func TestEnableAndDisableRoundTrip(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{
		name:      "fake",
		supported: true,
		state:     network.ProxyState{Enabled: true, Protocol: "http", Host: "10.0.0.5", Port: 8080, Bypass: "*.local"},
	}
	manager := network.NewManager(paths, nil, adapter)

	result, err := manager.EnableSystemProxy(context.Background(), desired())
	if err != nil {
		t.Fatalf("EnableSystemProxy: %v", err)
	}
	if !result.Applied || !result.State.Enabled {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(paths.RuntimeDir(), "network-backup.json")); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.RuntimeDir(), "network-journal.json")); err == nil {
		t.Fatal("journal should be removed after a successful commit")
	}

	backup, ok := manager.ActiveBackup()
	if !ok {
		t.Fatal("ActiveBackup = false")
	}
	if backup.Original.Host != "10.0.0.5" || backup.Original.Port != 8080 {
		t.Errorf("backup original = %+v", backup.Original)
	}

	restore, err := manager.DisableSystemProxy(context.Background())
	if err != nil {
		t.Fatalf("DisableSystemProxy: %v", err)
	}
	if !restore.Restored {
		t.Fatalf("result = %+v", restore)
	}
	if adapter.state.Host != "10.0.0.5" || adapter.state.Port != 8080 || adapter.state.Bypass != "*.local" {
		t.Errorf("state after restore = %+v", adapter.state)
	}
	if _, err := os.Stat(filepath.Join(paths.RuntimeDir(), "network-backup.json")); err == nil {
		t.Fatal("backup should be removed after restore")
	}
	if _, ok := manager.ActiveBackup(); ok {
		t.Error("ActiveBackup still reports a backup")
	}
}

func TestApplyFailureRollsBack(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{
		name:      "fake",
		supported: true,
		state:     network.ProxyState{Enabled: true, Host: "10.0.0.5", Port: 8080},
		applyErr:  errors.New("registry denied"),
	}
	manager := network.NewManager(paths, nil, adapter)

	result, err := manager.EnableSystemProxy(context.Background(), desired())
	if err == nil {
		t.Fatal("expected an error")
	}
	if adapter.restoreCall != 1 {
		t.Fatalf("restore calls = %d, want 1", adapter.restoreCall)
	}
	if !result.RolledBack {
		t.Errorf("result = %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(paths.RuntimeDir(), "network-backup.json")); statErr == nil {
		t.Fatal("a failed enable must not write a backup")
	}
	journal := network.JournalForTest(paths)
	if journal == nil {
		t.Fatal("expected a rolled-back journal")
	}
	if journal.Phase != "rolled-back" {
		t.Errorf("journal phase = %q", journal.Phase)
	}
}

func TestVerifyFailureRollsBack(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{
		name:      "fake",
		supported: true,
		state:     network.ProxyState{},
		verifyErr: errors.New("proxy did not apply"),
	}
	manager := network.NewManager(paths, nil, adapter)
	if _, err := manager.EnableSystemProxy(context.Background(), desired()); err == nil {
		t.Fatal("expected an error")
	}
	if adapter.restoreCall != 1 {
		t.Fatalf("restore calls = %d", adapter.restoreCall)
	}
}

func TestDisableWithoutBackup(t *testing.T) {
	paths := testPaths(t)
	enabled := &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: true, Host: "1.2.3.4", Port: 8080}}
	manager := network.NewManager(paths, nil, enabled)
	if _, err := manager.DisableSystemProxy(context.Background()); !errors.Is(err, network.ErrNoBackup) {
		t.Fatalf("error = %v, want ErrNoBackup", err)
	}

	disabled := &fakeAdapter{name: "fake", supported: true}
	manager = network.NewManager(paths, nil, disabled)
	result, err := manager.DisableSystemProxy(context.Background())
	if err != nil {
		t.Fatalf("DisableSystemProxy: %v", err)
	}
	if !result.Restored {
		t.Errorf("result = %+v", result)
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{name: "fake", supported: false}
	manager := network.NewManager(paths, nil, adapter)
	if _, err := manager.EnableSystemProxy(context.Background(), desired()); !errors.Is(err, network.ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
	report, err := manager.Report(context.Background(), "")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.Capabilities.Supported {
		t.Error("report says supported")
	}
	if report.Capabilities.Reason == "" {
		t.Error("report has no reason")
	}
}

func TestTUNIsDeclaredButUnsupported(t *testing.T) {
	paths := testPaths(t)
	manager := network.NewManager(paths, nil, &fakeAdapter{name: "fake", supported: true})
	if _, err := manager.SetMode(context.Background(), network.ModeTUN, desired()); !errors.Is(err, network.ErrTUNUnsupported) {
		t.Fatalf("error = %v, want ErrTUNUnsupported", err)
	}
}

func TestRecoverInterruptedTransaction(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: true, Host: "1.2.3.4", Port: 8080}}
	manager := network.NewManager(paths, nil, adapter)

	journal := network.TransactionJournal{
		Phase:     "prepared",
		Adapter:   "fake",
		StartedAt: time.Now(),
		Snapshot:  network.Snapshot{Adapter: "fake", Proxy: network.ProxyState{}, Data: json.RawMessage(`{"Enabled":true,"Host":"1.2.3.4","Port":8080}`)},
	}
	if err := network.WriteJournalForTest(paths, journal); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	result, err := manager.Recover(context.Background(), false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !result.Restored {
		t.Fatalf("result = %+v", result)
	}
	if adapter.restoreCall != 1 {
		t.Errorf("restore calls = %d", adapter.restoreCall)
	}
	if network.JournalForTest(paths) != nil {
		t.Error("journal was not removed")
	}
}

func TestRecoverRestoresAfterCoreStopped(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: true, Host: "10.0.0.5", Port: 8080}}
	manager := network.NewManager(paths, nil, adapter)
	if _, err := manager.EnableSystemProxy(context.Background(), desired()); err != nil {
		t.Fatalf("EnableSystemProxy: %v", err)
	}

	kept, err := manager.Recover(context.Background(), true)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if kept.Restored {
		t.Error("recovery must keep the proxy while the core runs")
	}
	if !adapter.state.Enabled {
		t.Error("state changed while the core was running")
	}

	restored, err := manager.Recover(context.Background(), false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !restored.Restored {
		t.Errorf("result = %+v", restored)
	}
	if adapter.state.Host != "10.0.0.5" {
		t.Errorf("state after recovery = %+v", adapter.state)
	}
}

func TestReportDetectsForeignProxy(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{
		name:      "fake",
		supported: true,
		state:     network.ProxyState{Enabled: true, Host: "127.0.0.1", Port: 10808},
	}
	manager := network.NewManager(paths, nil, adapter)
	report, err := manager.Report(context.Background(), "127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !report.ForeignProxy {
		t.Fatal("an enabled proxy at another address must be reported as foreign")
	}
	if !strings.Contains(report.ForeignProxyDetail, "10808") {
		t.Errorf("detail = %q", report.ForeignProxyDetail)
	}

	same := network.NewManager(paths, nil, &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: true, Host: "127.0.0.1", Port: 7890}})
	sameReport, err := same.Report(context.Background(), "127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if sameReport.ForeignProxy {
		t.Error("our own proxy must not be reported as foreign")
	}
}

func TestRecoverLeavesForeignProxyAlone(t *testing.T) {
	paths := testPaths(t)
	adapter := &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: true, Host: "10.0.0.5", Port: 8080}}
	manager := network.NewManager(paths, nil, adapter)
	if _, err := manager.EnableSystemProxy(context.Background(), desired()); err != nil {
		t.Fatalf("EnableSystemProxy: %v", err)
	}
	// Another program takes the proxy over after ProxyTUI.
	adapter.state = network.ProxyState{Enabled: true, Host: "127.0.0.1", Port: 10808}

	result, err := manager.Recover(context.Background(), false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Restored {
		t.Fatal("recovery must not overwrite another program's proxy settings")
	}
	if !strings.Contains(result.Action, "no longer managed") {
		t.Errorf("action = %q", result.Action)
	}
	if adapter.restoreCall != 0 {
		t.Errorf("restore calls = %d, want 0", adapter.restoreCall)
	}
	if _, ok := manager.ActiveBackup(); !ok {
		t.Error("the backup should be kept for auditing")
	}
}

func TestReportIncludesEnvironmentAndBackup(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8080")
	t.Setenv("NO_PROXY", "localhost")
	adapter := &fakeAdapter{name: "fake", supported: true, state: network.ProxyState{Enabled: false}}
	manager := network.NewManager(paths, nil, adapter)

	report, err := manager.Report(context.Background(), "127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !report.Environment.Detected() || report.Environment.HTTP != "http://127.0.0.1:8080" {
		t.Errorf("environment = %+v", report.Environment)
	}
	if report.Mode != network.ModeOff {
		t.Errorf("mode = %q", report.Mode)
	}
	if len(report.Notes) == 0 {
		t.Error("expected a TUN note")
	}
}
