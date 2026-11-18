package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/dialogs"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
	"github.com/proxytui/proxy-tui/internal/tun"
)

type tickMsg time.Time

type actionResultMsg struct {
	label       string
	err         error
	mode        network.Mode
	modeChanged bool
	warnings    []string
	report      *tun.Report
}

type connectivityMsg struct {
	result app.ProxyConnectivity
}

type preflightMsg struct {
	report tun.Report
	err    error
}

type recoveryMsg struct {
	result tun.RecoveryResult
	err    error
}

type importResultMsg struct {
	outcomes []subscription.ImportOutcome
	err      error
}

type updateResultMsg struct {
	profile  model.Profile
	outcomes []subscription.UpdateOutcome
	all      bool
	err      error
}

type deleteResultMsg struct {
	name string
	err  error
}

type testResultMsg struct {
	ok     int
	failed int
	best   string
	err    error
}

type doctorResultMsg struct {
	lines []string
}

type settingsSavedMsg struct {
	err error
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
		m.ready = true
		m.rebuildData()
		return m, nil

	case refreshMsg:
		m.statuses = typed.statuses
		m.profiles = typed.profiles
		m.live = typed.live
		m.network = typed.network
		if typed.networkErr != "" {
			m.network = network.Report{
				Capabilities: network.Capabilities{Supported: false, Reason: typed.networkErr},
			}
		}
		m.rebuildData()
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{m.refreshCmd(), tickCmd()}
		if auto := m.autoUpdateCmd(); auto != nil {
			cmds = append(cmds, auto)
		}
		return m, tea.Batch(cmds...)

	case actionResultMsg:
		m.busy = ""
		if typed.err != nil {
			if typed.report != nil {
				lines := append([]string{"error: " + typed.err.Error(), ""}, typed.report.Lines()...)
				m.setDialog(dialogs.NewInfo("TUN PREFLIGHT", lines))
			} else {
				m.notice("error", "%s: %s", typed.label, typed.err.Error())
			}
			return m, m.refreshCmd()
		}
		level := "ok"
		if len(typed.warnings) > 0 {
			level = "warn"
			m.notice(level, "%s — %s", typed.label, typed.warnings[0])
		} else {
			m.notice(level, "%s", typed.label)
		}
		if typed.modeChanged {
			m.settings.TrafficMode = string(typed.mode)
			m.rebuildData()
			return m, tea.Batch(m.refreshCmd(), m.saveSettings())
		}
		return m, m.refreshCmd()

	case connectivityMsg:
		m.busy = ""
		m.connectivity = typed.result
		m.rebuildData()
		lines := []string{
			"target: " + typed.result.Address,
		}
		if typed.result.OK {
			lines = append(lines,
				"exit IP: "+typed.result.ExitIP,
				fmt.Sprintf("latency: %d ms", typed.result.LatencyMS),
				"HTTP request went through the local proxy port (not the system proxy).",
			)
			m.notice("ok", "proxy connectivity: %s (%d ms)", typed.result.ExitIP, typed.result.LatencyMS)
		} else {
			lines = append(lines, "error: "+typed.result.Err)
			m.notice("error", "proxy connectivity failed: %s", typed.result.Err)
		}
		if m.effectiveTrafficMode() == network.ModeOff {
			lines = append(lines, "", "traffic mode is OFF: this proves the local proxy works, but system traffic is not redirected.")
		}
		m.setDialog(dialogs.NewInfo("PROXY CONNECTIVITY", lines))
		return m, nil

	case preflightMsg:
		m.busy = ""
		if typed.err != nil {
			m.setDialog(dialogs.NewInfo("TUN PREFLIGHT", []string{"error: " + typed.err.Error()}))
			return m, nil
		}
		m.setDialog(dialogs.NewInfo("TUN PREFLIGHT", typed.report.Lines()))
		return m, nil

	case recoveryMsg:
		m.busy = ""
		if typed.err != nil {
			m.notice("error", "recover TUN: %s", typed.err.Error())
			return m, m.refreshCmd()
		}
		if typed.result.Action == "" {
			m.notice("info", "nothing to recover")
			return m, m.refreshCmd()
		}
		level := "ok"
		if !typed.result.Released {
			level = "warn"
		}
		m.notice(level, "%s", typed.result.Action)
		return m, m.refreshCmd()

	case importResultMsg:
		return m.handleImportResult(typed)

	case updateResultMsg:
		return m.handleUpdateResult(typed)

	case deleteResultMsg:
		m.busy = ""
		if typed.err != nil {
			m.notice("error", "delete %s: %s", typed.name, typed.err.Error())
		} else {
			m.notice("ok", "deleted %s", typed.name)
		}
		return m, m.refreshCmd()

	case testResultMsg:
		m.busy = ""
		if typed.err != nil {
			m.notice("error", "latency test: %s", typed.err.Error())
		} else {
			m.notice("ok", "latency: %d ok, %d failed, best %s", typed.ok, typed.failed, typed.best)
		}
		return m, m.refreshCmd()

	case doctorResultMsg:
		m.setDialog(dialogs.NewInfo("DOCTOR", typed.lines))
		return m, nil

	case settingsSavedMsg:
		if typed.err != nil {
			m.notice("error", "save settings: %s", typed.err.Error())
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(typed)

	case tea.MouseMsg:
		return m.handleMouse(typed)
	}

	if m.dialog != nil {
		return m.updateDialog(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dialog != nil {
		return m.updateDialog(msg)
	}
	if msg.Paste {
		m.notice("info", "pasted %d bytes — press A to import it", len(msg.Runes))
		return m, nil
	}
	if m.busy != "" {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m.requestQuit()
	case "?":
		m.setDialog(dialogs.NewHelp())
		return m, nil
	case "/", "ctrl+p":
		m.setDialog(dialogs.NewCommand())
		return m, nil
	case "tab":
		m.setPage(pages.IDs()[(m.navIndex+1)%len(pages.IDs())])
		return m, nil
	case "shift+tab":
		index := (m.navIndex - 1 + len(pages.IDs())) % len(pages.IDs())
		m.setPage(pages.IDs()[index])
		return m, nil
	case "A":
		m.setDialog(dialogs.NewAddProfile(m.core))
		return m, nil
	case "u":
		profile, ok := m.selector()
		if !ok {
			m.notice("warn", "select a profile on the %s page first", m.core.DisplayName())
			return m, nil
		}
		m.busy = "updating " + profile.Label()
		return m, m.cmdUpdateProfile(profile.ID)
	case "d":
		profile, ok := m.selector()
		if !ok {
			m.notice("warn", "select a profile on the %s page first", m.core.DisplayName())
			return m, nil
		}
		m.confirm = &pages.Action{Kind: pages.ActionDeleteProfile, Core: profile.Core, ProfileID: profile.ID}
		m.dialog = dialogs.NewConfirm(fmt.Sprintf("Delete profile %q and its snapshots?", profile.Label()))
		return m, nil
	case "1", "2", "3", "4", "5", "6":
		index := int(msg.String()[0] - '1')
		if index >= 0 && index < len(pages.IDs()) {
			m.setPage(pages.IDs()[index])
		}
		return m, nil
	}

	page, action, cmd := m.currentPage().Update(msg)
	m.pages[m.page] = page
	if action != nil {
		return m, m.dispatch(*action)
	}
	return m, cmd
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	geometry := layout.Compute(m.width, m.height)
	if geometry.TooSmall {
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X < geometry.SidebarWidth {
		// header (2) + NAVIGATION (1) + spacer (1) precede the items
		index := msg.Y - 4
		if index >= 0 && index < len(pages.IDs()) {
			m.setPage(pages.IDs()[index])
		}
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	case tea.MouseButtonWheelDown:
		return m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	return m, nil
}

func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if !m.settings.ConfirmQuit {
		return m, tea.Quit
	}
	m.confirm = &pages.Action{Kind: pages.ActionQuit}
	m.dialog = dialogs.NewConfirm("Quit proxy-tui? A running proxy core keeps running in the background.")
	return m, nil
}

func (m *Model) updateDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	dialog, intent, cmd := m.dialog.Update(msg)
	m.dialog = dialog
	switch intent.Kind {
	case dialogs.IntentCancel:
		m.dialog = nil
		m.confirm = nil
		return m, nil
	case dialogs.IntentSubmit:
		switch payload := intent.Payload.(type) {
		case dialogs.AddProfileResult:
			return m, m.cmdImport(payload)
		case dialogs.ProxyResult:
			m.dialog = nil
			kind, err := model.ParseCoreKind(payload.Core)
			if err != nil {
				m.notice("error", "%s", err.Error())
				return m, nil
			}
			return m, m.cmdSelectProxy(kind, payload.Group, payload.Name)
		case dialogs.ProxyTestRequest:
			m.dialog = nil
			kind, err := model.ParseCoreKind(payload.Core)
			if err != nil {
				m.notice("error", "%s", err.Error())
				return m, nil
			}
			m.busy = "testing " + payload.Name
			return m, m.cmdTestProxy(kind, payload.Name)
		case dialogs.ConfirmResult:
			target := m.confirm
			m.dialog = nil
			m.confirm = nil
			if payload.Confirmed && target != nil {
				if target.Kind == pages.ActionSetTrafficMode {
					m.busy = "changing traffic mode"
					return m, m.cmdSetTrafficMode(target.Mode)
				}
				return m, m.dispatch(*target)
			}
			if !payload.Confirmed {
				m.notice("info", "cancelled")
			}
			return m, nil
		case dialogs.CommandResult:
			m.dialog = nil
			return m, m.runCommand(payload.Line)
		}
	}
	return m, cmd
}

func (m *Model) dispatch(action pages.Action) tea.Cmd {
	switch action.Kind {
	case pages.ActionQuit:
		return tea.Quit
	case pages.ActionRefresh:
		return m.refreshCmd()
	case pages.ActionAddProfile:
		kind := action.Core
		if !kind.Valid() {
			kind = m.core
		}
		m.setDialog(dialogs.NewAddProfile(kind))
		return nil
	case pages.ActionUseProfile:
		m.busy = "activating profile"
		return m.cmdUseProfile(action.ProfileID)
	case pages.ActionUpdateProfile:
		m.busy = "updating subscription"
		return m.cmdUpdateProfile(action.ProfileID)
	case pages.ActionDeleteProfile:
		m.busy = "deleting profile"
		return m.cmdDeleteProfile(action.ProfileID)
	case pages.ActionStartCore:
		m.busy = "starting " + action.Core.DisplayName()
		return m.cmdStartCore(action.Core)
	case pages.ActionStopCore:
		m.busy = "stopping " + action.Core.DisplayName()
		return m.cmdStopCore(action.Core)
	case pages.ActionRestartCore:
		m.busy = "restarting " + action.Core.DisplayName()
		return m.cmdRestartCore(action.Core)
	case pages.ActionProxyPicker:
		if !m.statusOf(action.Core).Running {
			m.notice("warn", "%s is not running", action.Core.DisplayName())
			return nil
		}
		if m.live.kind != action.Core || len(m.live.proxies) == 0 {
			m.notice("warn", "no proxy groups loaded for %s yet; press R to refresh", action.Core.DisplayName())
			return nil
		}
		m.setDialog(dialogs.NewProxyPicker(action.Core, m.live.proxies))
		return nil
	case pages.ActionVerifyProxy:
		m.busy = "verifying proxy connectivity"
		return m.cmdVerifyProxy()
	case pages.ActionTestProxies:
		if !m.statusOf(action.Core).Running {
			m.notice("warn", "%s is not running", action.Core.DisplayName())
			return nil
		}
		m.busy = "testing node latency"
		return m.cmdTestProxies(action.Core)
	case pages.ActionSelectProxy:
		return m.cmdSelectProxy(action.Core, action.Group, action.Proxy)
	case pages.ActionSetTrafficMode:
		return m.requestTrafficMode(action.Mode)
	case pages.ActionToggleSystemProxy:
		if m.network.Desktop.Enabled {
			return m.requestTrafficMode(network.ModeOff)
		}
		return m.requestTrafficMode(network.ModeSystemProxy)
	case pages.ActionToggleTUN:
		if m.tun.Enabled {
			return m.requestTrafficMode(network.ModeOff)
		}
		return m.requestTrafficMode(network.ModeTUN)
	case pages.ActionCycleTunStack:
		m.settings.TunStack = nextTunStack(m.settings.TunStack)
		m.rebuildData()
		return m.reapplyTunIfActive()
	case pages.ActionCycleTunMTU:
		m.settings.TunMTU = nextTunMTU(m.settings.TunMTU)
		m.rebuildData()
		return m.reapplyTunIfActive()
	case pages.ActionToggleTunDNS:
		m.settings.TunDNSHijack = !m.settings.TunDNSHijack
		m.rebuildData()
		return m.reapplyTunIfActive()
	case pages.ActionToggleTunIPv6:
		m.settings.TunIPv6 = !m.settings.TunIPv6
		m.rebuildData()
		return m.reapplyTunIfActive()
	case pages.ActionRunTUNPreflight:
		m.busy = "running TUN preflight"
		return m.cmdTUNPreflight()
	case pages.ActionRecoverTUN:
		m.busy = "recovering TUN state"
		return m.cmdRecoverTUN()
	case pages.ActionToggleTheme:
		if m.settings.Theme == storage.ThemeDark {
			m.settings.Theme = storage.ThemeLight
		} else {
			m.settings.Theme = storage.ThemeDark
		}
		m.rebuildData()
		m.notice("ok", "theme: %s", m.settings.Theme)
		return m.saveSettings()
	case pages.ActionToggleAutoUpdate:
		m.settings.AutoUpdate = !m.settings.AutoUpdate
		m.rebuildData()
		m.notice("ok", "auto update: %v", m.settings.AutoUpdate)
		return m.saveSettings()
	case pages.ActionToggleConfirmQuit:
		m.settings.ConfirmQuit = !m.settings.ConfirmQuit
		m.rebuildData()
		m.notice("ok", "confirm quit: %v", m.settings.ConfirmQuit)
		return m.saveSettings()
	case pages.ActionCycleInterval:
		m.settings.UpdateIntervalSeconds = nextInterval(m.settings.UpdateIntervalSeconds)
		m.app.Subs.SetDefaultInterval(m.settings.UpdateIntervalSeconds)
		m.rebuildData()
		m.notice("ok", "update interval: %d h", m.settings.UpdateIntervalSeconds/3600)
		return m.saveSettings()
	case pages.ActionClearLogs:
		m.app.Logs.Clear()
		m.notice("ok", "logs cleared")
		return m.refreshCmd()
	case pages.ActionHelp:
		m.setDialog(dialogs.NewHelp())
		return nil
	case pages.ActionCommandPalette:
		m.setDialog(dialogs.NewCommand())
		return nil
	}
	return nil
}

func nextInterval(current int) int {
	steps := []int{3600, 3 * 3600, 6 * 3600, 12 * 3600, 24 * 3600}
	for i, step := range steps {
		if step == current {
			return steps[(i+1)%len(steps)]
		}
	}
	return steps[0]
}

func (m *Model) handleImportResult(result importResultMsg) (tea.Model, tea.Cmd) {
	m.importing = false
	setter, hasSetter := m.dialog.(dialogs.StatusSetter)
	if result.err != nil {
		if hasSetter {
			setter.SetError(result.err.Error())
		} else {
			m.notice("error", "%s", result.err.Error())
		}
		return m, nil
	}

	success := 0
	var failures []string
	warnings := 0
	for _, outcome := range result.outcomes {
		if outcome.Err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", outcome.Source, outcome.Err.Error()))
			continue
		}
		success++
		warnings += len(outcome.Warnings)
	}

	switch {
	case success == 0 && len(failures) > 0:
		if hasSetter {
			setter.SetError(failures[0])
		} else {
			m.notice("error", "%s", failures[0])
		}
		return m, nil
	case success == 0:
		if hasSetter {
			setter.SetError("nothing was imported")
		}
		return m, nil
	}

	m.dialog = nil
	if success > 0 {
		for _, outcome := range result.outcomes {
			if outcome.Err == nil && outcome.Profile.Core.Valid() {
				m.core = outcome.Profile.Core
				if outcome.Profile.Core == model.CoreSingBox {
					m.setPage(pages.IDSingBox)
				} else {
					m.setPage(pages.IDMihomo)
				}
				break
			}
		}
	}
	if len(failures) > 0 {
		m.notice("warn", "imported %d profile(s), %d failed: %s", success, len(failures), failures[0])
	} else if warnings > 0 {
		m.notice("ok", "imported %d profile(s) with %d warning(s) — press Enter to activate, s to start, p to pick a node", success, warnings)
	} else {
		m.notice("ok", "imported %d profile(s) — press Enter to activate, s to start, p to pick a node", success)
	}
	return m, m.refreshCmd()
}

func (m *Model) handleUpdateResult(result updateResultMsg) (tea.Model, tea.Cmd) {
	m.updating = false
	m.busy = ""
	if result.err != nil {
		m.notice("error", "update failed: %s", result.err.Error())
		return m, m.refreshCmd()
	}
	if result.all {
		ok, failed := 0, 0
		var firstErr string
		for _, outcome := range result.outcomes {
			if outcome.Err != nil {
				failed++
				if firstErr == "" {
					firstErr = outcome.Err.Error()
				}
				continue
			}
			ok++
		}
		switch {
		case failed > 0 && ok > 0:
			m.notice("warn", "updated %d, failed %d: %s", ok, failed, firstErr)
		case failed > 0:
			m.notice("error", "all %d updates failed: %s", failed, firstErr)
		default:
			m.notice("ok", "updated %d profile(s)", ok)
		}
		return m, m.refreshCmd()
	}
	m.notice("ok", "updated %s to revision %d", result.profile.Label(), result.profile.Revision)
	return m, m.refreshCmd()
}

func (m *Model) runCommand(line string) tea.Cmd {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := fields[1:]

	switch name {
	case "help":
		m.setDialog(dialogs.NewHelp())
	case "refresh":
		return m.refreshCmd()
	case "quit", "exit":
		return tea.Quit
	case "logs":
		m.setPage(pages.IDLogs)
	case "doctor":
		return m.cmdDoctor()
	case "core":
		if len(args) < 1 {
			m.notice("warn", "usage: /core mihomo|sing-box")
			return nil
		}
		kind, err := model.ParseCoreKind(args[0])
		if err != nil {
			m.notice("error", "%s", err.Error())
			return nil
		}
		if kind == model.CoreMihomo {
			m.setPage(pages.IDMihomo)
		} else {
			m.setPage(pages.IDSingBox)
		}
		m.notice("ok", "switched to %s", kind.DisplayName())
	case "profile":
		sub := ""
		if len(args) > 0 {
			sub = strings.ToLower(args[0])
		}
		switch sub {
		case "add":
			m.setDialog(dialogs.NewAddProfile(m.core))
		case "list":
			if m.core == model.CoreSingBox {
				m.setPage(pages.IDSingBox)
			} else {
				m.setPage(pages.IDMihomo)
			}
		case "update":
			m.busy = "updating subscriptions"
			return m.cmdUpdateAll()
		case "use":
			m.notice("info", "press enter on a profile to activate it")
		default:
			m.notice("warn", "usage: /profile add|list|update|use")
		}
	case "proxy":
		sub := ""
		if len(args) > 0 {
			sub = strings.ToLower(args[0])
		}
		switch sub {
		case "test":
			return m.dispatch(pages.Action{Kind: pages.ActionTestProxies, Core: m.core})
		case "select":
			return m.dispatch(pages.Action{Kind: pages.ActionProxyPicker, Core: m.core})
		default:
			m.notice("warn", "usage: /proxy select|test")
		}
	case "system-proxy":
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "enable", "on":
				return m.dispatch(pages.Action{Kind: pages.ActionSetTrafficMode, Mode: network.ModeSystemProxy})
			case "disable", "off":
				return m.dispatch(pages.Action{Kind: pages.ActionSetTrafficMode, Mode: network.ModeOff})
			}
		}
		m.notice("warn", "usage: /system-proxy enable|disable")
	case "network":
		return m.cmdNetworkInfo()
	case "tun":
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "enable", "on":
				return m.dispatch(pages.Action{Kind: pages.ActionSetTrafficMode, Mode: network.ModeTUN})
			case "disable", "off":
				return m.dispatch(pages.Action{Kind: pages.ActionSetTrafficMode, Mode: network.ModeOff})
			}
		}
		m.notice("warn", "usage: /tun enable|disable")
	default:
		m.notice("warn", "unknown command /%s — press ? for help", name)
	}
	return nil
}

