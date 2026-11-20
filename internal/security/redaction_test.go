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

func TestRedactURLPathTokenCanary(t *testing.T) {
	raw := "https://example.com/api/v1/client/subscribe/PROXYTUI_TEST_TOKEN_53Z?token=PROXYTUI_TEST_SECRET_SUB_91X&password=PROXYTUI_TEST_PASSWORD_72Y"
	got := RedactURL(raw)
	for _, canary := range []string{"PROXYTUI_TEST_SECRET_SUB_91X", "PROXYTUI_TEST_PASSWORD_72Y", "PROXYTUI_TEST_TOKEN_53Z"} {
		if strings.Contains(got, canary) {
			t.Errorf("canary leaked from RedactURL: %s", got)
		}
	}
	if !strings.Contains(got, "example.com") {
		t.Errorf("host was removed: %s", got)
	}
}

func TestRedactTextPathCanary(t *testing.T) {
	got := Redact("GET https://example.com/api/PROXYTUI_TEST_TOKEN_53Z failed: password=PROXYTUI_TEST_PASSWORD_72Y")
	if strings.Contains(got, "PROXYTUI_TEST_TOKEN_53Z") || strings.Contains(got, "PROXYTUI_TEST_PASSWORD_72Y") {
		t.Errorf("canary leaked from Redact: %s", got)
	}
}
