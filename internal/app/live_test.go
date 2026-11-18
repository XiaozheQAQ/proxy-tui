package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// TestLiveProxySelection exercises the real Clash API and the local proxy
// end-to-end. Start a Mihomo core first and run with:
//
//	PROXY_TUI_LIVE=1 PROXY_TUI_HOME=<home> go test ./internal/app -run TestLiveProxySelection -v
func TestLiveProxySelection(t *testing.T) {
	if os.Getenv("PROXY_TUI_LIVE") != "1" {
		t.Skip("set PROXY_TUI_LIVE=1 with a running core")
	}
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	application, err := New(paths)
	if err != nil {
		t.Fatalf("app: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if !application.Cores.Status(ctx, model.CoreMihomo).Running {
		t.Skip("Mihomo is not running")
	}
	proxies, err := application.Cores.Proxies(ctx, model.CoreMihomo)
	if err != nil {
		t.Fatalf("proxies: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("no proxies reported")
	}
	byName := map[string]int{}
	for i, proxy := range proxies {
		byName[proxy.Name] = i
	}
	type candidate struct {
		group    string
		original string
		target   string
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, proxy := range proxies {
		if proxy.Type != "Selector" || len(proxy.All) == 0 {
			continue
		}
		for _, member := range proxy.All {
			index, ok := byName[member]
			if !ok {
				continue
			}
			leaf := proxies[index]
			if leaf.Group || leaf.Type == "Direct" || leaf.Type == "Reject" || leaf.Type == "" {
				continue
			}
			key := proxy.Name + "\x00" + member
			if seen[key] {
				continue
			}
			seen[key] = true
			candidates = append(candidates, candidate{group: proxy.Name, original: proxy.Now, target: member})
			if len(candidates) >= 8 {
				break
			}
		}
		if len(candidates) >= 8 {
			break
		}
	}
	if len(candidates) == 0 {
		t.Skip("no selector with an alternative real node")
	}
	var lastErr error
	for _, candidate := range candidates {
		if err := application.Cores.SelectProxy(ctx, model.CoreMihomo, candidate.group, candidate.target); err != nil {
			lastErr = err
			continue
		}
		delay, err := application.Cores.TestDelay(ctx, model.CoreMihomo, candidate.target, 8*time.Second)
		if err != nil || delay <= 0 {
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("delay = %d", delay)
			}
			_ = application.Cores.SelectProxy(context.Background(), model.CoreMihomo, candidate.group, candidate.original)
			continue
		}
		refreshed, err := application.Cores.Proxies(ctx, model.CoreMihomo)
		if err != nil {
			t.Fatalf("proxies after select: %v", err)
		}
		for _, proxy := range refreshed {
			if proxy.Name == candidate.group && proxy.Now != candidate.target {
				t.Fatalf("selection not applied: now=%q want=%q", proxy.Now, candidate.target)
			}
		}
		check := application.VerifyProxyConnectivity(ctx)
		if !check.OK {
			lastErr = fmt.Errorf("connectivity: %s", check.Err)
			_ = application.Cores.SelectProxy(context.Background(), model.CoreMihomo, candidate.group, candidate.original)
			continue
		}
		t.Logf("group=%q selected=%q delay=%dms exit=%s latency=%dms", candidate.group, candidate.target, delay, check.ExitIP, check.LatencyMS)
		_ = application.Cores.SelectProxy(context.Background(), model.CoreMihomo, candidate.group, candidate.original)
		return
	}
	t.Fatalf("no candidate worked: %v", lastErr)
}
