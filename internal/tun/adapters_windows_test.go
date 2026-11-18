//go:build windows

package tun

import (
	"context"
	"os"
	"testing"
)

// TestLiveAdapterClassification inspects the real host read-only. Enable with:
//
//	PROXY_TUI_LIVE=1 go test ./internal/tun -run TestLiveAdapterClassification -v
func TestLiveAdapterClassification(t *testing.T) {
	if os.Getenv("PROXY_TUI_LIVE") != "1" {
		t.Skip("set PROXY_TUI_LIVE=1 to inspect the real host")
	}
	facts, err := (SystemProbe{}).Facts(context.Background())
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	if len(facts.Adapters) == 0 {
		t.Fatal("adapter inspector returned no adapters")
	}
	for _, adapter := range facts.Adapters {
		t.Logf("if=%d alias=%q desc=%q pnp=%q hw=%v provider=%q service=%q manufacturer=%q component=%q type=%q virtual=%v up=%v class=%s reason=%q infra=%v",
			adapter.InterfaceIndex, adapter.InterfaceAlias, adapter.InterfaceDescription,
			adapter.PnPDeviceID, adapter.HardwareIDs, adapter.DriverProvider,
			adapter.DriverService, adapter.Manufacturer, adapter.ComponentID,
			adapter.InterfaceType, adapter.Virtual, adapter.Up, adapter.Class,
			adapter.ClassReason, adapter.Infrastructure)
	}
	foundHostOnly := false
	for _, adapter := range facts.Adapters {
		switch adapter.InterfaceAlias {
		case "以太网 2":
			if adapter.Class != AdapterHostOnly {
				t.Errorf("以太网 2 class = %s, want HOST_ONLY", adapter.Class)
			}
			foundHostOnly = true
		case "xray_tun":
			if adapter.Class != AdapterVPNTunnel {
				t.Errorf("xray_tun class = %s, want VPN_TUNNEL", adapter.Class)
			}
		}
	}
	if !foundHostOnly {
		t.Log("VirtualBox host-only adapter not present on this host")
	}
	for _, name := range facts.HostOnlyDevices {
		if name == "xray_tun" {
			t.Error("tunnel adapter must not be classified as host-only")
		}
	}
	for _, name := range facts.UnclassifiedVirtual {
		if name == "以太网 2" {
			t.Error("confirmed host-only adapter leaked into unclassified virtual")
		}
	}
}
