package core

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomSecret returns a 32 character hex secret for the core control API.
func RandomSecret() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "proxy-tui-fallback-secret"
	}
	return hex.EncodeToString(buf)
}
