package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/dialogs"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
)

func runningMihomoModel(t *testing.T) *Model {
	t.Helper()
	ui := newTestModel(t, 120, 36)
	ui.statuses[model.CoreMihomo] = core.Status{Core: model.CoreMihomo, Installed: true, Running: true, APIAddr: "127.0.0.1:9090"}
	ui.live.kind = model.CoreMihomo
	ui.live.proxies = testProxies()
	ui.rebuildData()
	return ui
}

func TestProxyPickerRequiresRunningCore(t *testing.T) {
	ui := newTestModel(t, 120, 36)
	ui.setPage(pages.IDMihomo)
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if ui.dialog != nil {
		t.Fatal("picker must not open when the core is stopped")
	}
}

func TestProxyPickerOpensWithRunningCore(t *testing.T) {
	ui := runningMihomoModel(t)
	ui.setPage(pages.IDMihomo)
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if ui.dialog == nil {
		t.Fatal("picker did not open")
	}
	if !strings.Contains(ui.View(), "PROXIES") {
		t.Fatal("picker not rendered")
	}
}

func TestVerifyProxyStartsExplicitAction(t *testing.T) {
	ui := runningMihomoModel(t)
	cmd := ui.dispatch(pages.Action{Kind: pages.ActionVerifyProxy, Core: model.CoreMihomo})
	if cmd == nil {
		t.Fatal("verify did not return a command")
	}
	if ui.busy == "" {
		t.Fatal("verify should mark the model busy")
	}
}

func TestOverviewShowsTrafficModeAndProxyCheck(t *testing.T) {
	ui := newTestModel(t, 120, 36)
	ui.settings.TrafficMode = string(network.ModeOff)
	ui.rebuildData()
	view := ui.View()
	for _, want := range []string{"Traffic mode", "Proxy check", "not checked"} {
		if !strings.Contains(view, want) {
			t.Errorf("overview missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "OFF") {
		t.Errorf("overview should show traffic mode OFF:\n%s", view)
	}
}

func TestConnectivityMessageShowsResult(t *testing.T) {
	ui := runningMihomoModel(t)
	ui.Update(connectivityMsg{result: app.ProxyConnectivity{
		Attempted: true,
		OK:        true,
		Address:   "127.0.0.1:7890",
		ExitIP:    "203.0.113.9",
		LatencyMS: 42,
	}})
	if ui.connectivity.ExitIP != "203.0.113.9" {
		t.Fatalf("connectivity = %+v", ui.connectivity)
	}
	if ui.dialog == nil {
		t.Fatal("connectivity result should open a dialog")
	}
}

func TestProxyPickerBlockedWhenNoProxiesLoaded(t *testing.T) {
	ui := newTestModel(t, 120, 36)
	ui.statuses[model.CoreMihomo] = core.Status{Core: model.CoreMihomo, Running: true}
	ui.live.kind = model.CoreMihomo
	ui.live.proxies = nil
	ui.setPage(pages.IDMihomo)
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if ui.dialog != nil {
		t.Fatal("picker must not open with no loaded proxies")
	}
}

var _ = dialogs.IntentNone