func (m *Model) autoUpdateCmd() tea.Cmd {
	if !m.settings.AutoUpdate || m.updating || m.importing {
		return nil
	}
	if time.Since(m.lastAutoCheck) < time.Minute {
		return nil
	}
	m.lastAutoCheck = time.Now()
	if len(m.app.Subs.Due(time.Now())) == 0 {
		return nil
	}
	m.updating = true
	m.busy = "auto updating subscriptions"
	return m.cmdUpdateAll()
}

func (m *Model) saveSettings() tea.Cmd {
	paths := m.app.Paths
	settings := m.settings
	return func() tea.Msg {
		return settingsSavedMsg{err: storage.SaveSettings(paths, settings)}
	}
}

func (m *Model) cmdImport(result dialogs.AddProfileResult) tea.Cmd {
	kind, err := model.ParseCoreKind(result.Core)
	if err != nil {
		return func() tea.Msg { return importResultMsg{err: err} }
	}
	m.importing = true
	if setter, ok := m.dialog.(dialogs.StatusSetter); ok {
		setter.SetBusy("fetching, parsing and validating…")
	}
	application := m.app
	request := subscription.ImportRequest{Core: kind, Name: result.Name, Input: result.Content}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		outcomes, err := application.Import(ctx, request)
		return importResultMsg{outcomes: outcomes, err: err}
	}
}

