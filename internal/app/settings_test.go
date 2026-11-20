package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/proxytui/proxy-tui/internal/storage"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	application, err := New(paths)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return application
}

func raw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func TestPatchSettingsKeepsDifferentFields(t *testing.T) {
	application := testApp(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := application.PatchSettings(ctx, map[string]json.RawMessage{"theme": raw(storage.ThemeLight)}); err != nil {
			t.Errorf("theme patch: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := application.PatchSettings(ctx, map[string]json.RawMessage{"tun_mtu": raw(1400)}); err != nil {
			t.Errorf("mtu patch: %v", err)
		}
	}()
	wg.Wait()
	snapshot := application.SettingsSnapshot(ctx)
	if snapshot.Theme != storage.ThemeLight || snapshot.TunMTU != 1400 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestConcurrentSettingsSnapshotAndUpdate(t *testing.T) {
	application := testApp(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = application.SettingsSnapshot(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = application.PatchSettings(ctx, map[string]json.RawMessage{"tun_mtu": raw(1300)})
		}()
	}
	wg.Wait()
}

func TestPatchSettingsRejectsUnknownField(t *testing.T) {
	application := testApp(t)
	if err := application.PatchSettings(context.Background(), map[string]json.RawMessage{"bogus": raw(1)}); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}
