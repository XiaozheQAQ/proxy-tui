//go:build !windows

package tun

import (
	"os"
	"strings"
)

// readResolvConf parses /etc/resolv.conf for nameservers.
func readResolvConf() ([]string, string, bool) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, "resolv.conf", false
	}
	seen := map[string]bool{}
	var servers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		server := fields[1]
		if !seen[server] {
			seen[server] = true
			servers = append(servers, server)
		}
	}
	if len(servers) == 0 {
		return nil, "resolv.conf", false
	}
	return servers, "resolv.conf", true
}
