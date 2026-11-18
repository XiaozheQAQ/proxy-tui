// Package tun renders core-native TUN configuration, runs cross-platform
// preflight checks and manages the TUN lifecycle through the proxy cores.
//
// ProxyTUI never creates a TUN device, writes routes or runs nftables itself:
// Mihomo and sing-box already implement that. This package only produces their
// configuration, verifies the result and drives start/stop.
package tun

import (
	"fmt"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Stack is the TUN stack implementation used by the core.
type Stack string

// Supported TUN stacks.
const (
	StackMixed  Stack = "mixed"
	StackSystem Stack = "system"
	StackGvisor Stack = "gvisor"
)

// ParseStack accepts the CLI spellings.
func ParseStack(value string) (Stack, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mixed", "":
		return StackMixed, nil
	case "system":
		return StackSystem, nil
	case "gvisor":
		return StackGvisor, nil
	}
	return "", fmt.Errorf("unknown TUN stack %q (want mixed, system or gvisor)", value)
}

// Display renders the stack for the UI.
func (s Stack) Display() string {
	switch s {
	case StackMixed:
		return "mixed"
	case StackSystem:
		return "system"
	case StackGvisor:
		return "gvisor"
	default:
		return string(s)
	}
}

// Settings is the core agnostic TUN configuration.
type Settings struct {
	Stack       Stack  `json:"stack"`
	MTU         int    `json:"mtu"`
	DNSHijack   bool   `json:"dns_hijack"`
	IPv6        bool   `json:"ipv6"`
	Device      string `json:"device,omitempty"`
	AutoRoute   bool   `json:"auto_route"`
	StrictRoute bool   `json:"strict_route"`
}

// Defaults returns the settings used for a first enable.
func Defaults() Settings {
	return Settings{
		Stack:       StackMixed,
		MTU:         9000,
		DNSHijack:   true,
		IPv6:        false,
		AutoRoute:   true,
		StrictRoute: true,
	}
}

// Normalize repairs zero values.
func (s *Settings) Normalize() {
	if s.Stack == "" {
		s.Stack = StackMixed
	}
	if s.MTU <= 0 {
		s.MTU = 9000
	}
	if s.MTU < 1280 || s.MTU > 65535 {
		s.MTU = 9000
	}
}

// Validate checks the configuration before anything is rendered.
func (s Settings) Validate() error {
	switch s.Stack {
	case StackMixed, StackSystem, StackGvisor:
	default:
		return fmt.Errorf("unsupported TUN stack %q", s.Stack)
	}
	if s.MTU != 0 && (s.MTU < 1280 || s.MTU > 65535) {
		return fmt.Errorf("TUN MTU %d is out of range (1280-65535)", s.MTU)
	}
	if strings.ContainsAny(s.Device, " \t\r\n") {
		return fmt.Errorf("TUN device name %q contains whitespace", s.Device)
	}
	return nil
}

// MinimumVersion is the oldest core version ProxyTUI renders TUN for.
func MinimumVersion(kind model.CoreKind) string {
	switch kind {
	case model.CoreMihomo:
		return "1.18.0"
	case model.CoreSingBox:
		return "1.11.0"
	default:
		return ""
	}
}

// VersionAtLeast reports whether version >= minimum (dot separated numeric).
func VersionAtLeast(version, minimum string) bool {
	if minimum == "" {
		return true
	}
	return compareVersions(version, minimum) >= 0
}

func compareVersions(a, b string) int {
	left := versionParts(a)
	right := versionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l < r {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(version string) []int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	trimmed = strings.SplitN(trimmed, "-", 2)[0]
	fields := strings.Split(trimmed, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		number := 0
		for _, r := range field {
			if r < '0' || r > '9' {
				break
			}
			number = number*10 + int(r-'0')
		}
		parts = append(parts, number)
	}
	return parts
}
