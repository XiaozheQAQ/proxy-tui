package templates

import (
	"strings"
	"testing"
)

func TestMihomoBaseDNSBindsLoopback(t *testing.T) {
	base := string(MihomoBase())
	if !strings.Contains(base, "listen: 127.0.0.1:1053") {
		t.Fatalf("mihomo base DNS is not loopback: %s", base)
	}
	if strings.Contains(base, "0.0.0.0:1053") {
		t.Fatal("mihomo base DNS still listens on all interfaces")
	}
}
