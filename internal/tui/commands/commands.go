// Package commands defines the slash commands available in the TUI palette.
package commands

import "strings"

// Command is one palette entry.
type Command struct {
	Name  string
	Usage string
	Help  string
}

// All lists every command.
func All() []Command {
	return []Command{
		{Name: "/help", Usage: "/help", Help: "show the keyboard reference"},
		{Name: "/core", Usage: "/core mihomo|sing-box", Help: "switch the active core page"},
		{Name: "/profile", Usage: "/profile add|list|use|update", Help: "manage subscriptions"},
		{Name: "/proxy", Usage: "/proxy select|test", Help: "switch or test proxy nodes"},
		{Name: "/system-proxy", Usage: "/system-proxy enable|disable", Help: "toggle the system proxy (planned)"},
		{Name: "/tun", Usage: "/tun enable|disable", Help: "toggle TUN mode (planned)"},
		{Name: "/refresh", Usage: "/refresh", Help: "reload status and cores"},
		{Name: "/logs", Usage: "/logs", Help: "open the logs page"},
		{Name: "/doctor", Usage: "/doctor", Help: "run local diagnostics"},
		{Name: "/quit", Usage: "/quit", Help: "leave the TUI"},
	}
}

// Match filters commands by typed prefix, tolerant of a missing leading slash.
func Match(input string) []Command {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return All()
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	fields := strings.Fields(trimmed)
	prefix := ""
	if len(fields) > 0 {
		prefix = fields[0]
	}
	var out []Command
	for _, cmd := range All() {
		name := strings.TrimPrefix(cmd.Name, "/")
		if prefix == "" || strings.HasPrefix(name, prefix) {
			out = append(out, cmd)
		}
	}
	if len(out) == 0 {
		for _, cmd := range All() {
			if strings.Contains(cmd.Help, trimmed) {
				out = append(out, cmd)
			}
		}
	}
	return out
}