func (m *Model) cmdUseProfile(id string) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		profile, ok := application.Subs.Get(id)
		if !ok {
			return actionResultMsg{label: "activate profile", err: fmt.Errorf("profile not found")}
		}
		if _, err := application.Subs.SetActive(id); err != nil {
			return actionResultMsg{label: "activate " + profile.Label(), err: err}
		}
		if application.Cores.Status(ctx, profile.Core).Running {
			if err := application.RestartCore(ctx, profile.Core); err != nil {
				return actionResultMsg{label: "restart " + profile.Core.DisplayName() + " with " + profile.Label(), err: err}
			}
			return actionResultMsg{label: profile.Core.DisplayName() + " restarted with " + profile.Label()}
		}
		return actionResultMsg{label: "active profile: " + profile.Label()}
	}
}

func (m *Model) cmdUpdateProfile(id string) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		profile, err := application.Subs.Update(ctx, id)
		return updateResultMsg{profile: profile, err: err}
	}
}

func (m *Model) cmdUpdateAll() tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		outcomes := application.Subs.UpdateAll(ctx)
		return updateResultMsg{all: true, outcomes: outcomes}
	}
}

func (m *Model) cmdDeleteProfile(id string) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		profile, ok := application.Subs.Get(id)
		name := id
		if ok {
			name = profile.Label()
		}
		return deleteResultMsg{name: name, err: application.Subs.Delete(id)}
	}
}

