package shareuri

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

func parseHysteria2(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("hysteria2: %w", err)
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
		if secret, ok := u.User.Password(); ok && secret != "" {
			password = password + ":" + secret
		}
	}
	if password == "" {
		return parser.Node{}, fmt.Errorf("hysteria2: missing password")
	}
	if decoded, err := url.QueryUnescape(password); err == nil {
		password = decoded
	}
	q := u.Query()
	node := parser.Node{
		Protocol:     "hysteria2",
		Name:         u.Fragment,
		Server:       u.Hostname(),
		Port:         parsePort(u.Port()),
		Password:     password,
		TLS:          true,
		SNI:          firstQuery(q, "sni", "peer"),
		Obfs:         firstQuery(q, "obfs"),
		ObfsPassword: firstQuery(q, "obfs-password", "obfsPassword"),
		Insecure:     anyBoolQuery(q, "insecure", "allowInsecure", "skip-cert-verify"),
		UpMbps:       parseMbps(firstQuery(q, "up", "upmbps")),
		DownMbps:     parseMbps(firstQuery(q, "down", "downmbps")),
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("hysteria2: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("hysteria2: missing server")
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("hy2-%s:%d", node.Server, node.Port)
	}
	return node, nil
}

func parseMbps(value string) int {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(value), "mbps"))
	if trimmed == "" {
		return 0
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
