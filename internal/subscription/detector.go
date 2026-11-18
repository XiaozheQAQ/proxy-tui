package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser/shareuri"
)

// Detection is the result of sniffing a subscription payload.
type Detection struct {
	Core     model.CoreKind
	Format   model.Format
	Encoding model.Encoding
	Notes    []string
}

// Detect sniffs a payload and reports what it is. Core is empty for plain
// node lists because they are core agnostic.
func Detect(raw []byte) (Detection, error) {
	body := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	if len(body) == 0 {
		return Detection{}, fmt.Errorf("payload is empty")
	}

	if detection, ok, err := sniffStructured(body); ok {
		return detection, err
	}
	if detection, ok := sniffNodeList(body); ok {
		return detection, nil
	}
	if decoded, ok := tryBase64(body); ok {
		inner, err := Detect(decoded)
		if err == nil {
			inner.Encoding = model.EncodingBase64
			inner.Notes = append(inner.Notes, "payload was base64 encoded")
			return inner, nil
		}
	}
	return Detection{}, fmt.Errorf("unrecognised payload: not Mihomo YAML, sing-box JSON, base64 node list or share links")
}

func sniffStructured(body []byte) (Detection, bool, error) {
	if json.Valid(body) {
		doc := map[string]any{}
		if err := json.Unmarshal(body, &doc); err == nil {
			if isSingBoxDoc(doc) {
				return Detection{Core: model.CoreSingBox, Format: model.FormatSingBoxJSON, Encoding: model.EncodingPlain}, true, nil
			}
			if isMihomoDoc(doc) {
				return Detection{Core: model.CoreMihomo, Format: model.FormatMihomoYAML, Encoding: model.EncodingPlain}, true, nil
			}
		}
	}
	if !bytes.ContainsAny(body, ":\n") {
		return Detection{}, false, nil
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(body, &doc); err != nil || len(doc) == 0 {
		return Detection{}, false, nil
	}
	if isSingBoxDoc(doc) {
		return Detection{Core: model.CoreSingBox, Format: model.FormatSingBoxJSON, Encoding: model.EncodingPlain}, true, nil
	}
	if isMihomoDoc(doc) {
		return Detection{Core: model.CoreMihomo, Format: model.FormatMihomoYAML, Encoding: model.EncodingPlain}, true, nil
	}
	return Detection{}, false, nil
}

func isMihomoDoc(doc map[string]any) bool {
	for _, key := range []string{"proxies", "proxy-providers", "proxy-groups", "rules", "mixed-port", "socks-port", "redir-port"} {
		if _, ok := doc[key]; ok {
			return true
		}
	}
	return false
}

func isSingBoxDoc(doc map[string]any) bool {
	if _, ok := doc["outbounds"]; ok {
		return true
	}
	hasInbound := doc["inbounds"] != nil
	hasRoute := doc["route"] != nil
	hasDNS := doc["dns"] != nil
	hasLog := doc["log"] != nil
	return hasInbound || (hasRoute && (hasDNS || hasLog)) || (hasDNS && hasLog)
}

func sniffNodeList(body []byte) (Detection, bool) {
	total, matched := 0, 0
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		total++
		if shareuri.LooksLikeNodeURI(trimmed) {
			matched++
		}
	}
	if total > 0 && matched == total {
		return Detection{Format: model.FormatNodeList, Encoding: model.EncodingPlain}, true
	}
	return Detection{}, false
}

func tryBase64(body []byte) ([]byte, bool) {
	if len(body) < 16 {
		return nil, false
	}
	if bytes.ContainsAny(body, " \n\r\t") {
		// Base64 blobs are frequently wrapped by providers; only accept
		// wrapped payloads when every line looks like base64.
		for _, line := range bytes.Split(body, []byte("\n")) {
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && !isBase64Chars(trimmed) {
				return nil, false
			}
		}
	} else if !isBase64Chars(body) {
		return nil, false
	}
	cleaned := make([]byte, 0, len(body))
	for _, b := range body {
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		}
		cleaned = append(cleaned, b)
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(string(cleaned))
		if err != nil {
			continue
		}
		if !utf8.Valid(decoded) || len(decoded) == 0 {
			continue
		}
		if !mostlyPrintable(decoded) {
			continue
		}
		return decoded, true
	}
	return nil, false
}

func isBase64Chars(data []byte) bool {
	for _, b := range data {
		switch {
		case b >= 'A' && b <= 'Z':
		case b >= 'a' && b <= 'z':
		case b >= '0' && b <= '9':
		case b == '+', b == '/', b == '=', b == '-', b == '_':
		default:
			return false
		}
	}
	return len(data) > 0
}

func mostlyPrintable(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	printable := 0
	for _, b := range data {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b < 0x7F) || b >= 0xC0 {
			printable++
		}
	}
	return float64(printable)/float64(len(data)) > 0.9
}
