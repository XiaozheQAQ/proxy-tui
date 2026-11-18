package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

func parseTUIC(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("tuic: %w", err)
	}
	uuid, password := "", ""
	if u.User != nil {
		uuid = u.User.Username()
		if secret, ok := u.User.Password(); ok {
			password = secret
		}
	}
	if uuid == "" {
		return parser.Node{}, fmt.Errorf("tuic: missing uuid")
	}
	q := u.Query()
	node := parser.Node{
		Protocol: "tuic",
		Name:     u.Fragment,
		Server:   u.Hostname(),
		Port:     parsePort(u.Port()),
		UUID:     uuid,
		Password: password,
		TLS:      true,
		SNI:      firstQuery(q, "sni", "peer"),
		Insecure: anyBoolQuery(q, "allow_insecure", "allowInsecure", "insecure"),
		ALPN:     csv(firstQuery(q, "alpn")),
		Extra:    map[string]any{},
	}
	if mode := firstQuery(q, "udp_relay_mode", "udp-relay-mode"); mode != "" {
		node.Extra["udp_relay_mode"] = mode
	}
	if cc := firstQuery(q, "congestion_control", "congestion-controller"); cc != "" {
		node.Extra["congestion_control"] = cc
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("tuic: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("tuic: missing server")
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("tuic-%s:%d", node.Server, node.Port)
	}
	return node, nil
}
