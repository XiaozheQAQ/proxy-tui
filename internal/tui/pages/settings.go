package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/tui/components"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Settings exposes persisted configuration and storage locations.
type Settings struct {
	data     SettingsData
	selected int
}

// NewSettings builds the settings page.
func NewSettings() *Settings { return &Settings{} }

// ID implements Page.
func (p *Settings) ID() ID { return IDSettings }

// Title implements Page.
func (p *Settings) Title() string { return "Settings" }

// settingsRows is the number of interactive rows.
const settingsRows = 11

// Update implements Page.
func (p *Settings) Update(msg tea.Msg) (Page, *Action, tea.Cmd) {
	switch typed := msg.(type) {
	case DataMsg:
		p.data = typed.Settings
		return p, nil, nil
	case tea.KeyMsg:
		switch typed.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j":
			if p.selected < settingsRows-1 {
				p.selected++
			}
		case "enter":
			switch p.selected {
			case 0:
				return p, &Action{Kind: ActionToggleTheme}, nil
			case 1:
				return p, &Action{Kind: ActionToggleAutoUpdate}, nil
			case 2:
				return p, &Action{Kind: ActionCycleInterval}, nil
			case 3:
				return p, &Action{Kind: ActionToggleConfirmQuit}, nil
			case 4:
				switch network.Mode(p.data.Settings.TrafficMode) {
				case network.ModeOff:
					return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeSystemProxy}, nil
				case network.ModeSystemProxy:
					return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeTUN}, nil
				case network.ModeTUN:
					return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeManual}, nil
				default:
					return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeOff}, nil
				}
			case 5:
				return p, &Action{Kind: ActionCycleTunStack}, nil
			case 6:
				return p, &Action{Kind: ActionCycleTunMTU}, nil
			case 7:
				return p, &Action{Kind: ActionToggleTunDNS}, nil
			case 8:
				return p, &Action{Kind: ActionToggleTunIPv6}, nil
			case 9:
				if p.data.TUN.Enabled {
					return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeOff}, nil
				}
				return p, &Action{Kind: ActionSetTrafficMode, Mode: network.ModeTUN}, nil
			case 10:
				return p, &Action{Kind: ActionRunTUNPreflight}, nil
			}
		}
	}
	return p, nil, nil
}

// Hints implements Page.
func (p *Settings) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "↑↓", Label: "select"},
		{Key: "enter", Label: "toggle"},
	}
}

// View implements Page.
func (p *Settings) View(theme layout.Theme, width, height int) string {
	settings := p.data.Settings
	mode := network.Mode(settings.TrafficMode)
	status := p.data.TUN
	tunValue := "disabled"
	if status.Enabled {
		tunValue = "enabled"
		if status.Interface != "" {
			tunValue += " · " + status.Interface
		}
	}
	rows := []struct {
		label string
		value string
	}{
		{"Theme", settings.Theme},
		{"Auto update profiles", boolLabel(settings.AutoUpdate)},
		{"Update interval", format.DurationInterval(settings.UpdateIntervalSeconds)},
		{"Confirm quit", boolLabel(settings.ConfirmQuit)},
		{"Traffic mode", modeDisplay(mode) + "  (enter to change)"},
		{"TUN stack", settings.TunStack + "  (enter to change)"},
		{"TUN MTU", numberString(settings.TunMTU) + "  (enter to change)"},
		{"TUN DNS hijack", boolLabel(settings.TunDNSHijack)},
		{"TUN IPv6", boolLabel(settings.TunIPv6)},
		{"TUN", tunValue + "  (enter to toggle)"},
		{"Run TUN preflight", "enter"},
	}
	var body strings.Builder
	for i, row := range rows {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(theme.Text)
		if i == p.selected {
			cursor = "› "
			style = lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
		}
		line := fmt.Sprintf("%s%s  %s", cursor, format.PadRight(row.label, 22), row.value)
		body.WriteString(style.Render(line) + "\n")
	}
	body.WriteString("\n")
	body.WriteString(p.tunStatusBlock(theme, width))
	body.WriteString("\n")
	networkRows := p.networkRows()
	body.WriteString(components.KeyValueLines(theme, networkRows, width-6))
	body.WriteString("\n")
	body.WriteString(components.KeyValueLines(theme, [][2]string{
		{"Config dir", p.data.Paths.Config},
		{"Data dir", p.data.Paths.Data},
		{"State dir", p.data.Paths.State},
		{"Cores", strings.Join(p.data.CoreDirs, ", ")},
		{"Version", p.data.Version},
	}, width-6))

	cardHeight := height
	if cardHeight > 30 {
		cardHeight = 30
	}
	card := components.Card(theme, "SETTINGS", body.String(), width, cardHeight)
	if height > cardHeight {
		card += "\n" + lipgloss.NewStyle().Foreground(theme.Muted).
			Render("  Edit config.json/keymap.json directly for advanced options.")
	}
	return layout.Fit(card, width, height)
}