func (m *Model) cmdStartCore(kind model.CoreKind) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := application.StartCore(ctx, kind); err != nil {
			return actionResultMsg{label: "start " + kind.DisplayName(), err: err}
		}
		return actionResultMsg{label: kind.DisplayName() + " started — press p to choose a node"}
	}
}

func (m *Model) cmdStopCore(kind model.CoreKind) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := application.Cores.Stop(ctx, kind); err != nil {
			return actionResultMsg{label: "stop " + kind.DisplayName(), err: err}
		}
		return actionResultMsg{label: kind.DisplayName() + " stopped"}
	}
}

func (m *Model) cmdRestartCore(kind model.CoreKind) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := application.RestartCore(ctx, kind); err != nil {
			return actionResultMsg{label: "restart " + kind.DisplayName(), err: err}
		}
		return actionResultMsg{label: kind.DisplayName() + " restarted"}
	}
}

func (m *Model) cmdSelectProxy(kind model.CoreKind, group, name string) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := application.Cores.SelectProxy(ctx, kind, group, name); err != nil {
			return actionResultMsg{label: "select " + name, err: err}
		}
		proxies, err := application.Cores.Proxies(ctx, kind)
		if err != nil {
			return actionResultMsg{label: group + " → " + name, err: fmt.Errorf("selection sent, but the core API could not be re-read: %w", err)}
		}
		for _, proxy := range proxies {
			if proxy.Name == group {
				if proxy.Now != name {
					return actionResultMsg{label: group + " → " + name, err: fmt.Errorf("core reports %q as the active selection", proxy.Now)}
				}
				break
			}
		}
		return actionResultMsg{label: group + " → " + name}
	}
}

