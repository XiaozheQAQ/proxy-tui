package mihomo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func TestAdapterHelper(t *testing.T) {
	if os.Getenv("PROXYTUI_CORE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	fmt.Println("fake core running")
	time.Sleep(30 * time.Second)
}

func newIdentityTestFixture(t *testing.T) (storage.Paths, *exec.Cmd, core.ProcessIdentity, core.RuntimeState) {
	t.Helper()
	dir := t.TempDir()
	paths := storage.Paths{
		Config: filepath.Join(dir, "config"),
		Data:   filepath.Join(dir, "data"),
		State:  filepath.Join(dir, "state"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(executable, "-test.run=TestAdapterHelper$")
	cmd.Env = append(os.Environ(), "PROXYTUI_CORE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	identity, err := core.ReadProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	configPath := filepath.Join(paths.RuntimeDir(), "mihomo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("config dir: %v", err)
	}
	config := []byte("runtime-config")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	state := core.RuntimeState{
		Core:             model.CoreMihomo,
		PID:              cmd.Process.Pid,
		APIAddr:          "127.0.0.1:1",
		Secret:           "test-secret",
		ConfigPath:       configPath,
		ConfigSHA256:     core.HashBytes(config),
		ProcessStartedAt: identity.StartedAt,
		BinaryPath:       identity.Executable,
		BinarySHA256:     core.HashFile(identity.Executable),
	}
	if err := core.WriteRuntimeState(paths, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return paths, cmd, identity, state
}

func fakeCoreAPI(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"version":"fake"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestStatusDegradedWhenAPIDownAndStateKept(t *testing.T) {
	paths, _, _, state := newIdentityTestFixture(t)
	adapter := New(paths, core.DefaultRuntimeOptions(), nil)
	status := adapter.Status(context.Background())
	if !status.Running || !status.Degraded {
		t.Fatalf("status = %+v, want running+degraded", status)
	}
	if status.PID != state.PID {
		t.Fatalf("pid = %d, want %d", status.PID, state.PID)
	}
	persisted, ok := core.ReadRuntimeState(paths, model.CoreMihomo)
	if !ok || persisted.PID != state.PID {
		t.Fatal("runtime state was deleted on a transient API failure")
	}
}

func TestStopVerifiesIdentityAndKillsOwnedProcess(t *testing.T) {
	paths, helper, _, state := newIdentityTestFixture(t)
	server := fakeCoreAPI(t, http.StatusOK)
	state.APIAddr = strings.TrimPrefix(server.URL, "http://")
	if err := core.WriteRuntimeState(paths, state); err != nil {
		t.Fatalf("state: %v", err)
	}
	adapter := New(paths, core.DefaultRuntimeOptions(), nil)
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !core.WaitProcessExit(helper.Process.Pid, 5*time.Second) {
		t.Fatal("helper was not stopped")
	}
	if _, ok := core.ReadRuntimeState(paths, model.CoreMihomo); ok {
		t.Fatal("runtime state was not removed after a verified stop")
	}
}

func TestStopRefusesMismatchedOrLegacyState(t *testing.T) {
	paths, helper, identity, state := newIdentityTestFixture(t)
	adapter := New(paths, core.DefaultRuntimeOptions(), nil)

	mismatched := state
	mismatched.ProcessStartedAt = identity.StartedAt.Add(10 * time.Second)
	if err := core.WriteRuntimeState(paths, mismatched); err != nil {
		t.Fatalf("state: %v", err)
	}
	if err := adapter.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "CORE_IDENTITY_MISMATCH") {
		t.Fatalf("mismatch stop error = %v", err)
	}
	if !core.ProcessAlive(helper.Process.Pid) {
		t.Fatal("mismatched identity must not kill the helper")
	}

	legacy := state
	legacy.ProcessStartedAt = time.Time{}
	legacy.BinarySHA256 = ""
	legacy.ConfigSHA256 = ""
	if err := core.WriteRuntimeState(paths, legacy); err != nil {
		t.Fatalf("legacy state: %v", err)
	}
	if err := adapter.Stop(context.Background()); err == nil {
		t.Fatal("legacy state must be refused")
	}
	if !core.ProcessAlive(helper.Process.Pid) {
		t.Fatal("legacy state must not kill the helper")
	}
}

func TestStopRefusesUnauthorizedAPI(t *testing.T) {
	paths, helper, _, state := newIdentityTestFixture(t)
	server := fakeCoreAPI(t, http.StatusUnauthorized)
	state.APIAddr = strings.TrimPrefix(server.URL, "http://")
	if err := core.WriteRuntimeState(paths, state); err != nil {
		t.Fatalf("state: %v", err)
	}
	adapter := New(paths, core.DefaultRuntimeOptions(), nil)
	if err := adapter.Stop(context.Background()); err == nil {
		t.Fatal("Stop accepted a core API that rejected the stored secret")
	}
	if !core.ProcessAlive(helper.Process.Pid) {
		t.Fatal("unauthorized API must not kill the helper")
	}
}
