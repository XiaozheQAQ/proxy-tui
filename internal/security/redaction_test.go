package security

import (
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	raw := "https://example.com/api/v1/sub?token=abcdef123456&mode=rule&sub=my-sub"
	got := RedactURL(raw)
	for _, secret := range []string{"abcdef123456", "my-sub"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted URL still contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "mode=rule") {
		t.Errorf("benign query parameter was removed: %s", got)
	}
	if !strings.Contains(got, "example.com") {
		t.Errorf("host was removed: %s", got)
	}
}

func TestRedactURLCredentials(t *testing.T) {
	got := RedactURL("https://user:s3cret@example.com/sub")
	if strings.Contains(got, "s3cret") {
		t.Errorf("password leaked: %s", got)
	}
	if !strings.Contains(got, "user") {
		t.Errorf("username should survive: %s", got)
	}
}

func TestRedactText(t *testing.T) {
	got := Redact("failed: token=abc123&password=xyz")
	if strings.Contains(got, "abc123") || strings.Contains(got, "xyz") {
		t.Errorf("secret leaked: %s", got)
	}
}

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("1234567890abcdef"); got == "1234567890abcdef" {
		t.Error("secret was not masked")
	}
	if got := MaskSecret("short"); got != "REDACTED" {
		t.Errorf("short secret = %q", got)
	}
}