func (m *Model) cmdVerifyProxy() tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return connectivityMsg{result: application.VerifyProxyConnectivity(ctx)}
	}
}

func (m *Model) cmdTestProxy(kind model.CoreKind, name string) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		delay, err := application.Cores.TestDelay(ctx, kind, name, 8*time.Second)
		if err != nil {
			return actionResultMsg{label: "test " + name, err: err}
		}
		if delay <= 0 {
			return actionResultMsg{label: "test " + name, err: fmt.Errorf("node did not respond")}
		}
		return actionResultMsg{label: fmt.Sprintf("test %s: %d ms", name, delay)}
	}
}

func (m *Model) cmdTestProxies(kind model.CoreKind) tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		proxies, err := application.Cores.Proxies(ctx, kind)
		if err != nil {
			return testResultMsg{err: err}
		}
		names := make([]string, 0, len(proxies))
		for _, proxy := range proxies {
			if !proxy.Group {
				names = append(names, proxy.Name)
			}
		}
		if len(names) == 0 {
			return testResultMsg{err: fmt.Errorf("no proxy nodes reported")}
		}

		type sample struct {
			name  string
			delay int
			err   error
		}
		results := make(chan sample, len(names))
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for _, name := range names {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				delay, err := application.Cores.TestDelay(ctx, kind, name, 5*time.Second)
				results <- sample{name: name, delay: delay, err: err}
			}(name)
		}
		wg.Wait()
		close(results)

		ok, failed, best, bestName := 0, 0, 0, ""
		for result := range results {
			if result.err != nil || result.delay <= 0 {
				failed++
				continue
			}
			ok++
			if best == 0 || result.delay < best {
				best = result.delay
				bestName = result.name
			}
		}
		if ok == 0 {
			return testResultMsg{failed: failed, err: fmt.Errorf("no node responded (%d failed)", failed)}
		}
		return testResultMsg{ok: ok, failed: failed, best: fmt.Sprintf("%s %dms", bestName, best)}
	}
}

