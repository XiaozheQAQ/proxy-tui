package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/components"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// CorePage manages one proxy core and its profiles.
type CorePage struct {
	id       ID
	kind     model.CoreKind
	data     CoreData
	selected int
}

// NewCorePage builds the management page for a core.
func NewCorePage(kind model.CoreKind) *CorePage {
	id := IDMihomo
	if kind == model.CoreSingBox {
		id = IDSingBox
	}
	return &CorePage{id: id, kind: kind}
}

// ID implements Page.
func (p *CorePage) ID() ID { return p.id }

// Title implements Page.
func (p *CorePage) Title() string { return TitleOf(p.id) }

// Update implements Page.
func (p *CorePage) Update(msg tea.Msg) (Page, *Action, tea.Cmd) {
	switch typed := msg.(type) {
	case DataMsg:
		if data, ok := typed.Cores[p.kind]; ok {
			p.data = data
			if p.selected >= len(p.data.Profiles) {
				p.selected = len(p.data.Profiles) - 1
			}
			if p.selected < 0 {
				p.selected = 0
			}
		}
		return p, nil, nil
	case tea.KeyMsg:
		switch typed.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j":
			if p.selected < len(p.data.Profiles)-1 {
				p.selected++
			}
		case "home", "g":
			p.selected = 0
		case "end", "G":
			if len(p.data.Profiles) > 0 {
				p.selected = len(p.data.Profiles) - 1
			}
		case "enter":
			if profile, ok := p.current(); ok {
				return p, &Action{Kind: ActionUseProfile, Core: p.kind, ProfileID: profile.ID}, nil
			}
		case "s":
			return p, &Action{Kind: ActionStartCore, Core: p.kind}, nil
		case "x":
			return p, &Action{Kind: ActionStopCore, Core: p.kind}, nil
		case "r":
			return p, &Action{Kind: ActionRestartCore, Core: p.kind}, nil
		case "t":
			return p, &Action{Kind: ActionTestProxies, Core: p.kind}, nil
		case "p":
			return p, &Action{Kind: ActionProxyPicker, Core: p.kind}, nil
		case "v":
			return p, &Action{Kind: ActionVerifyProxy, Core: p.kind}, nil
		}
	}
	return p, nil, nil
}

// Hints implements Page.
func (p *CorePage) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "enter", Label: "activate"},
		{Key: "s", Label: "start"},
		{Key: "x", Label: "stop"},
		{Key: "u", Label: "update"},
		{Key: "d", Label: "delete"},
		{Key: "p", Label: "proxies"},
		{Key: "v", Label: "verify"},
		{Key: "A", Label: "add"},
	}
}

// SelectedProfile exposes the highlighted profile to the root model.
func (p *CorePage) SelectedProfile() (model.Profile, bool) { return p.current() }

func (p *CorePage) current() (model.Profile, bool) {
	if p.selected >= 0 && p.selected < len(p.data.Profiles) {
		return p.data.Profiles[p.selected], true
	}
	return model.Profile{}, false
}

// View implements Page.
func (p *CorePage) View(theme layout.Theme, width, height int) string {
	cardHeight := 8
	if height < 18 {
		cardHeight = 6
	}
	detailHeight := 0
	if _, ok := p.current(); ok {
		detailHeight = 4
	}
	tableHeight := height - cardHeight - detailHeight
	if tableHeight < 3 {
		tableHeight = 3
	}

	coreCard := components.Card(theme, "CORE", p.coreBody(theme, width-6), width, cardHeight)
	table := p.profileTable(theme, width, maxInt(tableHeight-1, 1))
	parts := []string{coreCard, table}
	if detailHeight > 0 {
		parts = append(parts, p.detailBody(theme, width))
	}
	return layout.Fit(strings.Join(parts, "\n"), width, height)
}

func (p *CorePage) coreBody(theme layout.Theme, width int) string {
	status := p.data.Status
	tone := "warn"
	switch {
	case status.Running:
		tone = "ok"
	case status.Installed:
		tone = "accent"
	}
	api := status.APIAddr
	if api == "" {
		api = "—"
	}
	pairs := [][2]string{
		{"Status", components.Badge(theme, status.Label(), tone)},
		{"Version", dash(status.Version)},
		{"API", api},
		{"Profiles", fmt.Sprintf("%d", len(p.data.Profiles))},
		{"Proxies", p.proxySummary()},
	}
	body := components.KeyValueLines(theme, pairs, width)
	if status.LastError != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(theme.Error).
			Render("  "+format.Truncate(status.LastError, width-4))
	}
	return body
}

func (p *CorePage) proxySummary() string {
	if !p.data.Status.Running {
		return "core stopped"
	}
	if p.data.ProxiesErr != "" {
		return "unavailable"
	}
	return fmt.Sprintf("%d groups / %d nodes", CountGroups(p.data.Proxies), len(p.data.Proxies)-CountGroups(p.data.Proxies))
}

func (p *CorePage) profileTable(theme layout.Theme, width, height int) string {
	nameWidth := width - 44
	if nameWidth < 12 {
		nameWidth = 12
	}
	widths := []int{nameWidth, 12, 5, 5, 9}
	rows := make([][]string, 0, len(p.data.Profiles))
	for _, profile := range p.data.Profiles {
		state := "ready"
		switch {
		case profile.ID == p.data.ActiveID && profile.Activatable:
			state = "active"
		case !profile.Activatable:
			state = "stored"
		}
		source := profile.Format.DisplayName()
		rows = append(rows, []string{
			profile.Label(),
			source,
			fmt.Sprintf("%d", profile.NodeCount),
			fmt.Sprintf("%d", profile.Revision),
			state,
		})
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("PROFILES")
	table := components.Table(theme, []string{"NAME", "FORMAT", "NODES", "REV", "STATE"}, rows, widths,
		p.selected, maxInt(p.selected-height+1, 0), maxInt(height-1, 1), width)
	return layout.Fit(title+"\n"+table, width, 0)
}

func (p *CorePage) detailBody(theme layout.Theme, width int) string {
	profile, ok := p.current()
	if !ok {
		return ""
	}
	lines := []string{
		components.LabelValue(theme, "Profile", profile.Label()),
		components.LabelValue(theme, "Updated", format.Relative(profile.UpdatedAt)+"  "+format.Bytes(profile.SizeBytes)),
	}
	if profile.LastUpdate != nil && !profile.LastUpdate.OK {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Error).
			Render("  last update failed: "+format.Truncate(profile.LastUpdate.Error, width-24)))
	} else if len(profile.Warnings) > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Warning).
			Render("  "+format.Truncate(profile.Warnings[0], width-4)))
	}
	return layout.Fit(strings.Join(lines, "\n"), width, 3)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