// tunStatusBlock renders the TUN lifecycle state and recovery hints.
func (p *Settings) tunStatusBlock(theme layout.Theme, width int) string {
	status := p.data.TUN
	level := lipgloss.NewStyle().Foreground(theme.Muted).Render("inactive")
	switch status.Level {
	case "READY":
		level = lipgloss.NewStyle().Foreground(theme.Success).Render("ready")
	case "WARNING":
		level = lipgloss.NewStyle().Foreground(theme.Warning).Render("warning")
	case "BLOCKED":
		level = lipgloss.NewStyle().Foreground(theme.Error).Render("blocked")
	case "UNKNOWN":
		level = lipgloss.NewStyle().Foreground(theme.Warning).Render("unknown")
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("TUN STATUS") + "  " + level,
	}
	pairs := [][2]string{}
	if status.Enabled {
		pairs = append(pairs, [2]string{"Core", status.Core.DisplayName()})
		pairs = append(pairs, [2]string{"Interface", status.Interface})
		pairs = append(pairs, [2]string{"Outbound", status.Outbound})
		pairs = append(pairs, [2]string{"DNS", status.DNSMode})
		pairs = append(pairs, [2]string{"Routing", status.Routing})
	}
	if status.Detail != "" {
		pairs = append(pairs, [2]string{"Detail", status.Detail})
	}
	for _, leftover := range status.Leftover {
		pairs = append(pairs, [2]string{"Leftover", leftover})
	}
	if len(pairs) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).
			Render("  TUN is provided by the core; enable it from the TUN row or traffic mode"))
	} else {
		lines = append(lines, components.KeyValueLines(theme, pairs, width-6))
	}
	return strings.Join(lines, "\n")
}

func numberString(value int) string {
	return fmt.Sprintf("%d", value)
}

func boolLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func modeDisplay(mode network.Mode) string {
	switch mode {
	case network.ModeOff, network.ModeSystemProxy, network.ModeTUN, network.ModeManual:
		return mode.Display()
	default:
		return "Off"
	}
}

// networkRows summarises the desktop proxy, the shell proxy and TUN support.
func (p *Settings) networkRows() [][2]string {
	report := p.data.Network
	rows := [][2]string{}
	if report.Capabilities.Supported {
		desktop := "off"
		if report.Desktop.Enabled {
			desktop = "on " + report.Desktop.Address()
		}
		rows = append(rows, [2]string{"Desktop proxy", desktop})
	} else {
		reason := report.Capabilities.Reason
		if reason == "" {
			reason = "unsupported"
		}
		rows = append(rows, [2]string{"Desktop proxy", "unavailable (" + reason + ")"})
	}
	if report.Environment.Detected() {
		shell := report.Environment.HTTP
		if shell == "" {
			shell = report.Environment.HTTPS
		}
		if shell == "" {
			shell = report.Environment.All
		}
		rows = append(rows, [2]string{"Shell proxy", shell + " (inherited, not managed)"})
	} else {
		rows = append(rows, [2]string{"Shell proxy", "not set in this shell"})
	}
	rows = append(rows, [2]string{"TUN", "provided by the core (Phase 3C)"})
	if report.BackupActive {
		rows = append(rows, [2]string{"Restore point", format.Relative(report.BackupAt) + " (original desktop proxy kept)"})
	}
	return rows
}