// requestTrafficMode stages a traffic-mode change behind an explicit
// confirmation. No network operation runs until the dialog is accepted, and a
// second request is ignored while the previous one is in flight.
func (m *Model) effectiveTrafficMode() network.Mode {
	if m.tun.Enabled {
		return network.ModeTUN
	}
	if m.network.Desktop.Enabled {
		return network.ModeSystemProxy
	}
	return network.Mode(m.settings.TrafficMode)
}

func (m *Model) requestTrafficMode(mode network.Mode) tea.Cmd {
	if m.busy != "" || m.dialog != nil {
		return nil
	}
	from := m.effectiveTrafficMode()
	if from == mode && mode != network.ModeTUN {
		m.notice("info", "traffic mode is already %s", mode.Display())
		return nil
	}
	m.confirm = &pages.Action{Kind: pages.ActionSetTrafficMode, Mode: mode}
	m.dialog = dialogs.NewConfirm(m.trafficConfirmMessage(from, mode))
	return nil
}

func (m *Model) trafficConfirmMessage(from, to network.Mode) string {
	lines := []string{fmt.Sprintf("Change traffic mode: %s → %s?", from.Display(), to.Display())}
	switch to {
	case network.ModeTUN:
		lines = append(lines, "TUN will take over the default route and DNS until it is disabled.")
		if m.tun.Enabled {
			lines = append(lines, "A TUN session is already active and will be restarted.")
		}
	case network.ModeSystemProxy:
		lines = append(lines, fmt.Sprintf("System proxy target: %s. It is enabled only after the core process and this port are verified.", m.app.OwnProxyAddress()))
		if m.tun.Enabled {
			lines = append(lines, "The active TUN session will be released first (modes are mutually exclusive).")
		}
	case network.ModeOff, network.ModeManual:
		lines = append(lines, "TUN and the system proxy will be released and the original settings restored.")
	}
	return strings.Join(lines, " ")
}

