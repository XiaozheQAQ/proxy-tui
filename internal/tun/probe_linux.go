//go:build linux

package tun

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func platformFacts(ctx context.Context, facts *Facts) {
	facts.Elevated = os.Geteuid() == 0
	if facts.Elevated {
		facts.ElevationDetail = "running as root"
	} else {
		facts.ElevationDetail = "running as uid " + strconv.Itoa(os.Geteuid()) + "; creating a TUN device requires root or CAP_NET_ADMIN"
	}
	servers, source, dnsKnown := readResolvConf()
	facts.DNSServers, facts.DNSSource, facts.DNSKnown = servers, source, dnsKnown
	facts.DesktopProxyKnown, facts.DesktopProxyEnabled, facts.DesktopProxyAddress = linuxDesktopProxy()
	facts.WSL = isWSL()

	nftPath, nftErr := exec.LookPath("nft")
	facts.NftKnown = true
	facts.NftAvailable = nftErr == nil
	if nftErr == nil {
		facts.NftDetail = nftPath
	} else {
		facts.NftDetail = "nft not found: core auto-redirect is unavailable, basic TUN still works"
	}
	facts.FirewallKnown = true
	facts.FirewallAvailable = nftErr == nil || lookPath("iptables")
	if facts.FirewallAvailable {
		facts.FirewallDetail = "nftables/iptables available for core-managed routing"
	} else {
		facts.FirewallDetail = "neither nft nor iptables found"
	}

	if count, known := linuxDefaultRouteCount(); known {
		facts.MultipleDefaults = count > 1
		if count > 1 {
			facts.Notes = append(facts.Notes, "multiple default routes detected")
		}
	}
	if !facts.DefaultRouteKnown {
		if name, ok := linuxDefaultRouteInterface(); ok {
			facts.DefaultEgress = name
			facts.DefaultRouteKnown = true
		}
	}
	if forwarding, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward"); err == nil {
		if strings.TrimSpace(string(forwarding)) == "0" {
			facts.Notes = append(facts.Notes, "net.ipv4.ip_forward=0: core auto-redirect needs forwarding")
		}
	}
}

func platformNotes(facts Facts) []string {
	var notes []string
	if facts.WSL {
		notes = append(notes, "WSL detected: TUN/nftables behaviour is not representative of a native Linux host")
	}
	return notes
}

func linuxDesktopProxy() (known, enabled bool, address string) {
	out, err := exec.Command("gsettings", "get", "org.gnome.system.proxy", "mode").Output()
	if err != nil {
		return false, false, ""
	}
	mode := strings.Trim(strings.TrimSpace(string(out)), "'")
	if mode != "manual" {
		return true, false, ""
	}
	hostOut, hostErr := exec.Command("gsettings", "get", "org.gnome.system.proxy.http", "host").Output()
	portOut, portErr := exec.Command("gsettings", "get", "org.gnome.system.proxy.http", "port").Output()
	if hostErr != nil || portErr != nil {
		return true, true, ""
	}
	host := strings.Trim(strings.TrimSpace(string(hostOut)), "'")
	port := strings.TrimSpace(string(portOut))
	if host == "" {
		return true, true, ""
	}
	return true, true, host + ":" + port
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// linuxDefaultRouteCount counts IPv4 default routes in /proc/net/route.
func linuxDefaultRouteCount() (int, bool) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return 0, false
	}
	count := 0
	for index, line := range strings.Split(string(data), "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		if fields[1] == "00000000" {
			flags, err := strconv.ParseInt(fields[3], 16, 32)
			if err == nil && flags&0x2 != 0 {
				count++
			}
		}
	}
	return count, true
}

// linuxDefaultRouteInterface reads the lowest metric default route. The first
// field of /proc/net/route is the interface name.
func linuxDefaultRouteInterface() (string, bool) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", false
	}
	best := ""
	bestMetric := int64(1<<62 - 1)
	for index, line := range strings.Split(string(data), "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" {
			continue
		}
		metric, err := strconv.ParseInt(fields[6], 10, 64)
		if err != nil {
			continue
		}
		if metric < bestMetric {
			bestMetric = metric
			best = fields[0]
		}
	}
	return best, best != ""
}
