//go:build windows

package systemproxy

import (
	"context"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/proxytui/proxy-tui/internal/network"
)

func TestWindowsDefaultKey(t *testing.T) {
	want := `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	if defaultWindowsKey != want {
		t.Fatalf("defaultWindowsKey = %q, want %q", defaultWindowsKey, want)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, defaultWindowsKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open the real WinINet key: %v", err)
	}
	_ = key.Close()
}

func TestWindowsAdapterRoundTrip(t *testing.T) {
	key := `Software\proxy-tui-test\RoundTrip`
	_ = registry.DeleteKey(registry.CURRENT_USER, key)
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\proxy-tui-test`)
	})

	adapter := NewWindowsWithKey(key)
	ctx := context.Background()

	// Seed the "user's original" proxy configuration.
	seed, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := seed.SetDWordValue("ProxyEnable", 1); err != nil {
		t.Fatalf("seed enable: %v", err)
	}
	if err := seed.SetStringValue("ProxyServer", "http=10.1.2.3:3128;https=10.1.2.3:3128"); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := seed.SetStringValue("ProxyOverride", "*.corp.local"); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	_ = seed.Close()

	state, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !state.Enabled || state.Host != "10.1.2.3" || state.Port != 3128 {
		t.Fatalf("initial state = %+v", state)
	}

	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Data) == 0 {
		t.Fatal("snapshot has no data")
	}

	desired := network.Desired{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 7890, Bypass: "localhost,127.0.0.1"}
	if err := adapter.Apply(ctx, desired); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := adapter.Verify(ctx, desired); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	applied, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState after apply: %v", err)
	}
	if applied.Host != "127.0.0.1" || applied.Port != 7890 {
		t.Errorf("applied state = %+v", applied)
	}
	if applied.Bypass != "localhost,127.0.0.1" {
		t.Errorf("applied bypass = %q", applied.Bypass)
	}

	if err := adapter.Restore(ctx, snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState after restore: %v", err)
	}
	if !restored.Enabled || restored.Host != "10.1.2.3" || restored.Port != 3128 {
		t.Errorf("restored state = %+v", restored)
	}
	if restored.Bypass != "*.corp.local" {
		t.Errorf("restored bypass = %q", restored.Bypass)
	}
}

func TestWindowsAdapterRestoresMissingValues(t *testing.T) {
	key := `Software\proxy-tui-test\Empty`
	_ = registry.DeleteKey(registry.CURRENT_USER, key)
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\proxy-tui-test`)
	})

	adapter := NewWindowsWithKey(key)
	ctx := context.Background()

	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	desired := network.Desired{Enabled: true, Protocol: "socks5", Host: "127.0.0.1", Port: 7890}
	if err := adapter.Apply(ctx, desired); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := adapter.Restore(ctx, snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	state, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Enabled {
		t.Errorf("the proxy should be disabled after restoring an empty snapshot: %+v", state)
	}
}