func (m *Model) cmdSetTrafficMode(mode network.Mode) tea.Cmd {
	application := m.app
	settings := app.TunSettingsFrom(m.settings)
	kind := m.core
	from := m.effectiveTrafficMode()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		result, err := application.ApplyTrafficMode(ctx, mode, kind, settings)
		label := "traffic mode: " + from.Display() + " → " + mode.Display()
		if err != nil {
			msg := actionResultMsg{label: label, err: err, warnings: result.Warnings}
			if mode == network.ModeTUN && result.TUN.Report.Level != "" {
				report := result.TUN.Report
				msg.report = &report
			}
			return msg
		}
		if mode == network.ModeTUN {
			if result.TUN.Status.Interface != "" {
				label += " (" + result.TUN.Status.Interface + ")"
			}
		} else if result.Network.Applied {
			label += " (" + result.Network.State.Address() + ")"
		} else if result.Network.Restored {
			label += " (restored)"
		}
		return actionResultMsg{label: label, mode: mode, modeChanged: true, warnings: result.Warnings}
	}
}

// reapplyTunIfActive restarts the core with the updated TUN settings when TUN
// is currently enabled, and only saves the settings otherwise.
func (m *Model) reapplyTunIfActive() tea.Cmd {
	if m.tun.Enabled {
		m.busy = "applying TUN settings"
		return tea.Batch(m.cmdSetTrafficMode(network.ModeTUN), m.saveSettings())
	}
	return m.saveSettings()
}

