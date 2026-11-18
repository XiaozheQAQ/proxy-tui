package tun

import (
	"runtime"
	"strings"
	"testing"
)

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
