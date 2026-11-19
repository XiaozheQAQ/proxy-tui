package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/components"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Overview is the landing page.
type Overview struct {
	data OverviewData
}

// NewOverview builds the overview page.
func NewOverview() *Overview { return &Overview{} }

// ID implements Page.
func (p *Overview) ID() ID { return IDOverview }

// Title implements Page.
func (p *Overview) Title() string { return "Overview" }

// Update implements Page.
func (p *Overview) Update(msg tea.Msg) (Page, *Action, tea.Cmd) {
	switch typed := msg.(type) {
	case DataMsg:
		p.data = typed.Overview
		return p, nil, nil
	case tea.KeyMsg:
		switch typed.String() {
		case "enter":
			if p.data.Status.Running {
				return p, &Action{Kind: ActionStopCore, Core: p.data.Core}, nil
			}
			return p, &Action{Kind: ActionStartCore, Core: p.data.Core}, nil
		case "r":
			return p, &Action{Kind: ActionRestartCore, Core: p.data.Core}, nil
		case "p":
			return p, &Action{Kind: ActionProxyPicker, Core: p.data.Core}, nil
		case "t":
			return p, &Action{Kind: ActionTestProxies, Core: p.data.Core}, nil
		case "v":
			return p, &Action{Kind: ActionVerifyProxy, Core: p.data.Core}, nil
		}
	}
	return p, nil, nil
}

// Hints implements Page.
func (p *Overview) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "enter", Label: "start/stop"},
		{Key: "r", Label: "restart"},
		{Key: "p", Label: "nodes"},
		{Key: "t", Label: "test"},
		{Key: "v", Label: "verify"},
		{Key: "A", Label: "add"},
	}
}

// View implements Page.
func (p *Overview) View(theme layout.Theme, width, height int) string {
	leftWidth := width * 3 / 5
	rightWidth := width - leftWidth
	topHeight := height / 2
	if topHeight < 8 {
		topHeight = 8
	}
	if topHeight > height-6 {
		topHeight = height - 6
	}
	if topHeight < 5 {
		topHeight = 5
	}
	bottomHeight := height - topHeight

	rightColumn := rightWidth - 1
	coreCard := components.Card(theme, "CURRENT CORE", p.coreBody(theme, leftWidth-7), leftWidth-1, topHeight)
	trafficCard := components.Card(theme, "THROUGHPUT", p.trafficBody(theme, rightColumn-6), rightColumn-1, topHeight)
	profileCard := components.Card(theme, "ACTIVE PROFILE", p.profileBody(theme, leftWidth-7), leftWidth-1, bottomHeight)
	proxyCard := components.Card(theme, "ACTIVE PROXY", p.proxyBody(theme, rightColumn-6), rightColumn-1, bottomHeight)

	row1 := layout.JoinColumns(coreCard, trafficCard, leftWidth, rightColumn, topHeight)
	row2 := layout.JoinColumns(profileCard, proxyCard, leftWidth, rightColumn, bottomHeight)
	return row1 + "\n" + row2
}

func (p *Overview) coreBody(theme layout.Theme, width int) string {
	status := p.data.Status
	tone := "warn"
	switch {
	case status.Running:
		tone = "ok"
	case status.Installed:
		tone = "accent"
	}
	desktop := "unavailable"
	if p.data.SystemProxy.Known {
		desktop = "off"
		if p.data.SystemProxy.Enabled {
			desktop = "on " + p.data.SystemProxy.Detail
		}
	} else if p.data.SystemProxy.Detail != "" {
		desktop = p.data.SystemProxy.Detail
	}
	pairs := [][2]string{
		{"Core", p.data.Core.DisplayName()},
		{"State", components.Badge(theme, status.Label(), tone)},
		{"Version", dash(status.Version)},
		{"Profiles", fmt.Sprintf("%d", p.data.ProfileCount)},
		{"Supervisor", p.data.Supervisor},
		{"Traffic mode", p.data.TrafficMode},
		{"Proxy check", proxyCheckLabel(p.data.ProxyCheck)},
		{"Desktop proxy", desktop},
	}
	body := components.KeyValueLines(theme, pairs, width)
	if status.LastError != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(theme.Error).
			Render("  "+format.Truncate(status.LastError, width-4))
	}
	return body
}

