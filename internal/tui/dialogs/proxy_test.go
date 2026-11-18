package dialogs

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

func key(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

func press(t *testing.T, picker *ProxyPicker, names ...string) Intent {
	t.Helper()
	intent := Intent{}
	for _, name := range names {
		_, next, _ := picker.Update(key(name))
		intent = next
	}
	return intent
}

func nestedProxies() []core.Proxy {
	return []core.Proxy{
		{Name: "节点选择", Type: "Selector", Group: true, Now: "香港 01", All: []string{"自动选择", "香港 01", "DIRECT", "REJECT"}},
		{Name: "自动选择", Type: "URLTest", Group: true, Now: "香港 01", All: []string{"香港 01", "日本 01"}},
		{Name: "香港 01", Type: "Vless", Delay: 86, Alive: true},
		{Name: "日本 01", Type: "Hysteria2", Delay: 103, Alive: true},
		{Name: "DIRECT", Type: "Direct"},
		{Name: "REJECT", Type: "Reject"},
	}
}

func TestPickerShowsOnlyRootGroups(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	if len(picker.groups) != 1 || picker.groups[0].Name != "节点选择" {
		t.Fatalf("roots = %+v", picker.groups)
	}
}

func TestPickerEntersNestedGroup(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	press(t, picker, "tab")
	intent := press(t, picker, "enter")
	if intent.Kind != IntentNone {
		t.Fatalf("entering a group must not submit: %+v", intent)
	}
	view := picker.View(layout.DarkTheme(), 100)
	if !strings.Contains(view, "PATH: 自动选择") || !strings.Contains(view, "automatic group") {
		t.Fatalf("nested group view:\n%s", view)
	}
	intent = press(t, picker, "enter")
	if intent.Kind != IntentNone {
		t.Fatalf("automatic group selection must be refused: %+v", intent)
	}
	if !strings.Contains(picker.View(layout.DarkTheme(), 100), "automatic") {
		t.Error("refusal reason not rendered")
	}
}

func TestPickerSelectsLeaf(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	press(t, picker, "tab", "down")
	intent := press(t, picker, "enter")
	if intent.Kind != IntentSubmit {
		t.Fatalf("intent = %+v", intent)
	}
	result := intent.Payload.(ProxyResult)
	if result.Group != "节点选择" || result.Name != "香港 01" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPickerSelectsDirectAndReject(t *testing.T) {
	for _, name := range []string{"DIRECT", "REJECT"} {
		picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
		press(t, picker, "tab", "down", "down")
		if name == "REJECT" {
			press(t, picker, "down")
		}
		intent := press(t, picker, "enter")
		if intent.Kind != IntentSubmit {
			t.Fatalf("%s intent = %+v", name, intent)
		}
		if got := intent.Payload.(ProxyResult).Name; got != name {
			t.Fatalf("%s selected %s", name, got)
		}
	}
}

func TestPickerTestIntent(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	intent := press(t, picker, "tab", "down", "t")
	if intent.Kind != IntentTest {
		t.Fatalf("intent = %+v", intent)
	}
	request := intent.Payload.(ProxyTestRequest)
	if request.Name != "香港 01" {
		t.Fatalf("request = %+v", request)
	}
}

func TestPickerSearchFiltersMembers(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	press(t, picker, "tab", "/", "香", "港")
	view := picker.View(layout.DarkTheme(), 100)
	if !strings.Contains(view, "search: 香港") {
		t.Fatalf("search line missing:\n%s", view)
	}
	intent := press(t, picker, "enter")
	if intent.Kind != IntentNone {
		t.Fatalf("ending search must not submit: %+v", intent)
	}
	if got := len(picker.members()); got != 1 {
		t.Fatalf("filtered members = %d", got)
	}
}

func TestPickerEscPopsNestedGroupThenCancels(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nestedProxies())
	press(t, picker, "tab", "enter")
	intent := press(t, picker, "esc")
	if intent.Kind != IntentNone {
		t.Fatalf("first esc should pop: %+v", intent)
	}
	intent = press(t, picker, "esc")
	if intent.Kind != IntentCancel {
		t.Fatalf("second esc should cancel: %+v", intent)
	}
}

func TestPickerEmpty(t *testing.T) {
	picker := NewProxyPicker(model.CoreMihomo, nil)
	view := picker.View(layout.DarkTheme(), 100)
	if !strings.Contains(view, "no proxy groups") {
		t.Fatalf("empty view:\n%s", view)
	}
	intent := press(t, picker, "enter")
	if intent.Kind != IntentCancel {
		t.Fatalf("intent = %+v", intent)
	}
}
