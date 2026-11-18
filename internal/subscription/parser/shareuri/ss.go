package shareuri

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// parseSS handles both SIP002 (ss://base64(method:pass)@host:port) and the
// legacy fully base64 encoded form.
func parseSS(rest string) (parser.Node, error) {
	node := parser.Node{Protocol: "ss"}

	body, name := splitFragment(rest)
	body, query := splitQuery(body)
	body = strings.TrimSuffix(strings.TrimSpace(body), "/")
	node.Name = name

	var hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userinfo := body[:at]
		hostport = body[at+1:]
		decoded, err := decodeBase64Loose(userinfo)
		if err != nil {
			decoded = userinfo
		}
		method, password, ok := strings.Cut(decoded, ":")
		if !ok || method == "" {
			return node, fmt.Errorf("ss: malformed credentials")
		}
		node.Method = method
		node.Password = password
	} else {
		decoded, err := decodeBase64Loose(body)
		if err != nil {
			return node, fmt.Errorf("ss: payload is not valid base64: %w", err)
		}
		at := strings.LastIndex(decoded, "@")
		if at < 0 {
			return node, fmt.Errorf("ss: missing server part")
		}
		method, password, ok := strings.Cut(decoded[:at], ":")
		if !ok || method == "" {
			return node, fmt.Errorf("ss: malformed credentials")
		}
		node.Method = method
		node.Password = password
		hostport = decoded[at+1:]
	}

	host, port, err := splitHostPort(hostport)
	if err != nil {
		return node, fmt.Errorf("ss: %w", err)
	}
	node.Server = host
	node.Port = port

	if query != "" {
		if q, err := url.ParseQuery(query); err == nil {
			if plugin := q.Get("plugin"); plugin != "" {
				parts := strings.Split(plugin, ";")
				node.Plugin = parts[0]
				opts := map[string]string{}
				for _, part := range parts[1:] {
					if part == "" {
						continue
					}
					if k, v, ok := strings.Cut(part, "="); ok {
						opts[k] = v
					} else {
						opts[part] = "true"
					}
				}
				if len(opts) > 0 {
					node.PluginOpts = opts
				}
			}
		}
	}

	if node.Name == "" {
		node.Name = fmt.Sprintf("%s:%d", node.Server, node.Port)
	}
	return node, nil
}
