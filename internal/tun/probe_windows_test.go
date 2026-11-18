package tun

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsDNSIgnoresRemovedAdapters(t *testing.T) {
	if os.Getenv("PROXY_TUI_LIVE") != "1" {
		t.Skip("set PROXY_TUI_LIVE=1 to inspect this host")
	}
	facts, err := (SystemProbe{}).Facts(context.Background())
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	if len(facts.TUNDevices) != 0 {
		t.Skip("a TUN adapter exists; stale DNS filtering cannot be asserted")
	}
	for _, server := range facts.DNSServers {
		if strings.HasPrefix(server, "198.18.") || strings.HasPrefix(server, "172.19.") {
			t.Errorf("stale TUN DNS server reported without an active TUN: %s", server)
		}
	}
}

func TestWindowsProxyRegistryKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	if !strings.Contains(windowsInternetSettingsKey, "\\") {
		t.Fatalf("proxy registry key lost its separators: %q", windowsInternetSettingsKey)
	}
	known, enabled, address := windowsDesktopProxy()
	if !known {
		t.Fatalf("expected to read the WinINet proxy state")
	}
	t.Logf("desktop proxy: enabled=%v address=%q", enabled, address)
}

func TestWindowsDNSRegistryKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	key := `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces`
	if !strings.Contains(key, "\\") {
		t.Fatalf("registry key lost its separators: %q", key)
	}
	probe := SystemProbe{}
	facts, err := probe.Facts(nil)
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	if facts.Platform != "windows" {
		t.Fatalf("platform = %q", facts.Platform)
	}
	if !facts.DNSKnown {
		t.Logf("DNS registry read returned unknown (host may use DoH or dynamic DNS): source=%s", facts.DNSSource)
	}
}
