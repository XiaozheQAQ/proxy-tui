package tun

import "sort"

// InterfaceInfo is one network interface observed on the host.
type InterfaceInfo struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	Addresses []string `json:"addresses,omitempty"`
	Flags     string   `json:"flags,omitempty"`
}

// Facts is the platform observation used by preflight and verification.
type Facts struct {
	Platform           string          `json:"platform"`
	Elevated           bool            `json:"elevated"`
	ElevationDetail    string          `json:"elevation_detail,omitempty"`
	Interfaces         []InterfaceInfo `json:"interfaces,omitempty"`
	DefaultEgress      string          `json:"default_egress,omitempty"`
	DefaultRouteKnown  bool            `json:"default_route_known"`
	DefaultEgress6     string          `json:"default_egress6,omitempty"`
	DefaultRouteKnown6 bool            `json:"default_route_known6"`
	IPv4               bool            `json:"ipv4"`
	IPv6               bool            `json:"ipv6"`
	DNSKnown           bool            `json:"dns_known"`
	DNSServers         []string        `json:"dns_servers,omitempty"`
	DNSSource          string          `json:"dns_source,omitempty"`
	TUNDevices         []string        `json:"tun_devices,omitempty"`
	VPNDevices         []string        `json:"vpn_devices,omitempty"`
	// UnclassifiedVirtual are virtual adapters that could not be attributed to
	// a TUN or VPN. They are never guessed to be ours.
	UnclassifiedVirtual []string `json:"unclassified_virtual,omitempty"`
	// HostOnlyDevices are adapters positively identified as host-only virtual
	// networks by driver and device identity.
	HostOnlyDevices []string `json:"host_only_devices,omitempty"`
	// UnknownAdapters are present adapters whose identity could not be
	// classified. They are never assumed to be harmless.
	UnknownAdapters     []string      `json:"unknown_adapters,omitempty"`
	Adapters            []AdapterInfo `json:"adapters,omitempty"`
	MultipleDefaults    bool          `json:"multiple_defaults"`
	DesktopProxyKnown   bool          `json:"desktop_proxy_known"`
	DesktopProxyEnabled bool          `json:"desktop_proxy_enabled"`
	DesktopProxyAddress string        `json:"desktop_proxy_address,omitempty"`
	// DesktopProxyProbeKnown/Reachable tell whether the configured proxy
	// endpoint currently accepts TCP connections.
	DesktopProxyProbeKnown bool            `json:"desktop_proxy_probe_known"`
	DesktopProxyReachable  bool            `json:"desktop_proxy_reachable"`
	WinHTTPKnown           bool            `json:"winhttp_known"`
	WinHTTPEnabled         bool            `json:"winhttp_enabled"`
	WinHTTPDetail          string          `json:"winhttp_detail,omitempty"`
	ExternalAgents         []ExternalAgent `json:"external_agents,omitempty"`
	FirewallKnown          bool            `json:"firewall_known"`
	FirewallAvailable      bool            `json:"firewall_available"`
	FirewallDetail         string          `json:"firewall_detail,omitempty"`
	NftKnown               bool            `json:"nft_known"`
	NftAvailable           bool            `json:"nft_available"`
	NftDetail              string          `json:"nft_detail,omitempty"`
	WSL                    bool            `json:"wsl"`
	Notes                  []string        `json:"notes,omitempty"`
}

// ExternalAgent is a proxy-like process observed on the host.
type ExternalAgent struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
}

// IsProxyProcessName reports whether a process name looks like a proxy client
// or proxy core.
func IsProxyProcessName(name string) bool {
	lower := lowerASCII(name)
	for _, token := range []string{"mihomo", "sing-box", "singbox", "xray", "v2ray", "clash", "verge", "nekoray", "wintun", "hysteria"} {
		if containsToken(lower, token) {
			return true
		}
	}
	return false
}

// HasToken reports a case-insensitive substring match used by platform probes.
func HasToken(value, token string) bool { return containsToken(lowerASCII(value), token) }

// UniqueStrings sorts and deduplicates a string slice.
func UniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// InterfaceNames returns the interface names.
func (f Facts) InterfaceNames() []string {
	names := make([]string, 0, len(f.Interfaces))
	for _, iface := range f.Interfaces {
		names = append(names, iface.Name)
	}
	sort.Strings(names)
	return names
}

// HasInterface reports whether the interface is currently present.
func (f Facts) HasInterface(name string) bool {
	for _, iface := range f.Interfaces {
		if iface.Name == name {
			return true
		}
	}
	return false
}

// NewInterfaces returns interfaces present in current but not in before.
func NewInterfaces(before, current Facts) []string {
	existing := map[string]bool{}
	for _, name := range before.InterfaceNames() {
		existing[name] = true
	}
	var added []string
	for _, name := range current.InterfaceNames() {
		if !existing[name] {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	return added
}

// TUNCandidates filters interface names that look like a core-created TUN.
func TUNCandidates(names []string) []string {
	var out []string
	for _, name := range names {
		if IsTUNName(name) {
			out = append(out, name)
		}
	}
	return out
}

// IsTUNName reports whether the interface name looks like a TUN adapter.
func IsTUNName(name string) bool {
	lower := lowerASCII(name)
	for _, token := range []string{"tun", "utun", "wintun", "mihomo", "sing-box", "singbox", "meta"} {
		if containsToken(lower, token) {
			return true
		}
	}
	return false
}

// IsVPNName reports whether the interface name looks like a VPN adapter.
func IsVPNName(name string) bool {
	lower := lowerASCII(name)
	for _, token := range []string{"vpn", "wireguard", "wg", "tailscale", "zerotier", "nordlynx", "openvpn", "proton"} {
		if containsToken(lower, token) {
			return true
		}
	}
	return false
}

func lowerASCII(value string) string {
	out := []byte(value)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + ('a' - 'A')
		}
	}
	return string(out)
}

func containsToken(value, token string) bool {
	for i := 0; i+len(token) <= len(value); i++ {
		if value[i:i+len(token)] == token {
			return true
		}
	}
	return false
}