func (p *Overview) trafficBody(theme layout.Theme, width int) string {
	traffic := p.data.Traffic
	lines := []string{
		components.LabelValue(theme, "up", format.Speed(traffic.UpRate)),
		components.LabelValue(theme, "down", format.Speed(traffic.DownRate)),
		components.LabelValue(theme, "total", format.Bytes(traffic.UpTotal+traffic.DownTotal)),
		components.LabelValue(theme, "conns", fmt.Sprintf("%d", p.data.Connections)),
	}
	if !p.data.Status.Running {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).Render("  start core for stats"))
	}
	if p.data.ConnectionsErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Warning).
			Render("  "+format.Truncate(p.data.ConnectionsErr, width-4)))
	}
	return strings.Join(lines, "\n")
}

func (p *Overview) profileBody(theme layout.Theme, width int) string {
	if !p.data.HasActive {
		return components.Empty(theme, "no active profile — press A to paste a subscription")
	}
	profile := p.data.Active
	source := "pasted content"
	if profile.Source.URL != "" {
		source = format.Truncate(profile.Source.URL, width-16)
	}
	pairs := [][2]string{
		{"Name", profile.Label()},
		{"Format", profile.Format.DisplayName()},
		{"Nodes", fmt.Sprintf("%d", profile.NodeCount)},
		{"Revision", fmt.Sprintf("%d", profile.Revision)},
		{"Source", source},
	}
	body := components.KeyValueLines(theme, pairs, width)
	if !profile.Activatable && len(profile.Warnings) > 0 {
		body += "\n" + lipgloss.NewStyle().Foreground(theme.Warning).
			Render("  "+format.Truncate(profile.Warnings[len(profile.Warnings)-1], width-4))
	}
	return body
}

func proxyCheckLabel(check ProxyCheck) string {
	if !check.Attempted {
		return "not checked (press v)"
	}
	if !check.OK {
		return "FAILED"
	}
	return fmt.Sprintf("ok · %s · %d ms", check.ExitIP, check.LatencyMS)
}

func (p *Overview) proxyBody(theme layout.Theme, width int) string {
	if !p.data.Status.Running {
		return components.Empty(theme, "core is not running")
	}
	if p.data.ProxiesErr != "" {
		return components.Empty(theme, format.Truncate(p.data.ProxiesErr, width-4))
	}
	group, member, ok := ActiveSelection(p.data.Proxies)
	if !ok {
		return components.Empty(theme, "no proxy groups reported")
	}
	lines := []string{
		components.LabelValue(theme, "group", group),
		components.LabelValue(theme, "node", member),
		components.LabelValue(theme, "groups", fmt.Sprintf("%d", CountGroups(p.data.Proxies))),
		lipgloss.NewStyle().Foreground(theme.Muted).Render("  press p to switch node"),
	}
	return strings.Join(lines, "\n")
}

// ActiveSelection finds the most relevant proxy group and its selection.
func ActiveSelection(proxies []core.Proxy) (group, member string, ok bool) {
	preferred := []string{"PROXY", "Proxy", "GLOBAL", "Global", "proxy", "global"}
	for _, wanted := range preferred {
		for _, proxy := range proxies {
			if proxy.Group && proxy.Name == wanted && proxy.Now != "" {
				return proxy.Name, proxy.Now, true
			}
		}
	}
	for _, proxy := range proxies {
		if proxy.Group && proxy.Now != "" {
			return proxy.Name, proxy.Now, true
		}
	}
	return "", "", false
}

// CountGroups counts proxy groups in a proxy list.
func CountGroups(proxies []core.Proxy) int {
	count := 0
	for _, proxy := range proxies {
		if proxy.Group {
			count++
		}
	}
	return count
}

func dash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
