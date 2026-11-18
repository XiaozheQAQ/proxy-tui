package parser

import "fmt"

// ToMihomo converts a node into a Mihomo proxy mapping.
func (n Node) ToMihomo() (map[string]any, error) {
	if n.Server == "" || n.Port <= 0 {
		return nil, fmt.Errorf("node %q has no server/port", n.Name)
	}
	m := map[string]any{
		"name":   n.Name,
		"server": n.Server,
		"port":   n.Port,
	}

	switch n.Protocol {
	case "ss":
		m["type"] = "ss"
		m["cipher"] = n.Method
		m["password"] = n.Password
		if n.Plugin != "" {
			m["plugin"] = n.Plugin
			if len(n.PluginOpts) > 0 {
				m["plugin-opts"] = n.PluginOpts
			}
		}
	case "vmess":
		m["type"] = "vmess"
		m["uuid"] = n.UUID
		m["alterId"] = n.AlterID
		m["cipher"] = "auto"
		if n.TLS {
			m["tls"] = true
			applyMihomoTLS(m, n, "servername")
		}
		applyMihomoTransport(m, n)
	case "vless":
		m["type"] = "vless"
		m["uuid"] = n.UUID
		if n.Flow != "" {
			m["flow"] = n.Flow
		}
		if n.TLS {
			m["tls"] = true
			applyMihomoTLS(m, n, "servername")
		}
		applyMihomoTransport(m, n)
		if n.RealityPublicKey != "" {
			reality := map[string]any{"public-key": n.RealityPublicKey}
			if n.RealityShortID != "" {
				reality["short-id"] = n.RealityShortID
			}
			m["reality-opts"] = reality
		}
	case "trojan":
		m["type"] = "trojan"
		m["password"] = n.Password
		if n.TLS || n.SNI != "" {
			m["tls"] = true
			applyMihomoTLS(m, n, "sni")
		}
		applyMihomoTransport(m, n)
	case "hysteria2":
		m["type"] = "hysteria2"
		m["password"] = n.Password
		if n.SNI != "" {
			m["sni"] = n.SNI
		}
		if n.Insecure {
			m["skip-cert-verify"] = true
		}
		if n.Obfs != "" {
			m["obfs"] = n.Obfs
			if n.ObfsPassword != "" {
				m["obfs-password"] = n.ObfsPassword
			}
		}
		if n.UpMbps > 0 {
			m["up"] = fmt.Sprintf("%d Mbps", n.UpMbps)
		}
		if n.DownMbps > 0 {
			m["down"] = fmt.Sprintf("%d Mbps", n.DownMbps)
		}
	case "tuic":
		m["type"] = "tuic"
		m["uuid"] = n.UUID
		m["password"] = n.Password
		if n.SNI != "" {
			m["sni"] = n.SNI
		}
		if len(n.ALPN) > 0 {
			m["alpn"] = n.ALPN
		}
		if n.Insecure {
			m["skip-cert-verify"] = true
		}
		if mode, ok := n.Extra["udp_relay_mode"].(string); ok && mode != "" {
			m["udp-relay-mode"] = mode
		}
	case "socks5":
		m["type"] = "socks5"
		if n.Username != "" {
			m["username"] = n.Username
		}
		if n.Password != "" {
			m["password"] = n.Password
		}
		if n.TLS {
			m["tls"] = true
			if n.SNI != "" {
				m["servername"] = n.SNI
			}
			if n.Insecure {
				m["skip-cert-verify"] = true
			}
		}
	case "http":
		m["type"] = "http"
		if n.Username != "" {
			m["username"] = n.Username
		}
		if n.Password != "" {
			m["password"] = n.Password
		}
		if n.TLS {
			m["tls"] = true
			if n.SNI != "" {
				m["sni"] = n.SNI
			}
			if n.Insecure {
				m["skip-cert-verify"] = true
			}
		}
	default:
		return nil, fmt.Errorf("protocol %q cannot be written as a Mihomo proxy", n.Protocol)
	}
	return m, nil
}

func applyMihomoTLS(m map[string]any, n Node, sniKey string) {
	if n.SNI != "" {
		m[sniKey] = n.SNI
	} else if n.Host != "" && sniKey == "servername" {
		m[sniKey] = n.Host
	}
	if len(n.ALPN) > 0 {
		m["alpn"] = n.ALPN
	}
	if n.Insecure {
		m["skip-cert-verify"] = true
	}
	if n.Fingerprint != "" {
		m["client-fingerprint"] = n.Fingerprint
	}
}

func applyMihomoTransport(m map[string]any, n Node) {
	switch n.Network {
	case "", "tcp":
		return
	case "ws":
		m["network"] = "ws"
		opts := map[string]any{}
		if n.Path != "" {
			opts["path"] = n.Path
		}
		if n.Host != "" {
			opts["headers"] = map[string]any{"Host": n.Host}
		}
		if len(opts) > 0 {
			m["ws-opts"] = opts
		}
	case "grpc":
		m["network"] = "grpc"
		opts := map[string]any{}
		if n.ServiceName != "" {
			opts["grpc-service-name"] = n.ServiceName
		}
		if len(opts) > 0 {
			m["grpc-opts"] = opts
		}
	case "h2":
		m["network"] = "h2"
		opts := map[string]any{}
		if n.Path != "" {
			opts["path"] = n.Path
		}
		if n.Host != "" {
			opts["host"] = []string{n.Host}
		}
		if len(opts) > 0 {
			m["h2-opts"] = opts
		}
	default:
		m["network"] = n.Network
	}
}
