// Package shareuri parses proxy share links (ss://, vmess://, vless:// ...)
// into protocol-neutral nodes.
package shareuri

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// ErrNotShareLink reports that a line is not a proxy share link.
var ErrNotShareLink = errors.New("not a share link")

// ParseLine converts one share link into a node.
func ParseLine(line string) (parser.Node, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return parser.Node{}, ErrNotShareLink
	}
	scheme, _, ok := splitScheme(trimmed)
	if !ok {
		return parser.Node{}, ErrNotShareLink
	}
	switch scheme {
	case "ss":
		return parseSS(strings.TrimSpace(trimmed[len("ss://"):]))
	case "vmess":
		return parseVMess(strings.TrimSpace(trimmed[len("vmess://"):]))
	case "vless":
		return parseVLESS(trimmed)
	case "trojan":
		return parseTrojan(trimmed)
	case "hysteria2", "hy2":
		return parseHysteria2(trimmed)
	case "tuic":
		return parseTUIC(trimmed)
	case "socks", "socks5":
		return parseSocks(trimmed)
	case "http", "https":
		return parseHTTPProxy(trimmed)
	default:
		return parser.Node{}, fmt.Errorf("unsupported scheme %q", scheme)
	}
}

// ParseLines parses every recognised share link in a newline separated blob.
func ParseLines(text string) ([]parser.Node, []parser.Unsupported, error) {
	var nodes []parser.Node
	var unsupported []parser.Unsupported
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !LooksLikeNodeURI(trimmed) {
			unsupported = append(unsupported, parser.Unsupported{
				Scheme: schemeName(trimmed),
				Reason: "unrecognised line",
				Sample: Sample(trimmed),
			})
			continue
		}
		node, err := ParseLine(trimmed)
		if err != nil {
			unsupported = append(unsupported, parser.Unsupported{
				Scheme: schemeName(trimmed),
				Reason: err.Error(),
				Sample: Sample(trimmed),
			})
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		if len(unsupported) > 0 {
			return nil, unsupported, fmt.Errorf("no supported nodes found (%d lines rejected)", len(unsupported))
		}
		return nil, nil, fmt.Errorf("no proxy links found")
	}
	parser.EnsureUniqueNames(nodes)
	return nodes, unsupported, nil
}

// LooksLikeNodeURI reports whether the line is a supported proxy share link.
// Plain http(s) links are only treated as proxies when they carry credentials
// or point at a bare IP:port, so subscription URLs stay distinguishable.
func LooksLikeNodeURI(line string) bool {
	trimmed := strings.TrimSpace(line)
	scheme, _, ok := splitScheme(trimmed)
	if !ok {
		return false
	}
	switch scheme {
	case "ss", "vmess", "vless", "trojan", "hysteria2", "hy2", "tuic", "socks", "socks5":
		return true
	case "http", "https":
		return looksLikeHTTPProxy(trimmed)
	}
	return false
}

// Sample returns a credential-free preview of a share link.
func Sample(raw string) string {
	scheme, _, ok := splitScheme(raw)
	if !ok {
		return "(unparsed)"
	}
	return scheme + "://..."
}

func looksLikeHTTPProxy(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.User != nil {
		return true
	}
	if u.Path != "" && u.Path != "/" {
		return false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	// A bare IP:port with no path is far more likely a proxy than a
	// subscription endpoint.
	return netParseIP(host)
}

func netParseIP(host string) bool {
	if host == "" {
		return false
	}
	allDigits := true
	for _, r := range host {
		if r != '.' {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
	}
	return allDigits && strings.Count(host, ".") == 3
}

func splitScheme(raw string) (string, string, bool) {
	i := strings.Index(raw, "://")
	if i <= 0 {
		return "", "", false
	}
	return strings.ToLower(raw[:i]), raw[i+3:], true
}

func schemeName(raw string) string {
	scheme, _, ok := splitScheme(raw)
	if !ok {
		return "unknown"
	}
	return scheme
}

// splitFragment separates "#name" and percent-decodes it.
func splitFragment(rest string) (string, string) {
	if i := strings.Index(rest, "#"); i >= 0 {
		name := rest[i+1:]
		if decoded, err := url.QueryUnescape(name); err == nil {
			name = decoded
		}
		return rest[:i], name
	}
	return rest, ""
}

func splitQuery(rest string) (string, string) {
	if i := strings.Index(rest, "?"); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}

func decodeBase64Loose(s string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)
	if cleaned == "" {
		return "", fmt.Errorf("empty base64 payload")
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(cleaned)
		if err == nil {
			return string(decoded), nil
		}
		lastErr = err
	}
	return "", lastErr
}

func splitHostPort(hostport string) (string, int, error) {
	hostport = strings.TrimSpace(strings.TrimSuffix(hostport, "/"))
	if hostport == "" {
		return "", 0, fmt.Errorf("missing server address")
	}
	if strings.HasPrefix(hostport, "[") {
		end := strings.Index(hostport, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("malformed IPv6 address %q", hostport)
		}
		host := hostport[1:end]
		rest := strings.TrimPrefix(hostport[end+1:], ":")
		port, err := strconv.Atoi(rest)
		if err != nil || port <= 0 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port in %q", hostport)
		}
		return host, port, nil
	}
	idx := strings.LastIndex(hostport, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("missing port in %q", hostport)
	}
	host := hostport[:idx]
	port, err := strconv.Atoi(hostport[idx+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port in %q", hostport)
	}
	if host == "" {
		return "", 0, fmt.Errorf("missing host in %q", hostport)
	}
	return host, port, nil
}

func pct(s string) string {
	if s == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

func parseBoolLoose(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func csv(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstQuery(q url.Values, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func anyBoolQuery(q url.Values, keys ...string) bool {
	for _, key := range keys {
		if parseBoolLoose(q.Get(key)) {
			return true
		}
	}
	return false
}

func rawInt(raw json.RawMessage, def int) int {
	if len(raw) == 0 {
		return def
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if n, err := number.Int64(); err == nil {
			return int(n)
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
			return n
		}
	}
	return def
}

func rawStringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return csv(single)
	}
	return nil
}
