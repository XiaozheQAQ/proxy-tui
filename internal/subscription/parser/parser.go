// Package parser normalises subscription payloads into proxy nodes.
package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Node is a protocol-neutral proxy description. It can be rendered into a
// Mihomo proxy entry or a sing-box outbound.
type Node struct {
	Name     string
	Protocol string
	Server   string
	Port     int

	Username string
	Password string
	UUID     string
	Method   string
	AlterID  int

	TLS         bool
	SNI         string
	ALPN        []string
	Fingerprint string
	Insecure    bool

	Network     string // tcp, ws, grpc, h2
	Path        string
	Host        string
	ServiceName string
	Flow        string

	RealityPublicKey string
	RealityShortID   string

	Obfs         string
	ObfsPassword string
	UpMbps       int
	DownMbps     int

	Plugin     string
	PluginOpts map[string]string

	Extra map[string]any
	Raw   string
}

// Unsupported records a payload element that could not be imported.
type Unsupported struct {
	Scheme string `json:"scheme,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
	Sample string `json:"sample,omitempty"`
}

// Result is the outcome of parsing one subscription payload.
type Result struct {
	Core        model.CoreKind
	Format      model.Format
	Encoding    model.Encoding
	Nodes       []Node
	Raw         []byte
	HasRouting  bool
	Warnings    []string
	Unsupported []Unsupported
	Groups      int
	Rules       int
}

// AddWarning appends a deduplicated warning.
func (r *Result) AddWarning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, existing := range r.Warnings {
		if existing == msg {
			return
		}
	}
	r.Warnings = append(r.Warnings, msg)
}

// UnsupportedSummaries renders the unsupported entries as short strings.
func (r *Result) UnsupportedSummaries(limit int) []string {
	var out []string
	for i, u := range r.Unsupported {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, fmt.Sprintf("%s: %s", u.Scheme, u.Reason))
	}
	return out
}

// Stats counts nodes per protocol.
func (r *Result) Stats() map[string]int {
	stats := make(map[string]int, len(r.Nodes))
	for _, n := range r.Nodes {
		stats[n.Protocol]++
	}
	return stats
}

// StatsString renders the protocol histogram for the UI.
func (r *Result) StatsString() string {
	stats := r.Stats()
	if len(stats) == 0 {
		return "no nodes"
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, stats[k]))
	}
	return strings.Join(parts, " ")
}

// EnsureUniqueNames disambiguates duplicated node names.
func EnsureUniqueNames(nodes []Node) {
	seen := map[string]int{}
	for i := range nodes {
		name := strings.TrimSpace(nodes[i].Name)
		if name == "" {
			name = fmt.Sprintf("%s-%d", nodes[i].Protocol, i+1)
		}
		if count, dup := seen[name]; dup {
			seen[name] = count + 1
			name = fmt.Sprintf("%s (%d)", name, count+1)
		} else {
			seen[name] = 1
		}
		nodes[i].Name = name
	}
}

// SanitizeName trims control characters from a user supplied name.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
			continue
		}
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}
