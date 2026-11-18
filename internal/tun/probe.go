package tun

import (
	"context"
	"net"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Probe observes the host network state.
type Probe interface {
	Facts(ctx context.Context) (Facts, error)
}

// SystemProbe reads the real host network state.
type SystemProbe struct{}

// NewSystemProbe returns the platform probe.
func NewSystemProbe() Probe { return SystemProbe{} }

// Facts implements Probe.
func (SystemProbe) Facts(ctx context.Context) (Facts, error) {
	facts := Facts{Platform: runtime.GOOS}
	facts.Notes = nil

	interfaces, err := net.Interfaces()
	if err != nil {
		facts.DefaultRouteKnown = false
		facts.Notes = append(facts.Notes, "cannot enumerate interfaces: "+err.Error())
	}
	for _, iface := range interfaces {
		entry := InterfaceInfo{Name: iface.Name, Index: iface.Index, Flags: iface.Flags.String()}
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				entry.Addresses = append(entry.Addresses, addr.String())
			}
		}
		facts.Interfaces = append(facts.Interfaces, entry)
		switch {
		case iface.Flags&net.FlagLoopback != 0:
		case IsTUNName(iface.Name):
			facts.TUNDevices = append(facts.TUNDevices, iface.Name)
		case IsVPNName(iface.Name):
			facts.VPNDevices = append(facts.VPNDevices, iface.Name)
		}
	}
	sort.Strings(facts.TUNDevices)
	sort.Strings(facts.VPNDevices)

	egress, known := detectDefaultEgress(facts)
	facts.DefaultEgress = egress
	facts.DefaultRouteKnown = known
	if !known {
		facts.Notes = append(facts.Notes, "default egress interface could not be determined")
	}
	egress6, known6 := detectDefaultEgress6(facts)
	facts.DefaultEgress6 = egress6
	facts.DefaultRouteKnown6 = known6

	facts.IPv4 = detectFamily(facts, false)
	facts.IPv6 = detectFamily(facts, true) || known6

	platformFacts(ctx, &facts)
	facts.Notes = append(facts.Notes, platformNotes(facts)...)
	facts.TUNDevices = UniqueStrings(facts.TUNDevices)
	facts.VPNDevices = UniqueStrings(facts.VPNDevices)
	facts.UnclassifiedVirtual = UniqueStrings(facts.UnclassifiedVirtual)
	facts.ExternalAgents = uniqueAgents(facts.ExternalAgents)

	if facts.DesktopProxyEnabled && facts.DesktopProxyAddress != "" {
		facts.DesktopProxyProbeKnown = true
		conn, err := net.DialTimeout("tcp", facts.DesktopProxyAddress, 300*time.Millisecond)
		if err == nil {
			facts.DesktopProxyReachable = true
			_ = conn.Close()
		}
	}
	return facts, nil
}

func uniqueAgents(agents []ExternalAgent) []ExternalAgent {
	seen := map[int]bool{}
	out := make([]ExternalAgent, 0, len(agents))
	for _, agent := range agents {
		if agent.PID <= 0 || seen[agent.PID] {
			continue
		}
		seen[agent.PID] = true
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// detectDefaultEgress6 mirrors detectDefaultEgress for IPv6.
func detectDefaultEgress6(facts Facts) (string, bool) {
	conn, err := net.DialUDP("udp6", nil, &net.UDPAddr{IP: net.ParseIP("2606:4700:4700::1111"), Port: 53})
	if err != nil {
		return "", false
	}
	local := conn.LocalAddr().(*net.UDPAddr).IP.String()
	_ = conn.Close()
	for _, iface := range facts.Interfaces {
		for _, addr := range iface.Addresses {
			if strings.HasPrefix(addr, local+"/") {
				return iface.Name, true
			}
		}
	}
	return "", false
}

// detectDefaultEgress uses a UDP socket (no packets are sent) to let the OS
// choose the route, then maps the local address back to an interface.
func detectDefaultEgress(facts Facts) (string, bool) {
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 53})
	if err != nil {
		return "", false
	}
	local := conn.LocalAddr().(*net.UDPAddr).IP.String()
	_ = conn.Close()
	for _, iface := range facts.Interfaces {
		for _, addr := range iface.Addresses {
			if strings.HasPrefix(addr, local+"/") {
				return iface.Name, true
			}
		}
	}
	return "", false
}

func detectFamily(facts Facts, ipv6 bool) bool {
	network := "udp4"
	target := &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 53}
	if ipv6 {
		network = "udp6"
		target = &net.UDPAddr{IP: net.ParseIP("2606:4700:4700::1111"), Port: 53}
	}
	conn, err := net.DialUDP(network, nil, target)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// probeTimeout keeps every platform probe bounded.
const probeTimeout = 5 * time.Second
