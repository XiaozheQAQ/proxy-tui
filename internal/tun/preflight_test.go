package tun

import (
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func readyEnvironment() Environment {
	return Environment{
		Facts: Facts{
			Platform:               "windows",
			Elevated:               true,
			ElevationDetail:        "elevated",
			Interfaces:             []InterfaceInfo{{Name: "WLAN"}},
			DefaultEgress:          "WLAN",
			DefaultRouteKnown:      true,
			IPv4:                   true,
			DNSKnown:               true,
			DNSServers:             []string{"1.1.1.1", "223.5.5.5"},
			DNSSource:              "registry",
			DesktopProxyKnown:      true,
			DesktopProxyEnabled:    false,
			DesktopProxyProbeKnown: true,
			DesktopProxyReachable:  false,
			WinHTTPKnown:           true,
			WinHTTPEnabled:         false,
			FirewallKnown:          true,
			FirewallAvailable:      true,
		},
		Core:             model.CoreMihomo,
		CoreStatus:       core.Status{Core: model.CoreMihomo, Installed: true, Version: "1.19.34"},
		HasActiveProfile: true,
		Settings:         Defaults(),
	}
}

func levelOf(report Report, id string) Level {
	for _, check := range report.Checks {
		if check.ID == id {
			return check.Level
		}
	}
	return ""
}

func TestPreflightReady(t *testing.T) {
	report := Evaluate(readyEnvironment())
	if report.Level != LevelReady {
		t.Fatalf("level = %s: %s", report.Level, report.Summary())
	}
	for _, id := range []string{"platform", "privilege", "core", "core_version", "profile", "egress", "ipv4", "dns"} {
		if levelOf(report, id) != LevelReady {
			t.Errorf("%s = %s", id, levelOf(report, id))
		}
	}
}

func TestPreflightBlocksWithoutPrivileges(t *testing.T) {
	env := readyEnvironment()
	env.Facts.Elevated = false
	env.Facts.ElevationDetail = "not elevated"
	report := Evaluate(env)
	if report.Level != LevelBlocked {
		t.Fatalf("level = %s", report.Level)
	}
	if levelOf(report, "privilege") != LevelBlocked {
		t.Errorf("privilege = %s", levelOf(report, "privilege"))
	}
	for _, check := range report.Checks {
		if check.ID == "privilege" && check.Hint == "" {
			t.Error("blocked check has no actionable hint")
		}
	}
}

func TestPreflightBlocksMissingCoreAndProfile(t *testing.T) {
	env := readyEnvironment()
	env.CoreStatus.Installed = false
	if report := Evaluate(env); levelOf(report, "core") != LevelBlocked {
		t.Errorf("core = %s", levelOf(report, "core"))
	}
	env = readyEnvironment()
	env.HasActiveProfile = false
	if report := Evaluate(env); levelOf(report, "profile") != LevelBlocked {
		t.Errorf("profile = %s", levelOf(report, "profile"))
	}
}

func TestPreflightBlocksOldCore(t *testing.T) {
	env := readyEnvironment()
	env.Core = model.CoreSingBox
	env.CoreStatus = core.Status{Core: model.CoreSingBox, Installed: true, Version: "1.10.7"}
	report := Evaluate(env)
	if levelOf(report, "core_version") != LevelBlocked {
		t.Errorf("core_version = %s", levelOf(report, "core_version"))
	}
}

func TestPreflightWSLAndNft(t *testing.T) {
	env := readyEnvironment()
	env.Facts.Platform = "linux"
	env.Facts.WSL = true
	env.Facts.NftKnown = true
	env.Facts.NftAvailable = false
	env.Facts.NftDetail = "nft not found"
	report := Evaluate(env)
	if levelOf(report, "wsl") != LevelWarning {
		t.Errorf("wsl = %s", levelOf(report, "wsl"))
	}
	if levelOf(report, "nftables") != LevelWarning {
		t.Errorf("nftables = %s", levelOf(report, "nftables"))
	}
}

func TestPreflightDarwinUnofficial(t *testing.T) {
	env := readyEnvironment()
	env.Facts.Platform = "darwin"
	report := Evaluate(env)
	if levelOf(report, "platform") != LevelWarning {
		t.Errorf("platform = %s", levelOf(report, "platform"))
	}
	if report.Level != LevelWarning {
		t.Errorf("overall = %s", report.Level)
	}
}

func TestPreflightWarnings(t *testing.T) {
	env := readyEnvironment()
	env.Facts.IPv6 = true
	env.Facts.TUNDevices = []string{"xray_tun"}
	env.Facts.VPNDevices = []string{"wg0"}
	env.Facts.MultipleDefaults = true
	report := Evaluate(env)
	if levelOf(report, "ipv6") != LevelWarning {
		t.Errorf("ipv6 = %s", levelOf(report, "ipv6"))
	}
	if levelOf(report, "tun_devices") != LevelWarning {
		t.Errorf("tun_devices = %s", levelOf(report, "tun_devices"))
	}
	if levelOf(report, "vpn") != LevelWarning {
		t.Errorf("vpn = %s", levelOf(report, "vpn"))
	}
	if levelOf(report, "route_conflicts") != LevelWarning {
		t.Errorf("route_conflicts = %s", levelOf(report, "route_conflicts"))
	}
	if report.Level != LevelWarning {
		t.Errorf("overall = %s", report.Level)
	}
}

func TestPreflightUnknownDNS(t *testing.T) {
	env := readyEnvironment()
	env.Facts.DNSKnown = false
	env.Facts.DNSServers = nil
	report := Evaluate(env)
	if levelOf(report, "dns") != LevelUnknown {
		t.Errorf("dns = %s", levelOf(report, "dns"))
	}
	if report.Level != LevelUnknown {
		t.Errorf("overall = %s", report.Level)
	}
}

func TestPreflightOverridesWarning(t *testing.T) {
	env := readyEnvironment()
	env.Overrides = []string{"tun.stack (was gvisor)"}
	env.Warnings = []string{"the profile already enabled TUN"}
	report := Evaluate(env)
	if levelOf(report, "profile_overrides") != LevelWarning {
		t.Errorf("profile_overrides = %s", levelOf(report, "profile_overrides"))
	}
	if !strings.Contains(report.Summary(), "WARNING") {
		t.Errorf("summary = %q", report.Summary())
	}
}

func TestPreflightSystemProxyCheck(t *testing.T) {
	env := readyEnvironment()
	if report := Evaluate(env); levelOf(report, "system_proxy") != LevelReady {
		t.Errorf("disabled proxy = %s", levelOf(report, "system_proxy"))
	}
	env.Facts.DesktopProxyEnabled = true
	env.Facts.DesktopProxyAddress = "127.0.0.1:10808"
	report := Evaluate(env)
	if levelOf(report, "system_proxy") != LevelWarning {
		t.Errorf("enabled proxy = %s", levelOf(report, "system_proxy"))
	}
	env.Facts.DesktopProxyKnown = false
	report = Evaluate(env)
	if levelOf(report, "system_proxy") != LevelUnknown {
		t.Errorf("unknown proxy = %s", levelOf(report, "system_proxy"))
	}
}

func TestExternalTUNCarryingIPv4Blocks(t *testing.T) {
	env := readyEnvironment()
	env.Facts.TUNDevices = []string{"xray_tun"}
	env.Facts.DefaultEgress = "xray_tun"
	report := Evaluate(env)
	if levelOf(report, "tun_devices") != LevelBlocked {
		t.Errorf("tun_devices = %s", levelOf(report, "tun_devices"))
	}
	if report.ActivationReady || !report.Completed {
		t.Errorf("report = %+v", report)
	}
	if len(report.BlockingIssues) == 0 {
		t.Error("blocking issues should be listed")
	}
}

func TestExternalTUNCarryingIPv6Blocks(t *testing.T) {
	env := readyEnvironment()
	env.Facts.TUNDevices = []string{"xray_tun"}
	env.Facts.DefaultEgress = "WLAN"
	env.Facts.DefaultEgress6 = "xray_tun"
	env.Facts.DefaultRouteKnown6 = true
	env.Facts.IPv6 = true
	report := Evaluate(env)
	if levelOf(report, "tun_devices") != LevelBlocked {
		t.Errorf("tun_devices = %s", levelOf(report, "tun_devices"))
	}
	detail := ""
	for _, check := range report.Checks {
		if check.ID == "tun_devices" {
			detail = check.Detail
		}
	}
	if !strings.Contains(detail, "IPv6") {
		t.Errorf("detail = %q", detail)
	}
}

func TestIdleExternalTUNWarnsButRemainsReady(t *testing.T) {
	env := readyEnvironment()
	env.Facts.TUNDevices = []string{"idle_tun"}
	report := Evaluate(env)
	if levelOf(report, "tun_devices") != LevelWarning {
		t.Errorf("tun_devices = %s", levelOf(report, "tun_devices"))
	}
	if !report.ActivationReady {
		t.Errorf("an idle external TUN must not block activation: %+v", report.BlockingIssues)
	}
}

func TestUnknownVirtualAdapter(t *testing.T) {
	env := readyEnvironment()
	env.Facts.UnclassifiedVirtual = []string{"mystery0"}
	report := Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelUnknown {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if report.ActivationReady {
		t.Error("a safety UNKNOWN must block activation")
	}

	env.Facts.DefaultEgress = "mystery0"
	report = Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelBlocked {
		t.Errorf("carrying virtual = %s", levelOf(report, "unknown_virtual"))
	}
}

func TestExternalSystemProxyWarns(t *testing.T) {
	env := readyEnvironment()
	env.Facts.DesktopProxyEnabled = true
	env.Facts.DesktopProxyAddress = "127.0.0.1:10808"
	env.Facts.DesktopProxyReachable = true
	report := Evaluate(env)
	if levelOf(report, "system_proxy") != LevelWarning {
		t.Errorf("system_proxy = %s", levelOf(report, "system_proxy"))
	}
	if !report.ActivationReady {
		t.Errorf("a live external proxy is a warning, not a blocker: %+v", report.BlockingIssues)
	}
}

func TestStaleSystemProxyWarns(t *testing.T) {
	env := readyEnvironment()
	env.Facts.DesktopProxyEnabled = true
	env.Facts.DesktopProxyAddress = "127.0.0.1:10808"
	env.Facts.DesktopProxyProbeKnown = true
	env.Facts.DesktopProxyReachable = false
	report := Evaluate(env)
	detail := ""
	for _, check := range report.Checks {
		if check.ID == "system_proxy" {
			detail = check.Detail
		}
	}
	if !strings.Contains(detail, "stale proxy") {
		t.Errorf("detail = %q", detail)
	}
	if !report.ActivationReady {
		t.Error("a stale proxy warning must not block activation")
	}
}

func TestUnknownTUNOwnershipBlocks(t *testing.T) {
	env := readyEnvironment()
	env.Facts.TUNDevices = []string{"xray_tun"}
	env.Facts.DefaultEgress = "xray_tun"
	env.TUNStateFound = true
	env.RecordedTUN = []string{"xray_tun"}
	env.TUNStateOwned = false
	report := Evaluate(env)
	if levelOf(report, "tun_devices") != LevelBlocked {
		t.Errorf("tun_devices = %s", levelOf(report, "tun_devices"))
	}
}

func TestActivationReadyFullDetermination(t *testing.T) {
	report := Evaluate(readyEnvironment())
	if !report.Completed || !report.ActivationReady {
		t.Fatalf("report = %+v", report)
	}
	if report.Level != LevelReady {
		t.Errorf("level = %s", report.Level)
	}
	if len(report.BlockingIssues) != 0 {
		t.Errorf("blocking issues = %v", report.BlockingIssues)
	}
}

func TestPreflightCompletedButNotReady(t *testing.T) {
	env := readyEnvironment()
	env.Facts.Elevated = false
	report := Evaluate(env)
	if !report.Completed {
		t.Error("preflight should be completed")
	}
	if report.ActivationReady {
		t.Error("activation must be refused")
	}
	if len(report.BlockingIssues) == 0 {
		t.Error("blocking issues should be listed")
	}
	lines := strings.Join(report.Lines(), "\n")
	for _, want := range []string{"activation ready: no", "blocking issues:", "checks:"} {
		if !strings.Contains(lines, want) {
			t.Errorf("lines missing %q", want)
		}
	}
}

func TestHostOnlyAdapterIsReady(t *testing.T) {
	env := readyEnvironment()
	env.Facts.HostOnlyDevices = []string{"vboxnet"}
	report := Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelReady {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if !report.ActivationReady {
		t.Errorf("confirmed host-only must not block activation: %+v", report.BlockingIssues)
	}
}

func TestHostOnlyAdapterCarryingDefaultWarns(t *testing.T) {
	env := readyEnvironment()
	env.Facts.HostOnlyDevices = []string{"vboxnet"}
	env.Facts.DefaultEgress = "vboxnet"
	report := Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelWarning {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if !report.ActivationReady {
		t.Error("a confirmed host-only adapter is not a tunnel conflict")
	}
	if levelOf(report, "tun_devices") != LevelReady {
		t.Errorf("host-only leaked into TUN conflicts: %s", levelOf(report, "tun_devices"))
	}
}

func TestUnknownAdapterCarryingDefaultBlocks(t *testing.T) {
	env := readyEnvironment()
	env.Facts.UnknownAdapters = []string{"mystery0"}
	env.Facts.DefaultEgress = "mystery0"
	report := Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelBlocked {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if report.ActivationReady {
		t.Error("an unknown adapter carrying the default route must block")
	}
}

func TestUnknownAdapterIdleIsSafetyUnknown(t *testing.T) {
	env := readyEnvironment()
	env.Facts.UnknownAdapters = []string{"mystery0"}
	report := Evaluate(env)
	if levelOf(report, "unknown_virtual") != LevelUnknown {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if report.ActivationReady {
		t.Error("a safety UNKNOWN must block activation")
	}
}

func TestHostOnlyAndVPNCoexist(t *testing.T) {
	env := readyEnvironment()
	env.Facts.HostOnlyDevices = []string{"vboxnet"}
	env.Facts.VPNDevices = []string{"wg0"}
	report := Evaluate(env)
	if levelOf(report, "vpn") != LevelWarning {
		t.Errorf("vpn = %s", levelOf(report, "vpn"))
	}
	if levelOf(report, "unknown_virtual") != LevelReady {
		t.Errorf("unknown_virtual = %s", levelOf(report, "unknown_virtual"))
	}
	if !report.ActivationReady {
		t.Error("idle VPN + host-only must not block")
	}

	env.Facts.TUNDevices = []string{"wg_tun"}
	env.Facts.DefaultEgress = "wg_tun"
	report = Evaluate(env)
	if levelOf(report, "tun_devices") != LevelBlocked {
		t.Errorf("tunnel default route = %s", levelOf(report, "tun_devices"))
	}
}

func TestWinHTTPProxyWarning(t *testing.T) {
	env := readyEnvironment()
	env.Facts.WinHTTPEnabled = true
	env.Facts.WinHTTPDetail = "127.0.0.1:10809"
	report := Evaluate(env)
	if levelOf(report, "winhttp") != LevelWarning {
		t.Errorf("winhttp = %s", levelOf(report, "winhttp"))
	}
	if !report.ActivationReady {
		t.Error("WinHTTP warning must not block activation")
	}
}

func TestExternalAgentsWarning(t *testing.T) {
	env := readyEnvironment()
	env.Facts.ExternalAgents = []ExternalAgent{{Name: "xray", PID: 100}}
	env.OwnCorePIDs = map[int]bool{200: true}
	report := Evaluate(env)
	if levelOf(report, "external_agents") != LevelWarning {
		t.Errorf("external_agents = %s", levelOf(report, "external_agents"))
	}
	if !report.ActivationReady {
		t.Error("external agents are informational warnings")
	}

	env.OwnCorePIDs = map[int]bool{100: true}
	report = Evaluate(env)
	if levelOf(report, "external_agents") != LevelReady {
		t.Errorf("own core reported as external: %s", levelOf(report, "external_agents"))
	}
}

func TestPreflightBlockedDoesNotLookReady(t *testing.T) {
	env := readyEnvironment()
	env.Facts.Elevated = false
	env.Facts.DNSKnown = false
	report := Evaluate(env)
	if report.ActivationReady {
		t.Fatal("report must not be activation ready")
	}
	if !report.Completed {
		t.Fatal("preflight should still be marked completed")
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "BLOCKED") {
		t.Errorf("lines = %s", lines)
	}
}
