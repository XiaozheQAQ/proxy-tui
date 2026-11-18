//go:build darwin

package tun

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func platformFacts(_ context.Context, facts *Facts) {
	facts.Elevated = os.Geteuid() == 0
	if facts.Elevated {
		facts.ElevationDetail = "running as root"
	} else {
		facts.ElevationDetail = "running as uid " + strconv.Itoa(os.Geteuid()) + "; creating a utun device requires root"
	}
	servers, source, dnsKnown := readResolvConf()
	facts.DNSServers, facts.DNSSource, facts.DNSKnown = servers, source, dnsKnown
	facts.DesktopProxyKnown, facts.DesktopProxyEnabled, facts.DesktopProxyAddress = darwinDesktopProxy()
	facts.NftKnown = false
	facts.FirewallKnown = true
	_, err := exec.LookPath("pfctl")
	facts.FirewallAvailable = err == nil
	facts.FirewallDetail = "pf is not modified by proxy-tui"
	if err != nil {
		facts.FirewallKnown = false
		facts.FirewallDetail = "pfctl not found"
	}
}

func darwinDesktopProxy() (known, enabled bool, address string) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return false, false, ""
	}
	var service string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		service = line
		break
	}
	if service == "" {
		return true, false, ""
	}
	proxyOut, err := exec.Command("networksetup", "-getwebproxy", service).Output()
	if err != nil {
		return false, false, ""
	}
	server, port := "", ""
	for _, line := range strings.Split(string(proxyOut), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enabled":
			enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "server":
			server = strings.TrimSpace(value)
		case "port":
			port = strings.TrimSpace(value)
		}
	}
	if server != "" && port != "" {
		address = server + ":" + port
	}
	return true, enabled, address
}

func platformNotes(_ Facts) []string {
	return []string{"macOS TUN has not been verified on real hardware by this project; treat it as unofficial support"}
}
