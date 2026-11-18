package tun

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Level is the outcome of one preflight check.
type Level string

// Preflight levels.
const (
	LevelReady   Level = "READY"
	LevelWarning Level = "WARNING"
	LevelBlocked Level = "BLOCKED"
	LevelUnknown Level = "UNKNOWN"
)

func levelRank(level Level) int {
	switch level {
	case LevelBlocked:
		return 3
	case LevelWarning:
		return 2
	case LevelUnknown:
		return 1
	default:
		return 0
	}
}

// Check is one probe result with an actionable explanation. Safety marks an
// UNKNOWN that must block activation because the risk cannot be ruled out.
type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Level  Level  `json:"level"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Safety bool   `json:"safety,omitempty"`
}

// Environment is everything Evaluate needs; it is pure data for testability.
type Environment struct {
	Facts            Facts
	Core             model.CoreKind
	CoreStatus       core.Status
	HasActiveProfile bool
	ProfileError     string
	Settings         Settings
	Overrides        []string
	Warnings         []string

	// OwnedTUN lists interfaces recorded in a TUN state whose owner still
	// matches the running core.
	OwnedTUN       []string
	TUNStateFound  bool
	TUNStateOwned  bool
	TUNStateActive bool
	TUNStateDetail string
	RecordedTUN    []string
	// OwnCorePIDs are processes started by ProxyTUI itself.
	OwnCorePIDs map[int]bool
}

// Report is the preflight result. Completed means the checks ran; it says
// nothing about whether activation is allowed.
type Report struct {
	GeneratedAt     time.Time      `json:"generated_at"`
	Platform        string         `json:"platform"`
	Core            model.CoreKind `json:"core"`
	CoreVersion     string         `json:"core_version,omitempty"`
	Level           Level          `json:"level"`
	Completed       bool           `json:"completed"`
	ActivationReady bool           `json:"activation_ready"`
	Checks          []Check        `json:"checks"`
	BlockingIssues  []string       `json:"blocking_issues,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
	UnknownFacts    []string       `json:"unknown_facts,omitempty"`
	Settings        Settings       `json:"settings"`
}

// Counts returns the number of checks per level.
func (r Report) Counts() map[Level]int {
	counts := map[Level]int{}
	for _, check := range r.Checks {
		counts[check.Level]++
	}
	return counts
}

// Summary renders a one-line summary.
func (r Report) Summary() string {
	counts := r.Counts()
	ready := "yes"
	if !r.ActivationReady {
		ready = "no"
	}
	return fmt.Sprintf("completed=%v activation_ready=%s %s: %d ready, %d warning, %d blocked, %d unknown",
		r.Completed, ready, r.Level, counts[LevelReady], counts[LevelWarning], counts[LevelBlocked], counts[LevelUnknown])
}

