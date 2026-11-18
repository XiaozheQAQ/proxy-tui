// Package templates embeds the base configuration templates that ProxyTUI
// uses when a subscription only ships proxy entries.
package templates

import _ "embed"

//go:embed mihomo/base.yaml
var mihomoBase []byte

//go:embed singbox/base.json
var singboxBase []byte

// MihomoBase returns the Clash/Mihomo YAML template.
func MihomoBase() []byte { return append([]byte(nil), mihomoBase...) }

// SingBoxBase returns the sing-box JSON template.
func SingBoxBase() []byte { return append([]byte(nil), singboxBase...) }
