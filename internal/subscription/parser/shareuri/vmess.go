package shareuri

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// parseVMess decodes the base64 JSON payload used by vmess:// links.
func parseVMess(rest string) (parser.Node, error) {
	payload := strings.TrimSpace(rest)
	if i := strings.Index(payload, "#"); i >= 0 {
		payload = payload[:i]
	}
	payload = strings.TrimSuffix(payload, "/")

	decoded, err := decodeBase64Loose(payload)
	if err != nil {
		return parser.Node{}, fmt.Errorf("vmess: payload is not valid base64: %w", err)
	}

	var raw struct {
		PS            string          `json:"ps"`
		Add           string          `json:"add"`
		Port          json.RawMessage `json:"port"`
		ID            string          `json:"id"`
		Aid           json.RawMessage `json:"aid"`
		Scy           string          `json:"scy"`
		Net           string          `json:"net"`
		Host          string          `json:"host"`
		Path          string          `json:"path"`
		TLS           string          `json:"tls"`
		SNI           string          `json:"sni"`
		ALPN          json.RawMessage `json:"alpn"`
		FP            string          `json:"fp"`
		Type          string          `json:"type"`
		AllowInsecure bool            `json:"allowInsecure"`
	}
	if err := json.Unmarshal([]byte(decoded), &raw); err != nil {
		return parser.Node{}, fmt.Errorf("vmess: invalid JSON payload: %w", err)
	}
	if raw.Add == "" || raw.ID == "" {
		return parser.Node{}, fmt.Errorf("vmess: missing server or uuid")
	}
	port := rawInt(raw.Port, 0)
	if port <= 0 || port > 65535 {
		return parser.Node{}, fmt.Errorf("vmess: invalid port %d", port)
	}

	node := parser.Node{
		Protocol:    "vmess",
		Name:        raw.PS,
		Server:      raw.Add,
		Port:        port,
		UUID:        raw.ID,
		AlterID:     rawInt(raw.Aid, 0),
		Network:     strings.ToLower(raw.Net),
		TLS:         strings.EqualFold(raw.TLS, "tls"),
		SNI:         raw.SNI,
		Host:        raw.Host,
		Path:        pct(raw.Path),
		Fingerprint: raw.FP,
		Insecure:    raw.AllowInsecure,
		ALPN:        rawStringList(raw.ALPN),
	}
	if node.SNI == "" && node.Host != "" && raw.Type == "" {
		node.SNI = node.Host
	}
	if node.Network == "" {
		node.Network = "tcp"
	}
	if raw.Scy != "" && !strings.EqualFold(raw.Scy, "auto") {
		node.Extra = map[string]any{"scy": raw.Scy}
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("vmess-%s:%d", node.Server, node.Port)
	}
	return node, nil
}
