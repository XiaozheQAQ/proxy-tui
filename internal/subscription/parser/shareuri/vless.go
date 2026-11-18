package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

func parseVLESS(raw string) (parser.Node, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parser.Node{}, fmt.Errorf("vless: %w", err)
	}
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return parser.Node{}, fmt.Errorf("vless: missing uuid")
	}
	q := u.Query()
	security := strings.ToLower(firstQuery(q, "security"))

	node := parser.Node{
		Protocol:         "vless",
		Name:             u.Fragment,
		Server:           u.Hostname(),
		Port:             parsePort(u.Port()),
		UUID:             uuid,
		TLS:              security == "tls" || security == "reality" || security == "xtls",
		SNI:              firstQuery(q, "sni", "serverName"),
		Flow:             firstQuery(q, "flow"),
		Network:          strings.ToLower(firstQuery(q, "type", "network")),
		Path:             pct(firstQuery(q, "path")),
		Host:             firstQuery(q, "host"),
		ServiceName:      firstQuery(q, "serviceName", "service_name"),
		RealityPublicKey: firstQuery(q, "pbk", "publicKey"),
		RealityShortID:   firstQuery(q, "sid", "shortId"),
		Fingerprint:      firstQuery(q, "fp", "fingerprint"),
		Insecure:         anyBoolQuery(q, "allowInsecure", "insecure", "skip-cert-verify"),
		ALPN:             csv(firstQuery(q, "alpn")),
	}
	if node.Port <= 0 {
		return node, fmt.Errorf("vless: invalid port %q", u.Port())
	}
	if node.Server == "" {
		return node, fmt.Errorf("vless: missing server")
	}
	if node.Network == "" {
		node.Network = "tcp"
	}
	if node.Name == "" {
		if node.SNI != "" {
			node.Name = node.SNI
		} else {
			node.Name = fmt.Sprintf("vless-%s:%d", node.Server, node.Port)
		}
	}
	return node, nil
}
