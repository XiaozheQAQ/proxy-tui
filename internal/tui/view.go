package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
)

// View implements tea.Model.
func (m *Model) View() string {
	theme := m.theme()
	if !m.ready {
		return "starting proxy-tui…"
	}
	geometry := layout.Compute(m.width, m.height)
	if geometry.TooSmall {
		return layout.TooSmall(theme, m.width, m.height)
	}

	status := m.statusOf(m.core)
	apiReady := status.Running && m.live.proxyErr == ""
	header := layout.Header(theme, m.coreLabel(), status.Running, apiReady,
		m.liveTraffic().UpRate, m.liveTraffic().DownRate, geometry.Width)

	navLabels := make([]string, 0, len(pages.IDs()))
	for _, id := range pages.IDs() {
		navLabels = append(navLabels, pages.TitleOf(id))
	}
	sidebar := layout.Sidebar(theme, navLabels, m.navIndex, geometry.SidebarWidth, geometry.ContentHeight)

	hints := m.pageHints()
	var mainBlock string
	if m.dialog != nil {
		dialogWidth := geometry.ContentWidth - 4
		if dialogWidth > 88 {
			dialogWidth = 88
		}
		if dialogWidth < 30 {
			dialogWidth = 30
		}
		mainBlock = layout.Center(m.dialog.View(theme, dialogWidth), geometry.ContentWidth, geometry.ContentHeight)
		hints = m.dialog.Hints()
	} else {
		mainBlock = layout.Fit(m.currentPage().View(theme, geometry.ContentWidth, geometry.ContentHeight),
			geometry.ContentWidth, geometry.ContentHeight)
	}

	body := layout.JoinColumns(sidebar, mainBlock, geometry.SidebarWidth, geometry.ContentWidth, geometry.ContentHeight)
	footer := layout.Footer(theme, hints, m.statusLine(theme, geometry.Width), geometry.Width)
	return layout.Fit(header+"\n"+body+"\n"+footer, geometry.Width, geometry.Height)
}

func (m *Model) coreLabel() string {
	status := m.statusOf(m.core)
	label := m.core.DisplayName()
	if status.Version != "" {
		label += " " + status.Version
	}
	return label
}

// statusLine renders the transient message and the summary.
func (m *Model) statusLine(theme layout.Theme, width int) string {
	left := ""
	level := "muted"
	switch {
	case m.busy != "":
		left = "… " + m.busy
		level = "warn"
	case m.toast.text != "" && time.Since(m.toast.at) < 8*time.Second:
		left = m.toast.text
		level = m.toast.level
	}

	active, hasActive := m.app.Subs.ActiveFor(m.core)
	right := fmt.Sprintf("profiles %d", len(m.profiles))
	if hasActive {
		right = fmt.Sprintf("%s · %d nodes · profiles %d", active.Label(), active.NodeCount, len(m.profiles))
	}

	leftStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	switch level {
	case "ok":
		leftStyle = lipgloss.NewStyle().Foreground(theme.Success)
	case "warn":
		leftStyle = lipgloss.NewStyle().Foreground(theme.Warning)
	case "error":
		leftStyle = lipgloss.NewStyle().Foreground(theme.Error)
	case "info":
		leftStyle = lipgloss.NewStyle().Foreground(theme.Accent)
	}
	rightStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	rightWidth := lipgloss.Width(right)
	leftWidth := width - rightWidth - 2
	if leftWidth < 0 {
		leftWidth = 0
	}
	left = format.Truncate(left, leftWidth)
	gap := width - lipgloss.Width(left) - rightWidth
	if gap < 1 {
		gap = 1
	}
	line := leftStyle.Render(left) + strings.Repeat(" ", gap) + rightStyle.Render(right)
	return layout.Fit(line, width, 1)
}
