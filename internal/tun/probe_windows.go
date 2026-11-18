//go:build windows

package tun

import (
	"context"
	"net"
	"os/exec"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func platformFacts(_ context.Context, facts *Facts) {
	facts.Elevated = currentProcessElevated()
	if facts.Elevated {
		facts.ElevationDetail = "process runs elevated"
	} else {
		facts.ElevationDetail = "process is not elevated; creating a Wintun adapter requires Administrator"
	}
	servers, source, known := windowsDNS(*facts)
	facts.DNSKnown = known
	facts.DNSServers = servers
	facts.DNSSource = source

	facts.DesktopProxyKnown, facts.DesktopProxyEnabled, facts.DesktopProxyAddress = windowsDesktopProxy()
	facts.WinHTTPKnown, facts.WinHTTPEnabled, facts.WinHTTPDetail = windowsWinHTTP()
	facts.ExternalAgents = append(facts.ExternalAgents, windowsExternalAgents()...)
	inspectWindowsAdapters(facts)

	facts.NftKnown = false
	facts.FirewallKnown = true
	facts.FirewallAvailable = true
	facts.FirewallDetail = "WinINet/Wintun do not require firewall rules; Windows Firewall stays untouched"
	if _, err := exec.LookPath("netsh"); err != nil {
		facts.FirewallKnown = false
		facts.FirewallAvailable = false
		facts.FirewallDetail = "netsh is unavailable; firewall state could not be inspected"
	}
}

func platformNotes(facts Facts) []string {
	if facts.WSL {
		return []string{"WSL is not a supported Windows TUN target"}
	}
	return nil
}

const windowsInternetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const windowsNetworkClassKey = `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`

func windowsWinHTTP() (known, enabled bool, detail string) {
	out, err := exec.Command("netsh", "winhttp", "show", "proxy").CombinedOutput()
	if err != nil {
		return false, false, "netsh winhttp is unavailable"
	}
	text := string(out)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "direct access"), strings.Contains(text, "直接访问"):
		return true, false, "direct"
	case strings.Contains(lower, "proxy server"), strings.Contains(text, "代理服务器"):
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, ":") {
				return true, true, line
			}
		}
		return true, true, "enabled"
	}
	return false, false, "unrecognised netsh winhttp output"
}

func windowsExternalAgents() []ExternalAgent {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	var agents []ExternalAgent
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if IsProxyProcessName(name) {
			agents = append(agents, ExternalAgent{Name: name, PID: int(entry.ProcessID)})
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return agents
}

func windowsDesktopProxy() (known, enabled bool, address string) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false, ""
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil {
		return false, false, ""
	}
	server, _, serverErr := key.GetStringValue("ProxyServer")
	if serverErr != nil || strings.TrimSpace(server) == "" {
		return true, false, ""
	}
	address = firstProxyAddress(server)
	if address == "" {
		return true, value == 1, ""
	}
	return true, value == 1, address
}

// firstProxyAddress accepts "host:port" and "http=host:port;https=..."
func firstProxyAddress(server string) string {
	entries := []string{server}
	if strings.Contains(server, "=") {
		entries = strings.Split(server, ";")
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		value := entry
		if _, v, ok := strings.Cut(entry, "="); ok {
			value = strings.TrimSpace(v)
		}
		host, port, err := net.SplitHostPort(value)
		if err != nil || host == "" || port == "" {
			continue
		}
		return host + ":" + port
	}
	return ""
}

func currentProcessElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

func windowsDNS(facts Facts) ([]string, string, bool) {
	// Only interfaces that currently exist may contribute DNS servers; removed
	// Wintun adapters leave their DNS values behind in the registry.
	active := map[string]bool{}
	present := map[string]bool{}
	for _, iface := range facts.Interfaces {
		present[lowerASCII(iface.Name)] = true
	}
	for name, conn := range windowsConnections() {
		if present[name] {
			active[strings.ToLower(conn.guid)] = true
		}
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces`, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, "registry", false
	}
	defer func() { _ = key.Close() }()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil, "registry", false
	}
	seen := map[string]bool{}
	var servers []string
	for _, name := range names {
		if len(active) > 0 && !active[strings.ToLower(name)] {
			continue
		}
		sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		for _, value := range []string{"NameServer", "DhcpNameServer"} {
			raw, _, err := sub.GetStringValue(value)
			if err != nil {
				continue
			}
			for _, server := range strings.FieldsFunc(raw, func(r rune) bool {
				return r == ',' || r == ' ' || r == ';'
			}) {
				server = strings.TrimSpace(server)
				if server != "" && !seen[server] {
					seen[server] = true
					servers = append(servers, server)
				}
			}
		}
		_ = sub.Close()
	}
	sort.Strings(servers)
	if len(servers) == 0 {
		return nil, "registry", false
	}
	return servers, "registry", true
}