// Lines renders the structured result for the CLI and dialogs.
func (r Report) Lines() []string {
	ready := "no"
	if r.ActivationReady {
		ready = "yes"
	}
	lines := []string{
		fmt.Sprintf("preflight: completed=%v platform=%s", r.Completed, r.Platform),
		fmt.Sprintf("activation ready: %s", ready),
	}
	if len(r.BlockingIssues) > 0 {
		lines = append(lines, "blocking issues:")
		for _, issue := range r.BlockingIssues {
			lines = append(lines, "  - "+issue)
		}
	}
	if len(r.Warnings) > 0 {
		lines = append(lines, "warnings:")
		for _, warning := range r.Warnings {
			lines = append(lines, "  - "+warning)
		}
	}
	if len(r.UnknownFacts) > 0 {
		lines = append(lines, "unknown facts:")
		for _, unknown := range r.UnknownFacts {
			lines = append(lines, "  - "+unknown)
		}
	}
	lines = append(lines, "checks:")
	for _, check := range r.Checks {
		line := fmt.Sprintf("%-8s %-24s %s", check.Level, check.Title, check.Detail)
		if check.Hint != "" {
			line += " | " + check.Hint
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

// Evaluate runs every check against the collected facts.
func Evaluate(env Environment) Report {
	report := Report{
		GeneratedAt: time.Now(),
		Platform:    env.Facts.Platform,
		Core:        env.Core,
		CoreVersion: env.CoreStatus.Version,
		Completed:   true,
		Settings:    env.Settings,
	}
	add := func(check Check) { report.Checks = append(report.Checks, check) }

	add(checkPlatform(env.Facts))
	add(checkWSL(env.Facts))
	add(checkPrivilege(env.Facts))
	add(checkCore(env))
	add(checkCoreVersion(env))
	add(checkProfile(env))
	add(checkEgress(env))
	add(checkIPv4(env.Facts))
	add(checkIPv6(env))
	add(checkDNS(env.Facts))
	add(checkTUNDevices(env))
	add(checkUnclassifiedVirtual(env))
	add(checkVPN(env.Facts))
	add(checkFirewall(env.Facts))
	add(checkNftables(env.Facts))
	add(checkRouteConflicts(env.Facts))
	add(checkSystemProxy(env.Facts))
	add(checkWinHTTP(env.Facts))
	add(checkExternalAgents(env))
	add(checkProfileOverrides(env))

	level := LevelReady
	activationBlocked := false
	safetyUnknown := false
	for _, check := range report.Checks {
		if levelRank(check.Level) > levelRank(level) {
			level = check.Level
		}
		switch check.Level {
		case LevelBlocked:
			activationBlocked = true
			report.BlockingIssues = append(report.BlockingIssues, check.Title+": "+check.Detail)
		case LevelWarning:
			report.Warnings = append(report.Warnings, check.Title+": "+check.Detail)
		case LevelUnknown:
			report.UnknownFacts = append(report.UnknownFacts, check.Title+": "+check.Detail)
			if check.Safety {
				safetyUnknown = true
			}
		}
	}
	report.Level = level
	report.ActivationReady = !activationBlocked && !safetyUnknown
	return report
}

func checkPlatform(facts Facts) Check {
	check := Check{ID: "platform", Title: "Operating system", Safety: true}
	switch facts.Platform {
	case "windows", "linux", "darwin":
		check.Level = LevelReady
		check.Detail = facts.Platform
		if facts.Platform == "darwin" {
			check.Level = LevelWarning
			check.Detail = "darwin (unofficial until verified on real hardware)"
			check.Hint = "macOS TUN has not been validated by this project; expect rough edges"
		}
	default:
		check.Level = LevelBlocked
		check.Detail = facts.Platform
		check.Hint = "TUN is only supported on Windows, Linux and macOS"
	}
	return check
}

func checkWSL(facts Facts) Check {
	check := Check{ID: "wsl", Title: "WSL environment"}
	if facts.WSL {
		check.Level = LevelWarning
		check.Detail = "running inside WSL"
		check.Hint = "WSL does not reproduce native nftables/routing; validate on a native Linux host"
		return check
	}
	check.Level = LevelReady
	check.Detail = "native host"
	return check
}

func checkPrivilege(facts Facts) Check {
	check := Check{ID: "privilege", Title: "Privileges", Safety: true}
	if facts.Elevated {
		check.Level = LevelReady
		check.Detail = facts.ElevationDetail
		return check
	}
	check.Level = LevelBlocked
	check.Detail = facts.ElevationDetail
	switch facts.Platform {
	case "windows":
		check.Hint = "start proxy-tui from an elevated terminal (Run as administrator)"
	case "linux":
		check.Hint = "run with sudo or grant CAP_NET_ADMIN to the proxy core"
	case "darwin":
		check.Hint = "run the core as root (sudo) or install its privileged helper"
	default:
		check.Hint = "elevated privileges are required to create a TUN device"
	}
	return check
}

func checkCore(env Environment) Check {
	check := Check{ID: "core", Title: "Proxy core", Safety: true}
	if !env.Core.Valid() {
		check.Level = LevelBlocked
		check.Detail = "no core selected"
		check.Hint = "choose Mihomo or sing-box"
		return check
	}
	if !env.CoreStatus.Installed {
		check.Level = LevelBlocked
		check.Detail = env.Core.DisplayName() + " is not installed"
		check.Hint = "run: proxy core install " + string(env.Core)
		return check
	}
	check.Level = LevelReady
	check.Detail = env.Core.DisplayName() + " " + env.CoreStatus.Version
	return check
}

func checkCoreVersion(env Environment) Check {
	check := Check{ID: "core_version", Title: "Core version", Safety: true}
	minimum := MinimumVersion(env.Core)
	if minimum == "" {
		check.Level = LevelBlocked
		check.Detail = "unknown core"
		return check
	}
	version := env.CoreStatus.Version
	if version == "" {
		check.Level = LevelUnknown
		check.Detail = "version not reported"
		check.Hint = "install the latest " + env.Core.DisplayName() + " release"
		return check
	}
	if !VersionAtLeast(version, minimum) {
		check.Level = LevelBlocked
		check.Detail = fmt.Sprintf("%s %s < required %s", env.Core.DisplayName(), version, minimum)
		check.Hint = "run: proxy core install " + string(env.Core)
		return check
	}
	check.Level = LevelReady
	check.Detail = fmt.Sprintf("%s >= %s", version, minimum)
	return check
}

func checkProfile(env Environment) Check {
	check := Check{ID: "profile", Title: "Active profile", Safety: true}
	switch {
	case !env.HasActiveProfile:
		check.Level = LevelBlocked
		check.Detail = "no active profile for " + env.Core.DisplayName()
		check.Hint = "import a subscription and press enter on it"
	case env.ProfileError != "":
		check.Level = LevelBlocked
		check.Detail = env.ProfileError
		check.Hint = "fix or re-import the profile"
	default:
		check.Level = LevelReady
		check.Detail = "active profile is readable"
	}
	return check
}

func checkEgress(env Environment) Check {
	check := Check{ID: "egress", Title: "Default egress", Safety: true}
	facts := env.Facts
	if !facts.DefaultRouteKnown {
		check.Level = LevelUnknown
		check.Detail = "IPv4 default route could not be determined"
		check.Hint = "check the host routing table before enabling TUN"
		return check
	}
	detail := "IPv4 via " + facts.DefaultEgress
	if facts.DefaultRouteKnown6 {
		detail += ", IPv6 via " + facts.DefaultEgress6
	} else {
		detail += ", no IPv6 default route"
	}
	check.Level = LevelReady
	check.Detail = detail
	return check
}

func checkIPv4(facts Facts) Check {
	check := Check{ID: "ipv4", Title: "IPv4", Safety: true}
	if facts.IPv4 {
		check.Level = LevelReady
		check.Detail = "available"
		return check
	}
	check.Level = LevelBlocked
	check.Detail = "no IPv4 connectivity detected"
	check.Hint = "restore IPv4 connectivity before enabling TUN"
	return check
}

func checkIPv6(env Environment) Check {
	check := Check{ID: "ipv6", Title: "IPv6"}
	if env.Settings.IPv6 && !env.Facts.IPv6 {
		check.Level = LevelWarning
		check.Detail = "TUN IPv6 enabled but the host has no IPv6 connectivity"
		check.Hint = "disable IPv6 in TUN settings or fix the host IPv6 stack"
		return check
	}
	if !env.Settings.IPv6 && env.Facts.IPv6 {
		check.Level = LevelWarning
		check.Detail = "host has IPv6 but TUN IPv6 is disabled"
		check.Hint = "enable IPv6 in TUN settings, or expect IPv6 traffic to bypass the tunnel"
		return check
	}
	check.Level = LevelReady
	if env.Settings.IPv6 {
		check.Detail = "enabled on both sides"
	} else {
		check.Detail = "IPv6 absent or disabled consistently"
	}
	return check
}

func checkDNS(facts Facts) Check {
	check := Check{ID: "dns", Title: "DNS"}
	if !facts.DNSKnown || len(facts.DNSServers) == 0 {
		check.Level = LevelUnknown
		check.Detail = "system DNS servers could not be read"
		check.Hint = "TUN DNS hijack may still work, but DNS state cannot be verified"
		return check
	}
	check.Level = LevelReady
	check.Detail = strings.Join(facts.DNSServers, ", ")
	if facts.DNSSource != "" {
		check.Detail += " (" + facts.DNSSource + ")"
	}
	return check
}

func carryingFamilies(facts Facts, name string) []string {
	var families []string
	if facts.DefaultRouteKnown && facts.DefaultEgress == name {
		families = append(families, "IPv4")
	}
	if facts.DefaultRouteKnown6 && facts.DefaultEgress6 == name {
		families = append(families, "IPv6")
	}
	return families
}

func checkTUNDevices(env Environment) Check {
	check := Check{ID: "tun_devices", Title: "TUN conflicts", Safety: true}
	facts := env.Facts
	if len(facts.TUNDevices) == 0 {
		check.Level = LevelReady
		check.Detail = "no TUN adapters"
		return check
	}
	owned := map[string]bool{}
	for _, name := range env.OwnedTUN {
		owned[name] = true
	}
	recorded := map[string]bool{}
	for _, name := range env.RecordedTUN {
		recorded[name] = true
	}

	var externalCarrying, uncertainCarrying, staleCarrying []string
	var externalIdle, ours []string
	for _, name := range facts.TUNDevices {
		families := carryingFamilies(facts, name)
		switch {
		case owned[name]:
			ours = append(ours, name)
			if len(families) > 0 && !env.TUNStateActive {
				staleCarrying = append(staleCarrying, name+" ("+strings.Join(families, "+")+")")
			}
		case env.TUNStateFound && recorded[name]:
			if len(families) > 0 {
				uncertainCarrying = append(uncertainCarrying, name+" ("+strings.Join(families, "+")+")")
			} else {
				externalIdle = append(externalIdle, name+" (ownership unverified)")
			}
		default:
			if len(families) > 0 {
				externalCarrying = append(externalCarrying, name+" ("+strings.Join(families, "+")+")")
			} else {
				externalIdle = append(externalIdle, name)
			}
		}
	}

	switch {
	case len(externalCarrying) > 0:
		check.Level = LevelBlocked
		check.Detail = "external TUN carries the default route: " + strings.Join(externalCarrying, ", ")
		check.Hint = "stop the external proxy client or restore the default route before enabling TUN; proxy-tui never edits routes"
	case len(uncertainCarrying) > 0:
		check.Level = LevelBlocked
		check.Detail = "recorded TUN state cannot be verified and its interface carries the default route: " + strings.Join(uncertainCarrying, ", ")
		check.Hint = "run proxy network tun recover; if that fails, resolve the recorded adapter manually"
	case len(staleCarrying) > 0:
		check.Level = LevelBlocked
		check.Detail = "proxy-tui-owned TUN is stale and still carries the default route: " + strings.Join(staleCarrying, ", ")
		check.Hint = "run proxy network tun recover, then re-run preflight"
	case len(externalIdle) > 0:
		check.Level = LevelWarning
		check.Detail = "idle TUN adapters: " + strings.Join(externalIdle, ", ")
		check.Hint = "they do not carry the default route now, but may do so later; keep them stopped"
	case len(ours) > 0:
		check.Level = LevelReady
		check.Detail = "owned by proxy-tui: " + strings.Join(ours, ", ")
	default:
		check.Level = LevelReady
		check.Detail = "no conflicts"
	}
	return check
}

func checkUnclassifiedVirtual(env Environment) Check {
	check := Check{ID: "unknown_virtual", Title: "Virtual adapters", Safety: true}
	facts := env.Facts
	if len(facts.UnclassifiedVirtual) == 0 {
		check.Level = LevelReady
		check.Detail = "no unrecognised virtual adapters"
		return check
	}
	var carrying, idle []string
	for _, name := range facts.UnclassifiedVirtual {
		if len(carryingFamilies(facts, name)) > 0 {
			carrying = append(carrying, name)
		} else {
			idle = append(idle, name)
		}
	}
	if len(carrying) > 0 {
		check.Level = LevelBlocked
		check.Detail = "unrecognised virtual adapter carries the default route: " + strings.Join(carrying, ", ")
		check.Hint = "identify and stop that client, or remove it from the default route; proxy-tui never guesses ownership"
		return check
	}
	check.Level = LevelUnknown
	check.Detail = "unrecognised virtual adapters present: " + strings.Join(idle, ", ")
	check.Hint = "verify these are not tunnel adapters; if they are, stop them before enabling TUN"
	return check
}

func checkVPN(facts Facts) Check {
	check := Check{ID: "vpn", Title: "VPN interfaces"}
	if len(facts.VPNDevices) == 0 {
		check.Level = LevelReady
		check.Detail = "none detected"
		return check
	}
	check.Level = LevelWarning
	check.Detail = strings.Join(facts.VPNDevices, ", ")
	check.Hint = "a system VPN can conflict with core-managed routes and DNS"
	return check
}

func checkFirewall(facts Facts) Check {
	check := Check{ID: "firewall", Title: "Firewall"}
	if !facts.FirewallKnown {
		check.Level = LevelUnknown
		check.Detail = facts.FirewallDetail
		check.Hint = "proxy-tui does not change firewall rules; verify manually if traffic is blocked"
		return check
	}
	check.Level = LevelReady
	check.Detail = facts.FirewallDetail
	return check
}

func checkNftables(facts Facts) Check {
	check := Check{ID: "nftables", Title: "nftables (Linux)"}
	if facts.Platform != "linux" {
		check.Level = LevelReady
		check.Detail = "not applicable"
		return check
	}
	switch {
	case !facts.NftKnown:
		check.Level = LevelUnknown
		check.Detail = "nftables state unknown"
		check.Hint = "core auto-redirect may be unavailable"
	case facts.NftAvailable:
		check.Level = LevelReady
		check.Detail = facts.NftDetail
	default:
		check.Level = LevelWarning
		check.Detail = facts.NftDetail
		check.Hint = "basic TUN still works; auto-redirect needs nftables"
	}
	return check
}

func checkRouteConflicts(facts Facts) Check {
	check := Check{ID: "route_conflicts", Title: "Route conflicts", Safety: true}
	if facts.MultipleDefaults {
		check.Level = LevelWarning
		check.Detail = "multiple IPv4 default routes detected"
		check.Hint = "the core will replace the default route; confirm the other route is intentional"
		return check
	}
	check.Level = LevelReady
	check.Detail = "single IPv4 default route"
	return check
}

func checkSystemProxy(facts Facts) Check {
	check := Check{ID: "system_proxy", Title: "System proxy", Safety: true}
	if !facts.DesktopProxyKnown {
		check.Level = LevelUnknown
		check.Detail = "desktop proxy state could not be read"
		check.Hint = "TUN does not require the desktop proxy, but the conflict cannot be ruled out"
		return check
	}
	if !facts.DesktopProxyEnabled {
		check.Level = LevelReady
		check.Detail = "disabled"
		return check
	}
	check.Level = LevelWarning
	check.Detail = "enabled " + facts.DesktopProxyAddress
	check.Hint = "TUN bypasses the desktop proxy; keep it only if another client needs it, otherwise traffic may be proxied twice"
	if facts.DesktopProxyProbeKnown && !facts.DesktopProxyReachable {
		check.Detail += " (no listener; stale proxy?)"
		check.Hint = "the configured proxy port is not accepting connections; another client may have exited"
	}
	return check
}

func checkWinHTTP(facts Facts) Check {
	check := Check{ID: "winhttp", Title: "WinHTTP proxy"}
	if facts.Platform != "windows" {
		check.Level = LevelReady
		check.Detail = "not applicable"
		return check
	}
	if !facts.WinHTTPKnown {
		check.Level = LevelUnknown
		check.Detail = facts.WinHTTPDetail
		check.Hint = "winhttp state could not be read; it is separate from the WinINet system proxy"
		return check
	}
	if !facts.WinHTTPEnabled {
		check.Level = LevelReady
		check.Detail = "direct"
		return check
	}
	check.Level = LevelWarning
	check.Detail = "enabled: " + facts.WinHTTPDetail
	check.Hint = "WinHTTP is separate from the WinINet system proxy and is not changed by proxy-tui"
	return check
}

func checkExternalAgents(env Environment) Check {
	check := Check{ID: "external_agents", Title: "External proxy agents"}
	var external []string
	for _, agent := range env.Facts.ExternalAgents {
		if env.OwnCorePIDs[agent.PID] {
			continue
		}
		external = append(external, fmt.Sprintf("%s(%d)", agent.Name, agent.PID))
	}
	if len(external) == 0 {
		check.Level = LevelReady
		check.Detail = "none detected"
		return check
	}
	sort.Strings(external)
	check.Level = LevelWarning
	check.Detail = strings.Join(external, ", ")
	check.Hint = "another proxy client is running; stop it only with explicit user consent before relying on TUN"
	return check
}

func checkProfileOverrides(env Environment) Check {
	check := Check{ID: "profile_overrides", Title: "Profile TUN compatibility"}
	if len(env.Overrides) == 0 && len(env.Warnings) == 0 {
		check.Level = LevelReady
		check.Detail = "profile TUN settings are compatible"
		return check
	}
	check.Level = LevelWarning
	parts := make([]string, 0, 2)
	if len(env.Overrides) > 0 {
		parts = append(parts, "overridden: "+strings.Join(env.Overrides, ", "))
	}
	if len(env.Warnings) > 0 {
		parts = append(parts, env.Warnings...)
	}
	check.Detail = strings.Join(parts, "; ")
	check.Hint = "review the profile if its TUN block was intentional"
	return check
}
