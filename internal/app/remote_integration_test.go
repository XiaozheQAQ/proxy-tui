package app_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/supervisor"
)

func rawSetting(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func TestTwoRemoteFrontendsShareSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	application, err := app.New(paths)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervisor.Serve(ctx, paths, "test", supervisor.NewAppBackend(application), nil) }()
	defer func() {
		cancel()
		<-done
	}()
	endpoint, err := ipc.EndpointPath(paths.RuntimeDir())
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	dial := func() *ipc.Client {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			client, dialErr := ipc.Dial(context.Background(), endpoint, time.Second)
			if dialErr == nil {
				return client
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("supervisor did not accept connections")
		return nil
	}
	clientA := dial()
	defer clientA.Close()
	clientB := dial()
	defer clientB.Close()
	frontendA, err := app.NewRemote(context.Background(), clientA)
	if err != nil {
		t.Fatalf("remote A: %v", err)
	}
	frontendB, err := app.NewRemote(context.Background(), clientB)
	if err != nil {
		t.Fatalf("remote B: %v", err)
	}
	if err := frontendA.PatchSettings(context.Background(), map[string]json.RawMessage{"theme": rawSetting(storage.ThemeLight)}); err != nil {
		t.Fatalf("A theme patch: %v", err)
	}
	if got := frontendB.SettingsSnapshot(context.Background()).Theme; got != storage.ThemeLight {
		t.Fatalf("B did not see A's theme: %q", got)
	}
	if err := frontendB.PatchSettings(context.Background(), map[string]json.RawMessage{"tun_mtu": rawSetting(1400)}); err != nil {
		t.Fatalf("B mtu patch: %v", err)
	}
	snapshotA := frontendA.SettingsSnapshot(context.Background())
	if snapshotA.Theme != storage.ThemeLight || snapshotA.TunMTU != 1400 {
		t.Fatalf("A lost a field written by B: %+v", snapshotA)
	}
	if filepath.Base(paths.Config) == "" {
		t.Fatal("unreachable")
	}
}
