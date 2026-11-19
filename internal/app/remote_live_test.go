package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// TestLiveRemoteFrontend exercises the TUI-facing remote frontend against a
// running supervisor. Start it first:
//
//	proxy supervisor start
//	PROXY_TUI_LIVE=1 PROXY_TUI_HOME=<home> go test ./internal/app -run TestLiveRemoteFrontend -v
func TestLiveRemoteFrontend(t *testing.T) {
	if os.Getenv("PROXY_TUI_LIVE") != "1" {
		t.Skip("set PROXY_TUI_LIVE=1 with a running supervisor")
	}
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := ipc.Dial(ctx, ipc.EndpointPath(paths.RuntimeDir()), 5*time.Second)
	if err != nil {
		t.Fatalf("dial supervisor: %v", err)
	}
	defer client.Close()
	frontend, err := NewRemote(ctx, client)
	if err != nil {
		t.Fatalf("remote frontend: %v", err)
	}
	if err := frontend.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	kinds := frontend.Cores.Kinds()
	if len(kinds) == 0 {
		t.Fatal("no cores reported")
	}
	profiles := frontend.Subs.List()
	t.Logf("remote profiles=%d", len(profiles))
	if len(profiles) == 0 {
		t.Skip("no profiles in this home")
	}
	active, ok := frontend.Subs.ActiveFor(model.CoreMihomo)
	if !ok {
		for _, profile := range profiles {
			if profile.Core == model.CoreMihomo {
				if _, err := frontend.Subs.SetActive(profile.ID); err != nil {
					t.Fatalf("activate profile: %v", err)
				}
				active, ok = frontend.Subs.ActiveFor(model.CoreMihomo)
				break
			}
		}
	}
	if !ok {
		t.Skip("no Mihomo profile")
	}
	if frontend.Cores.Status(ctx, model.CoreMihomo).Running {
		t.Fatal("core should be stopped before the test")
	}
	if err := frontend.StartCore(ctx, model.CoreMihomo); err != nil {
		t.Fatalf("remote start: %v", err)
	}
	if !frontend.Cores.Status(ctx, model.CoreMihomo).Running {
		t.Fatal("core did not report RUNNING after remote start")
	}
	if err := frontend.StopCore(ctx, model.CoreMihomo); err != nil {
		t.Fatalf("remote stop: %v", err)
	}
	if frontend.Cores.Status(ctx, model.CoreMihomo).Running {
		t.Fatal("core still running after remote stop")
	}
	t.Logf("remote frontend OK: profile=%q kinds=%v", active.Label(), kinds)
}
