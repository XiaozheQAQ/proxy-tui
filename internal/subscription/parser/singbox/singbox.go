// Package singbox parses sing-box JSON subscriptions.
package singbox

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// Parse reads a sing-box JSON document and extracts outbound proxies.
func Parse(raw []byte) (*parser.Result, error) {
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("sing-box: invalid JSON: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("sing-box: empty JSON document")
	}
	outbounds := sliceVal(doc["outbounds"])
	if len(outbounds) == 0 && doc["inbounds"] == nil && doc["route"] == nil && doc["dns"] == nil {
		return nil, fmt.Errorf("sing-box: JSON does not look like a sing-box configuration (no outbounds, inbounds, dns or route)")
	}

	res := &parser.Result{
		Core:       model.CoreSingBox,
		Format:     model.FormatSingBoxJSON,
		Encoding:   model.EncodingPlain,
		Raw:        raw,
		HasRouting: doc["route"] != nil || doc["dns"] != nil,
	}
	groups := 0
	for i, entry := range outbounds {
		m := mapVal(entry)
		if len(m) == 0 {
			continue
		}
		outboundType := strings.ToLower(strVal(m["type"]))
		switch outboundType {
		case "selector", "urltest", "direct", "block", "dns":
			groups++
			continue
		}
		node, err := NodeFromOutbound(m)
		if err != nil {
			res.Unsupported = append(res.Unsupported, parser.Unsupported{
				Scheme: outboundType,
				Name:   strVal(m["tag"]),
				Reason: err.Error(),
				Sample: fmt.Sprintf("outbounds[%d]", i),
			})
			continue
		}
		res.Nodes = append(res.Nodes, node)
	}
	res.Groups = groups
	if routes, ok := doc["route"].(map[string]any); ok {
		res.Rules = len(sliceVal(routes["rules"]))
	}
	if len(res.Nodes) == 0 {
		if len(res.Unsupported) > 0 {
			return nil, fmt.Errorf("sing-box: no usable outbounds (%s)", strings.Join(res.UnsupportedSummaries(5), "; "))
		}
		return nil, fmt.Errorf("sing-box: configuration contains no usable outbounds")
	}
	parser.EnsureUniqueNames(res.Nodes)
	return res, nil
}

// NodeFromOutbound converts one sing-box outbound into a normalised node.
func NodeFromOutbound(m map[string]any) (parser.Node, error) {
	outboundType := strings.ToLower(strVal(m["type"]))
	node := parser.Node{
		Protocol: outboundType,
		Name:     strVal(m["tag"]),
		Server:   strVal(m["server"]),
		Port:     intVal(m["server_port"]),
		Extra:    map[string]any{},
	}
	if node.Server == "" || node.Port <= 0 {
		return node, fmt.Errorf("missing server/server_port")
	}
	tls := mapVal(m["tls"])
	if boolVal(tls["enabled"]) {
		node.TLS = true
		node.SNI = strVal(tls["server_name"])
		node.Insecure = boolVal(tls["insecure"])
		node.ALPN = stringSlice(tls["alpn"])
		if utls := mapVal(tls["utls"]); len(utls) > 0 {
			node.Fingerprint = strVal(utls["fingerprint"])
		}
		if reality := mapVal(tls["reality"]); len(reality) > 0 {
			node.RealityPublicKey = strVal(reality["public_key"])
			node.RealityShortID = strVal(reality["short_id"])
			node.TLS = true
		}
	}
	applyTransport(&node, mapVal(m["transport"]))

	switch outboundType {
	case "shadowsocks":
		node.Protocol = "ss"
		node.Method = strVal(m["method"])
		node.Password = strVal(m["password"])
		node.Plugin = strVal(m["plugin"])
		if opts := strVal(m["plugin_opts"]); opts != "" {
			node.PluginOpts = parsePluginOpts(opts)
		}
		if node.Method == "" {
			return node, fmt.Errorf("shadowsocks outbound without method")
		}
	case "vmess":
		node.UUID = strVal(m["uuid"])
		node.AlterID = intVal(m["alter_id"])
	case "vless":
		node.UUID = strVal(m["uuid"])
		node.Flow = strVal(m["flow"])
	case "trojan":
		node.Password = strVal(m["password"])
		node.TLS = true
	case "hysteria2":
		node.Password = strVal(m["password"])
		node.TLS = true
		if obfs := mapVal(m["obfs"]); len(obfs) > 0 {
			node.Obfs = strVal(obfs["type"])
			node.ObfsPassword = strVal(obfs["password"])
		}
		node.UpMbps = intVal(m["up_mbps"])
		node.DownMbps = intVal(m["down_mbps"])
	case "tuic":
		node.UUID = strVal(m["uuid"])
		node.Password = strVal(m["password"])
		node.TLS = true
		if cc := strVal(m["congestion_control"]); cc != "" {
			node.Extra["congestion_control"] = cc
		}
		if mode := strVal(m["udp_relay_mode"]); mode != "" {
			node.Extra["udp_relay_mode"] = mode
		}
	case "socks":
		node.Protocol = "socks5"
		node.Username = strVal(m["username"])
		node.Password = strVal(m["password"])
	case "http":
		node.Username = strVal(m["username"])
		node.Password = strVal(m["password"])
	default:
		return node, fmt.Errorf("outbound type %q is not supported by proxy-tui", outboundType)
	}
	return node, nil
}

func applyTransport(node *parser.Node, transport map[string]any) {
	if len(transport) == 0 {
		node.Network = "tcp"
		return
	}
	switch strings.ToLower(strVal(transport["type"])) {
	case "ws":
		node.Network = "ws"
		node.Path = strVal(transport["path"])
		if headers := mapVal(transport["headers"]); len(headers) > 0 {
			node.Host = strVal(headers["Host"])
		}
	case "grpc":
		node.Network = "grpc"
		node.ServiceName = strVal(transport["service_name"])
	case "http", "h2":
		node.Network = "h2"
		node.Path = strVal(transport["path"])
		if hosts := stringSlice(transport["host"]); len(hosts) > 0 {
			node.Host = hosts[0]
		}
	default:
		node.Network = "tcp"
	}
}

func parsePluginOpts(raw string) map[string]string {
	opts := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		if part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			opts[k] = v
		} else {
			opts[part] = "true"
		}
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

func strVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func boolVal(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func intVal(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
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
