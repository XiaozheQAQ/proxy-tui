package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/dialogs"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
)

func newTestModel(t *testing.T, width, height int) *Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	application, err := app.New(paths)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	ui := New(application)
	ui.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return ui
}

func testProxies() []core.Proxy {
	return []core.Proxy{
		{Name: "PROXY", Type: "Selector", Group: true, Now: "NodeA", All: []string{"NodeA", "NodeB", "DIRECT"}},
		{Name: "NodeA", Type: "Shadowsocks", Delay: 88, Alive: true},
		{Name: "NodeB", Type: "Trojan", Delay: 120, Alive: true},
		{Name: "AUTO", Type: "URLTest", Group: true, Now: "NodeB", All: []string{"NodeA", "NodeB"}},
	}
}

func TestViewAt80x24(t *testing.T) {
	ui := newTestModel(t, 80, 24)
	view := ui.View()
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Fatalf("view height = %d, want 24\n%s", lines, view)
	}
	for _, want := range []string{"PROXY TUI", "NAVIGATION", "Overview", "CURRENT CORE"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q", want)
		}
	}
	for i, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width != 80 {
			t.Errorf("line %d width = %d, want 80: %q", i, width, line)
		}
	}
}

func TestViewAt120x40(t *testing.T) {
	ui := newTestModel(t, 120, 40)
	view := ui.View()
	if lines := strings.Count(view, "\n") + 1; lines != 40 {
		t.Fatalf("view height = %d, want 40", lines)
	}
	for i, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width != 120 {
			t.Errorf("line %d width = %d, want 120", i, width)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	ui := newTestModel(t, 40, 10)
	view := ui.View()
	if !strings.Contains(view, "Terminal too small") {
		t.Fatalf("expected the too-small message, got %q", view)
	}
}

func TestNavigationSwitchesPages(t *testing.T) {
	ui := newTestModel(t, 100, 30)
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ui.page != pages.IDMihomo {
		t.Fatalf("page after tab = %q", ui.page)
	}
	view := ui.View()
	if !strings.Contains(view, "CORE") || !strings.Contains(view, "PROFILES") {
		t.Errorf("mihomo page did not render its sections:\n%s", view)
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ui.page != pages.IDSingBox {
		t.Fatalf("page after second tab = %q", ui.page)
	}
}

func TestAddDialogFitsTerminal(t *testing.T) {
	ui := newTestModel(t, 80, 24)
	ui.setDialog(dialogs.NewAddProfile(model.CoreMihomo))
	view := ui.View()
	if !strings.Contains(view, "ADD SUBSCRIPTION") {
		t.Fatalf("dialog not rendered:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Fatalf("dialog view height = %d, want 24", lines)
	}
	for i, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width != 80 {
			t.Errorf("dialog line %d width = %d, want 80: %q", i, width, line)
		}
	}
}

func TestHelpDialogRenders(t *testing.T) {
	ui := newTestModel(t, 100, 30)
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if ui.dialog == nil {
		t.Fatal("help dialog did not open")
	}
	view := ui.View()
	if !strings.Contains(view, "KEYBOARD REFERENCE") {
		t.Fatalf("help not rendered:\n%s", view)
	}
}

func TestImportThroughModel(t *testing.T) {
	ui := newTestModel(t, 90, 26)
	input := strings.Join([]string{
		"ss://YWVzLTEyOC1nY206cA@1.2.3.4:8388#NodeA",
		"trojan://pw@example.com:443#NodeB",
	}, "\n")
	cmd := ui.cmdImport(dialogs.AddProfileResult{Core: string(model.CoreMihomo), Content: input})
	if cmd == nil {
		t.Fatal("import command is nil")
	}
	ui.Update(cmd())
	if len(ui.app.Subs.List()) != 1 {
		t.Fatalf("profiles = %d, want 1", len(ui.app.Subs.List()))
	}
	view := ui.View()
	if !strings.Contains(view, "ACTIVE PROFILE") {
		t.Errorf("overview did not render:\n%s", view)
	}
}

func TestSettingsTogglePersists(t *testing.T) {
	ui := newTestModel(t, 100, 30)
	ui.setPage(pages.IDSettings)
	before := ui.settings.Theme
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.settings.Theme == before {
		t.Fatal("theme did not toggle")
	}
	if cmd != nil {
		ui.Update(cmd())
	}
	reloaded, err := storage.LoadSettings(ui.app.Paths)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if reloaded.Theme != ui.settings.Theme {
		t.Errorf("persisted theme = %q, want %q", reloaded.Theme, ui.settings.Theme)
	}
}

func TestSettingsShowsNetworkState(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	ui.network = network.Report{
		Capabilities: network.Capabilities{Platform: "windows", Supported: true, Method: "test"},
		Desktop:      network.ProxyState{Enabled: true, Host: "127.0.0.1", Port: 10808},
		Environment:  network.EnvironmentProxy{HTTP: "http://127.0.0.1:7890"},
	}
	ui.rebuildData()
	ui.setPage(pages.IDSettings)
	view := ui.View()
	for _, want := range []string{"Traffic mode", "Desktop proxy", "127.0.0.1:10808", "Shell proxy", "TUN", "TUN stack", "TUN MTU", "Run TUN preflight"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view is missing %q:\n%s", want, view)
		}
	}
}

func TestTrafficModeRowDispatchesAction(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	ui.setPage(pages.IDSettings)
	for i := 0; i < 4; i++ {
		ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter must not start a network operation before confirmation")
	}
	if ui.dialog == nil || ui.confirm == nil {
		t.Fatal("expected a confirmation dialog")
	}
	if ui.confirm.Kind != pages.ActionSetTrafficMode || ui.confirm.Mode != network.ModeSystemProxy {
		t.Fatalf("confirm = %+v", ui.confirm)
	}
	if ui.settings.TrafficMode != string(network.ModeOff) {
		t.Fatalf("traffic mode changed before confirmation: %q", ui.settings.TrafficMode)
	}
}

func TestProxyPickerRenders(t *testing.T) {
	ui := newTestModel(t, 100, 30)
	ui.setDialog(dialogs.NewProxyPicker(model.CoreMihomo, testProxies()))
	view := ui.View()
	if !strings.Contains(view, "SELECT PROXY") {
		t.Fatalf("picker not rendered:\n%s", view)
	}
}

func TestProxyPickerSelection(t *testing.T) {
	picker := dialogs.NewProxyPicker(model.CoreMihomo, testProxies())
	_, intent, _ := picker.Update(tea.KeyMsg{Type: tea.KeyTab})
	if intent.Kind != dialogs.IntentNone {
		t.Fatalf("unexpected intent after tab: %+v", intent)
	}
	picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, intent, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != dialogs.IntentSubmit {
		t.Fatalf("expected submit intent, got %+v", intent)
	}
	result, ok := intent.Payload.(dialogs.ProxyResult)
	if !ok {
		t.Fatalf("unexpected payload %T", intent.Payload)
	}
	if result.Group != "PROXY" || result.Name != "NodeB" {
		t.Errorf("selection = %+v", result)
	}
}
