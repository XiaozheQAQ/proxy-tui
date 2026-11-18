package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// parseHTTPProxy handles http(s):// links that carry proxy credentials.
func parseHTTPProxy(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("http: %w", err)
	}
	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		if secret, ok := u.User.Password(); ok {
			password = secret
		}
	}
	node := parser.Node{
		Protocol: "http",
		Name:     u.Fragment,
		Server:   u.Hostname(),
		Port:     parsePort(u.Port()),
		Username: pct(username),
		Password: pct(password),
		TLS:      strings.EqualFold(u.Scheme, "https"),
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("http: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("http: missing server")
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("http-%s:%d", node.Server, node.Port)
	}
	return node, nil
}
