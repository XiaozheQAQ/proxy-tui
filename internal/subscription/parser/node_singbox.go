package parser

import (
	"fmt"
	"sort"
	"strings"
)

// ToSingBoxOutbound converts a node into a sing-box outbound object.
func (n Node) ToSingBoxOutbound() (map[string]any, error) {
	if n.Server == "" || n.Port <= 0 {
		return nil, fmt.Errorf("node %q has no server/port", n.Name)
	}
	out := map[string]any{
		"tag":         n.Name,
		"server":      n.Server,
		"server_port": n.Port,
	}

	switch n.Protocol {
	case "ss":
		out["type"] = "shadowsocks"
		out["method"] = n.Method
		out["password"] = n.Password
		if n.Plugin != "" {
			out["plugin"] = n.Plugin
			if opts := singBoxPluginOpts(n.PluginOpts); opts != "" {
				out["plugin_opts"] = opts
			}
		}
	case "vmess":
		out["type"] = "vmess"
		out["uuid"] = n.UUID
		out["security"] = "auto"
		if n.AlterID > 0 {
			out["alter_id"] = n.AlterID
		}
		applySingBoxTransport(out, n)
		if n.TLS {
			applySingBoxTLS(out, n)
		}
	case "vless":
		out["type"] = "vless"
		out["uuid"] = n.UUID
		if n.Flow != "" {
			out["flow"] = n.Flow
		}
		applySingBoxTransport(out, n)
		if n.TLS {
			applySingBoxTLS(out, n)
		}
	case "trojan":
		out["type"] = "trojan"
		out["password"] = n.Password
		applySingBoxTransport(out, n)
		applySingBoxTLS(out, n)
	case "hysteria2":
		out["type"] = "hysteria2"
		out["password"] = n.Password
		if n.Obfs != "" {
			obfs := map[string]any{"type": n.Obfs}
			if n.ObfsPassword != "" {
				obfs["password"] = n.ObfsPassword
			}
			out["obfs"] = obfs
		}
		if n.UpMbps > 0 {
			out["up_mbps"] = n.UpMbps
		}
		if n.DownMbps > 0 {
			out["down_mbps"] = n.DownMbps
		}
		applySingBoxTLS(out, n)
	case "tuic":
		out["type"] = "tuic"
		out["uuid"] = n.UUID
		out["password"] = n.Password
		out["congestion_control"] = "bbr"
		if mode, ok := n.Extra["udp_relay_mode"].(string); ok && mode != "" {
			out["udp_relay_mode"] = mode
		}
		applySingBoxTLS(out, n)
	case "socks5":
		out["type"] = "socks"
		if n.Username != "" {
			out["username"] = n.Username
		}
		if n.Password != "" {
			out["password"] = n.Password
		}
		if n.TLS {
			applySingBoxTLS(out, n)
		}
	case "http":
		out["type"] = "http"
		if n.Username != "" {
			out["username"] = n.Username
		}
		if n.Password != "" {
			out["password"] = n.Password
		}
		if n.TLS {
			applySingBoxTLS(out, n)
		}
	default:
		return nil, fmt.Errorf("protocol %q cannot be written as a sing-box outbound", n.Protocol)
	}
	return out, nil
}

func applySingBoxTLS(out map[string]any, n Node) {
	tls := map[string]any{"enabled": true}
	switch {
	case n.SNI != "":
		tls["server_name"] = n.SNI
	case n.Host != "":
		tls["server_name"] = n.Host
	}
	if n.Insecure {
		tls["insecure"] = true
	}
	if len(n.ALPN) > 0 {
		tls["alpn"] = n.ALPN
	}
	if n.Fingerprint != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": n.Fingerprint}
	}
	if n.RealityPublicKey != "" {
		reality := map[string]any{"enabled": true, "public_key": n.RealityPublicKey}
		if n.RealityShortID != "" {
			reality["short_id"] = n.RealityShortID
		}
		tls["reality"] = reality
	}
	out["tls"] = tls
}

func applySingBoxTransport(out map[string]any, n Node) {
	switch n.Network {
	case "ws":
		transport := map[string]any{"type": "ws"}
		if n.Path != "" {
			transport["path"] = n.Path
		}
		if n.Host != "" {
			transport["headers"] = map[string]any{"Host": n.Host}
		}
		out["transport"] = transport
	case "grpc":
		transport := map[string]any{"type": "grpc"}
		if n.ServiceName != "" {
			transport["service_name"] = n.ServiceName
		}
		out["transport"] = transport
	case "h2":
		transport := map[string]any{"type": "http"}
		if n.Path != "" {
			transport["path"] = n.Path
		}
		if n.Host != "" {
			transport["host"] = []string{n.Host}
		}
		out["transport"] = transport
	}
}

func singBoxPluginOpts(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+opts[k])
	}
	return strings.Join(parts, ";")
}
