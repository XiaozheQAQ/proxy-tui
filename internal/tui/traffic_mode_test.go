package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/tui/dialogs"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
)

func selectSettingsRow(ui *Model, row int) {
	ui.setPage(pages.IDSettings)
	for i := 0; i < row; i++ {
		ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func TestTUNRowOpensConfirmationWithoutNetworkAction(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter on the TUN row must only open the confirmation")
	}
	if ui.dialog == nil || ui.confirm == nil {
		t.Fatal("expected a confirmation dialog")
	}
	if ui.confirm.Kind != pages.ActionSetTrafficMode || ui.confirm.Mode != network.ModeTUN {
		t.Fatalf("confirm = %+v", ui.confirm)
	}
	if ui.settings.TrafficMode != string(network.ModeOff) {
		t.Fatalf("mode changed before confirmation: %q", ui.settings.TrafficMode)
	}
	if ui.busy != "" {
		t.Fatalf("busy should stay empty before confirmation: %q", ui.busy)
	}
}

func TestTUNRowWhenEnabledConfirmsDisable(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	ui.tun.Enabled = true
	ui.rebuildData()
	selectSettingsRow(ui, 9)
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.confirm == nil || ui.confirm.Mode != network.ModeOff {
		t.Fatalf("confirm = %+v", ui.confirm)
	}
}

func TestSystemProxyRowConfirmsInsteadOfTUN(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 4)
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("no network action before confirmation")
	}
	if ui.confirm == nil || ui.confirm.Mode != network.ModeSystemProxy {
		t.Fatalf("confirm = %+v", ui.confirm)
	}
	if ui.confirm.Mode == network.ModeTUN {
		t.Fatal("system proxy row must not trigger TUN")
	}
	if ui.settings.TrafficMode != string(network.ModeOff) {
		t.Fatalf("mode changed: %q", ui.settings.TrafficMode)
	}
}

func TestConfirmDialogShowsTransition(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm, ok := ui.dialog.(*dialogs.Confirm)
	if !ok {
		t.Fatalf("dialog type = %T", ui.dialog)
	}
	if !strings.Contains(confirm.Message, "→") || !strings.Contains(strings.ToUpper(confirm.Message), "TUN") {
		t.Fatalf("message does not describe the transition: %q", confirm.Message)
	}
	if !strings.Contains(confirm.Message, network.ModeOff.Display()) {
		t.Errorf("message does not include the current mode: %q", confirm.Message)
	}
}

func TestCancelConfirmationChangesNothing(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		t.Fatal("cancel must not return a network command")
	}
	if ui.dialog != nil || ui.confirm != nil {
		t.Fatal("cancel must clear the pending action")
	}
	if ui.busy != "" {
		t.Fatalf("busy = %q", ui.busy)
	}
	if ui.settings.TrafficMode != string(network.ModeOff) {
		t.Fatalf("mode changed on cancel: %q", ui.settings.TrafficMode)
	}
}

func TestDialogBlocksGlobalShortcuts(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if ui.page != pages.IDSettings {
		t.Fatalf("global page shortcut leaked through the dialog: %s", ui.page)
	}
	if ui.dialog == nil {
		t.Fatal("dialog should stay open")
	}
}

func TestConfirmRunsExactlyOneAction(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("confirm should dispatch the traffic mode command")
	}
	if ui.busy == "" {
		t.Fatal("busy should be set while the action is in flight")
	}
	// Repeating Enter while busy must not queue a second transition.
	_, again := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if again != nil {
		t.Fatal("repeat key returned a second command")
	}
	if ui.dialog != nil || ui.confirm != nil {
		t.Fatal("a second confirmation was opened")
	}
}

func TestSettingsSelectionSurvivesPageSwitch(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	selectSettingsRow(ui, 9)
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("6")})
	_, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ui.confirm == nil || ui.confirm.Mode != network.ModeTUN {
		t.Fatalf("selection was not preserved: %+v", ui.confirm)
	}
}

func TestSmallTerminalStillHandlesKeys(t *testing.T) {
	ui := newTestModel(t, 40, 10)
	selectSettingsRow(ui, 9)
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("small terminal must not execute before confirmation")
	}
	if ui.confirm == nil || ui.confirm.Mode != network.ModeTUN {
		t.Fatalf("confirm = %+v", ui.confirm)
	}
}

func TestBusyModelIgnoresShortcuts(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	ui.busy = "changing traffic mode"
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("6")})
	if ui.page != pages.IDOverview {
		t.Fatalf("page changed while busy: %s", ui.page)
	}
	_, cmd := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("busy model accepted a command")
	}
}

func TestConfirmWhenAlreadyInModeDoesNothing(t *testing.T) {
	ui := newTestModel(t, 110, 34)
	ui.settings.TrafficMode = string(network.ModeSystemProxy)
	ui.rebuildData()
	cmd := ui.dispatch(pages.Action{Kind: pages.ActionSetTrafficMode, Mode: network.ModeSystemProxy})
	if cmd != nil || ui.confirm != nil || ui.dialog != nil {
		t.Fatalf("mode switch to the same mode should be a no-op: cmd=%v confirm=%+v", cmd, ui.confirm)
	}
}
