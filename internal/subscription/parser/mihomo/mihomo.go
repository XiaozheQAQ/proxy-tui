// Package mihomo parses Clash / Mihomo YAML subscriptions.
package mihomo

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// Parse reads a Mihomo YAML document and extracts proxy nodes.
func Parse(raw []byte) (*parser.Result, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("mihomo: invalid YAML: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("mihomo: empty YAML document")
	}

	proxies := sliceVal(doc["proxies"])
	providers := mapVal(doc["proxy-providers"])
	groups := sliceVal(doc["proxy-groups"])
	rules := sliceVal(doc["rules"])

	if len(proxies) == 0 && len(providers) == 0 && len(groups) == 0 && len(rules) == 0 {
		return nil, fmt.Errorf("mihomo: payload is YAML but not a Clash/Mihomo configuration (no proxies, proxy-providers, proxy-groups or rules)")
	}

	res := &parser.Result{
		Core:       model.CoreMihomo,
		Format:     model.FormatMihomoYAML,
		Encoding:   model.EncodingPlain,
		Raw:        raw,
		HasRouting: len(rules) > 0 || len(groups) > 0 || doc["dns"] != nil,
		Groups:     len(groups),
		Rules:      len(rules),
	}

	for i, entry := range proxies {
		m := mapVal(entry)
		if len(m) == 0 {
			res.Unsupported = append(res.Unsupported, parser.Unsupported{
				Scheme: "yaml",
				Reason: "proxy entry is not a mapping",
				Sample: fmt.Sprintf("proxies[%d]", i),
			})
			continue
		}
		node, err := NodeFromProxy(m)
		if err != nil {
			res.Unsupported = append(res.Unsupported, parser.Unsupported{
				Scheme: strVal(m["type"]),
				Name:   strVal(m["name"]),
				Reason: err.Error(),
				Sample: fmt.Sprintf("proxies[%d]", i),
			})
			continue
		}
		if node.Name == "" {
			node.Name = fmt.Sprintf("proxy-%d", i+1)
		}
		res.Nodes = append(res.Nodes, node)
	}

	if len(providers) > 0 {
		res.AddWarning("%d proxy-provider(s) are preserved verbatim; the core resolves them at runtime", len(providers))
	}
	if len(res.Nodes) == 0 && len(providers) == 0 {
		if len(res.Unsupported) > 0 {
			return nil, fmt.Errorf("mihomo: no usable proxies (%s)", strings.Join(res.UnsupportedSummaries(5), "; "))
		}
		return nil, fmt.Errorf("mihomo: configuration contains no usable proxies")
	}
	parser.EnsureUniqueNames(res.Nodes)
	return res, nil
}

// NodeFromProxy converts one Mihomo proxy mapping into a normalised node.
func NodeFromProxy(m map[string]any) (parser.Node, error) {
	proxyType := strings.ToLower(strVal(m["type"]))
	node := parser.Node{
		Protocol:    proxyType,
		Name:        strVal(m["name"]),
		Server:      strVal(m["server"]),
		Port:        intVal(m["port"]),
		TLS:         boolVal(m["tls"]),
		SNI:         firstNonEmpty(strVal(m["servername"]), strVal(m["sni"]), strVal(m["server-name"])),
		Insecure:    boolVal(m["skip-cert-verify"]),
		Fingerprint: strVal(m["client-fingerprint"]),
		Network:     strings.ToLower(strVal(m["network"])),
		ALPN:        stringSlice(m["alpn"]),
		Extra:       map[string]any{},
	}
	if node.Port <= 0 || node.Server == "" {
		return node, fmt.Errorf("missing server/port")
	}
	if node.Network == "" {
		node.Network = "tcp"
	}
	applyMihomoTransportFields(&node, m)
	applyMihomoTLSFields(&node, m)

	switch proxyType {
	case "ss", "shadowsocks":
		node.Protocol = "ss"
		node.Method = firstNonEmpty(strVal(m["cipher"]), strVal(m["method"]))
		node.Password = strVal(m["password"])
		node.Plugin = strVal(m["plugin"])
		node.PluginOpts = stringMap(m["plugin-opts"])
		if node.Method == "" {
			return node, fmt.Errorf("ss proxy without cipher")
		}
	case "vmess":
		node.UUID = strVal(m["uuid"])
		node.AlterID = intVal(m["alterId"])
		if node.UUID == "" {
			return node, fmt.Errorf("vmess proxy without uuid")
		}
	case "vless":
		node.UUID = strVal(m["uuid"])
		node.Flow = strVal(m["flow"])
		if node.UUID == "" {
			return node, fmt.Errorf("vless proxy without uuid")
		}
		if reality := mapVal(m["reality-opts"]); len(reality) > 0 {
			node.RealityPublicKey = strVal(reality["public-key"])
			node.RealityShortID = strVal(reality["short-id"])
			node.TLS = true
		}
	case "trojan":
		node.Password = strVal(m["password"])
		node.TLS = true
	case "hysteria2", "hy2":
		node.Protocol = "hysteria2"
		node.Password = strVal(m["password"])
		node.Obfs = strVal(m["obfs"])
		node.ObfsPassword = strVal(m["obfs-password"])
		node.UpMbps = mbpsVal(m["up"])
		node.DownMbps = mbpsVal(m["down"])
		node.TLS = true
	case "tuic":
		node.UUID = strVal(m["uuid"])
		node.Password = strVal(m["password"])
		node.TLS = true
		if mode := strVal(m["udp-relay-mode"]); mode != "" {
			node.Extra["udp_relay_mode"] = mode
		}
		if cc := strVal(m["congestion-controller"]); cc != "" {
			node.Extra["congestion_control"] = cc
		}
	case "socks5", "socks":
		node.Protocol = "socks5"
		node.Username = strVal(m["username"])
		node.Password = strVal(m["password"])
	case "http", "https":
		node.Protocol = "http"
		node.Username = strVal(m["username"])
		node.Password = strVal(m["password"])
	default:
		return node, fmt.Errorf("proxy type %q is not supported by proxy-tui", proxyType)
	}
	return node, nil
}

func applyMihomoTransportFields(node *parser.Node, m map[string]any) {
	switch node.Network {
	case "ws":
		opts := mapVal(m["ws-opts"])
		node.Path = strVal(opts["path"])
		if headers := mapVal(opts["headers"]); len(headers) > 0 {
			node.Host = strVal(headers["Host"])
		}
	case "grpc":
		opts := mapVal(m["grpc-opts"])
		node.ServiceName = strVal(opts["grpc-service-name"])
	case "h2":
		opts := mapVal(m["h2-opts"])
		node.Path = strVal(opts["path"])
		if hosts := stringSlice(opts["host"]); len(hosts) > 0 {
			node.Host = hosts[0]
		}
	}
}

func applyMihomoTLSFields(node *parser.Node, m map[string]any) {
	if node.SNI == "" && node.Host != "" && node.Network != "ws" {
		node.SNI = node.Host
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func strVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func boolVal(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case int:
		return t != 0
	}
	return false
}

func intVal(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n
		}
	}
	return 0
}

func mapVal(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func sliceVal(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func stringSlice(v any) []string {
	items := sliceVal(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(strVal(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringMap(v any) map[string]string {
	m := mapVal(v)
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, value := range m {
		out[k] = strVal(value)
	}
	return out
}

func mbpsVal(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(t), "mbps"))
		if n, err := strconv.Atoi(trimmed); err == nil {
			return n
		}
	}
	return 0
}
