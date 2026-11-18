package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

func parseSocks(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("socks: %w", err)
	}
	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		if secret, ok := u.User.Password(); ok {
			password = secret
		}
		// Some clients emit socks://base64(user:pass)@host:port.
		if password == "" {
			if decoded, err := decodeBase64Loose(username); err == nil {
				if name, secret, ok := strings.Cut(decoded, ":"); ok {
					username, password = name, secret
				}
			}
		}
	}
	node := parser.Node{
		Protocol: "socks5",
		Name:     u.Fragment,
		Server:   u.Hostname(),
		Port:     parsePort(u.Port()),
		Username: pct(username),
		Password: pct(password),
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("socks: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("socks: missing server")
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("socks-%s:%d", node.Server, node.Port)
	}
	return node, nil
}
