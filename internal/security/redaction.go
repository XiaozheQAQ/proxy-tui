// Package security centralises secret handling: log redaction and
// permission checks.
package security

import (
	"net/url"
	"regexp"
	"strings"
)

// sensitiveParams are query keys whose values must never be printed.
var sensitiveParams = map[string]bool{
	"token": true, "access_token": true, "refresh_token": true,
	"key": true, "api_key": true, "apikey": true,
	"password": true, "passwd": true, "pwd": true,
	"secret": true, "auth": true, "authorization": true,
	"signature": true, "sig": true, "ticket": true, "sub": true,
	"uuid": true, "email": true,
}

const redacted = "REDACTED"

var inlineSecret = regexp.MustCompile(`(?i)((?:token|access_token|refresh_token|api[_-]?key|apikey|password|passwd|pwd|secret|signature|ticket|sub)=)([^&\s"']+)`)

// RedactURL masks credential-bearing parts of a URL while keeping enough
// structure for a human to recognise it.
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" {
		return Redact(trimmed)
	}
	if u.User != nil {
		username := u.User.Username()
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(username, redacted)
		}
	}
	if u.RawQuery != "" {
		q := u.Query()
		changed := false
		for key := range q {
			if sensitiveParams[strings.ToLower(key)] {
				q.Set(key, redacted)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	return u.String()
}

// Redact masks token-like key/value pairs in arbitrary text such as logs.
func Redact(s string) string {
	return inlineSecret.ReplaceAllString(s, "1"+redacted)
}

// MaskSecret keeps the first and last four characters of a secret.
func MaskSecret(s string) string {
	if len(s) <= 10 {
		return redacted
	}
	return s[:4] + strings.Repeat("*", 4) + s[len(s)-4:]
}
