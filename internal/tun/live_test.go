package tun_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	mihomoadapter "github.com/proxytui/proxy-tui/internal/core/mihomo"
	singboxadapter "github.com/proxytui/proxy-tui/internal/core/singbox"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

const liveMihomoProfile = `
mixed-port: 7890
dns:
  enable: true
  nameserver: [223.5.5.5]
proxies:
  - {name: Tokyo, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
proxy-groups:
  - {name: PROXY, type: select, proxies: [Tokyo, DIRECT]}
rules:
  - MATCH,PROXY
`

const liveSingBoxProfile = `
{
  "log": {"level": "info"},
  "dns": {"servers": [{"type": "udp", "tag": "dns", "server": "1.1.1.1"}], "final": "dns"},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}],
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["ss-1", "direct"]},
    {"type": "shadowsocks", "tag": "ss-1", "server": "1.2.3.4", "server_port": 8388, "method": "aes-128-gcm", "password": "p"},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"rules": [{"ip_is_private": true, "outbound": "direct"}], "final": "proxy"}
}
`

// TestLiveCoreValidation renders TUN configuration with ProxyTUI and asks the
// real core binaries to validate it. It is opt-in because it needs installed
// cores:
//
//	PROXY_TUI_LIVE=1 PROXY_TUI_HOME=<dir with installed cores> \
//	  go test ./internal/tun -run TestLiveCoreValidation -v
func TestLiveCoreValidation(t *testing.T) {
	if os.Getenv("PROXY_TUI_LIVE") != "1" {
		t.Skip("set PROXY_TUI_LIVE=1 with installed cores to validate rendered configs")
	}
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	options := core.DefaultRuntimeOptions()
	mihomo := mihomoadapter.New(paths, options, nil)
	singbox := singboxadapter.New(paths, options, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cases := []struct {
		name     string
		kind     model.CoreKind
		raw      string
		version  string
		validate func(ctx context.Context, name string, data []byte) error
	}{
		{
			name: "mihomo", kind: model.CoreMihomo, raw: liveMihomoProfile,
			version: mihomo.Status(ctx).Version,
			validate: func(ctx context.Context, name string, data []byte) error {
				return mihomo.ValidateConfig(ctx, name, data)
			},
		},
		{
			name: "sing-box", kind: model.CoreSingBox, raw: liveSingBoxProfile,
			version: singbox.Status(ctx).Version,
			validate: func(ctx context.Context, name string, data []byte) error {
				return singbox.ValidateConfig(ctx, name, data)
			},
		},
	}

	for _, testCase := range cases {
		status := core.Status{}
		switch testCase.kind {
		case model.CoreMihomo:
			status = mihomo.Status(ctx)
		case model.CoreSingBox:
			status = singbox.Status(ctx)
		}
		if !status.Installed {
			t.Logf("%s is not installed; skipping", testCase.kind)
			continue
		}
		settings := tun.Defaults()
		rendered, err := tun.Render(testCase.kind, []byte(testCase.raw), settings, testCase.version)
		if err != nil {
			t.Fatalf("%s render: %v", testCase.name, err)
		}
		if err := testCase.validate(ctx, testCase.name+"-tun-live", rendered.Config); err != nil {
			t.Fatalf("%s rejected the rendered TUN config: %v\n%s", testCase.name, err, rendered.Config)
		}
		t.Logf("%s %s accepted the rendered TUN config (%d bytes)", testCase.kind, testCase.version, len(rendered.Config))
	}
}