func (m *Model) cmdTUNPreflight() tea.Cmd {
	application := m.app
	settings := app.TunSettingsFrom(m.settings)
	kind := m.core
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		report, err := application.TUNPreflight(ctx, kind, settings)
		return preflightMsg{report: report, err: err}
	}
}

func (m *Model) cmdRecoverTUN() tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := application.RecoverTUN(ctx)
		return recoveryMsg{result: result, err: err}
	}
}

func nextTunStack(current string) string {
	switch strings.ToLower(current) {
	case "mixed":
		return "system"
	case "system":
		return "gvisor"
	default:
		return "mixed"
	}
}

func nextTunMTU(current int) int {
	switch current {
	case 9000:
		return 1500
	case 1500:
		return 1280
	default:
		return 9000
	}
}

func (m *Model) cmdNetworkInfo() tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		report, err := application.NetworkReport(ctx)
		if err != nil {
			return actionResultMsg{label: "network status", err: err}
		}
		lines := []string{
			"platform: " + report.Capabilities.Platform,
			fmt.Sprintf("supported: %v (%s)", report.Capabilities.Supported, report.Capabilities.Method),
			"mode: " + m.settings.TrafficMode,
		}
		if report.Desktop.Enabled {
			lines = append(lines, "desktop proxy: on "+report.Desktop.Address())
		} else {
			lines = append(lines, "desktop proxy: off")
		}
		if report.Environment.Detected() {
			lines = append(lines, "shell proxy: "+report.Environment.HTTP+" (inherited, not managed)")
		}
		if report.BackupActive {
			lines = append(lines, "restore point: available")
		}
		lines = append(lines, report.Notes...)
		return doctorResultMsg{lines: lines}
	}
}

func (m *Model) cmdDoctor() tea.Cmd {
	application := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return doctorResultMsg{lines: application.Doctor(ctx)}
	}
}
