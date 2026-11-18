package subscription

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/configs/templates"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// RenderConfig produces a runnable configuration for target from a parsed
// subscription. Payloads that already target the core and carry their own
// routing are preserved verbatim; node-only payloads are expanded with the
// shipped base template.
func RenderConfig(res *parser.Result, target model.CoreKind) ([]byte, error) {
	if res.HasRouting {
		if res.Core == target {
			if len(res.Raw) == 0 {
				return nil, fmt.Errorf("payload has routing rules but the raw document is missing")
			}
			return append([]byte(nil), res.Raw...), nil
		}
		return nil, fmt.Errorf("%s config cannot be converted to %s without losing rules, DNS or groups; import it into a %s profile instead",
			res.Core.DisplayName(), target.DisplayName(), res.Core.DisplayName())
	}
	if len(res.Nodes) == 0 {
		if len(res.Raw) > 0 {
			return append([]byte(nil), res.Raw...), nil
		}
		return nil, fmt.Errorf("subscription contains no proxies to render")
	}
	switch target {
	case model.CoreMihomo:
		return RenderMihomo(res.Nodes)
	case model.CoreSingBox:
		return RenderSingBox(res.Nodes)
	}
	return nil, fmt.Errorf("unsupported core %q", target)
}

// RenderMihomo builds a full Clash/Mihomo config from nodes.
func RenderMihomo(nodes []parser.Node) ([]byte, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal(templates.MihomoBase(), &doc); err != nil {
		return nil, fmt.Errorf("mihomo template is invalid: %w", err)
	}
	proxies := make([]any, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		proxy, err := node.ToMihomo()
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", node.Name, err)
		}
		proxies = append(proxies, proxy)
		names = append(names, node.Name)
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies could be rendered for Mihomo")
	}
	doc["proxies"] = proxies
	doc["proxy-groups"] = mihomoGroups(names)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("render mihomo config: %w", err)
	}
	return out, nil
}

func mihomoGroups(names []string) []any {
	auto := make([]any, 0, len(names))
	for _, name := range names {
		auto = append(auto, name)
	}
	selectList := make([]any, 0, len(names)+2)
	selectList = append(selectList, "AUTO")
	for _, name := range names {
		selectList = append(selectList, name)
	}
	selectList = append(selectList, "DIRECT")
	return []any{
		map[string]any{
			"name":    "PROXY",
			"type":    "select",
			"proxies": selectList,
		},
		map[string]any{
			"name":      "AUTO",
			"type":      "url-test",
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  300,
			"tolerance": 50,
			"proxies":   auto,
		},
	}
}

// RenderSingBox builds a full sing-box config from nodes.
func RenderSingBox(nodes []parser.Node) ([]byte, error) {
	doc := map[string]any{}
	if err := json.Unmarshal(templates.SingBoxBase(), &doc); err != nil {
		return nil, fmt.Errorf("sing-box template is invalid: %w", err)
	}
	nodeOutbounds := make([]any, 0, len(nodes))
	nodeTags := make([]any, 0, len(nodes))
	for _, node := range nodes {
		out, err := node.ToSingBoxOutbound()
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", node.Name, err)
		}
		nodeOutbounds = append(nodeOutbounds, out)
		nodeTags = append(nodeTags, node.Name)
	}
	if len(nodeOutbounds) == 0 {
		return nil, fmt.Errorf("no outbounds could be rendered for sing-box")
	}

	selectorTargets := make([]any, 0, len(nodes)+2)
	selectorTargets = append(selectorTargets, "auto")
	selectorTargets = append(selectorTargets, nodeTags...)
	selectorTargets = append(selectorTargets, "direct")

	outbounds := []any{
		map[string]any{
			"type":      "selector",
			"tag":       "proxy",
			"outbounds": selectorTargets,
		},
		map[string]any{
			"type":      "urltest",
			"tag":       "auto",
			"outbounds": nodeTags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "5m",
		},
	}
	outbounds = append(outbounds, nodeOutbounds...)
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	doc["outbounds"] = outbounds

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render sing-box config: %w", err)
	}
	return append(out, '\n'), nil
}
