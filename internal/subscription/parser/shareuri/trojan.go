package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

func parseTrojan(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("trojan: %w", err)
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return parser.Node{}, fmt.Errorf("trojan: missing password")
	}
	if decrypted, err := url.QueryUnescape(password); err == nil {
		password = decrypted
	}
	q := u.Query()
	node := parser.Node{
		Protocol:    "trojan",
		Name:        u.Fragment,
		Server:      u.Hostname(),
		Port:        parsePort(u.Port()),
		Password:    password,
		TLS:         true,
		SNI:         firstQuery(q, "sni", "peer", "serverName"),
		Network:     strings.ToLower(firstQuery(q, "type", "network")),
		Path:        pct(firstQuery(q, "path")),
		Host:        firstQuery(q, "host"),
		ServiceName: firstQuery(q, "serviceName", "service_name"),
		Fingerprint: firstQuery(q, "fp", "fingerprint"),
		Insecure:    anyBoolQuery(q, "allowInsecure", "insecure", "skip-cert-verify"),
		ALPN:        csv(firstQuery(q, "alpn")),
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("trojan: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("trojan: missing server")
	}
	if node.Network == "" {
		node.Network = "tcp"
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("trojan-%s:%d", node.Server, node.Port)
	}
	return node, nil
}
