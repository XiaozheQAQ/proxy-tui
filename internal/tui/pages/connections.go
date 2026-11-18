package pages

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/components"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Connections shows the live connection table.
type Connections struct {
	data     ConnectionsData
	selected int
	offset   int
}

// NewConnections builds the connections page.
func NewConnections() *Connections { return &Connections{} }

// ID implements Page.
func (p *Connections) ID() ID { return IDConnections }

// Title implements Page.
func (p *Connections) Title() string { return "Connections" }

// Update implements Page.
func (p *Connections) Update(msg tea.Msg) (Page, *Action, tea.Cmd) {
	switch typed := msg.(type) {
	case DataMsg:
		p.data = typed.Connections
		if p.selected >= len(p.data.Connections) {
			p.selected = len(p.data.Connections) - 1
		}
		if p.selected < 0 {
			p.selected = 0
		}
		return p, nil, nil
	case tea.KeyMsg:
		switch typed.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j":
			if p.selected < len(p.data.Connections)-1 {
				p.selected++
			}
		case "home", "g":
			p.selected = 0
		case "end", "G":
			if len(p.data.Connections) > 0 {
				p.selected = len(p.data.Connections) - 1
			}
		case "r":
			return p, &Action{Kind: ActionRefresh}, nil
		}
	}
	return p, nil, nil
}

// Hints implements Page.
func (p *Connections) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "↑↓", Label: "scroll"},
		{Key: "r", Label: "refresh"},
	}
}

// View implements Page.
func (p *Connections) View(theme layout.Theme, width, height int) string {
	if !p.data.Running {
		return layout.Center(components.Empty(theme, "start the core to see connections"), width, height)
	}
	if !p.data.Supported {
		return layout.Center(lipgloss.NewStyle().Foreground(theme.Warning).
			Render("this core build does not expose connections"), width, height)
	}
	if p.data.Err != "" {
		return layout.Center(lipgloss.NewStyle().Foreground(theme.Warning).
			Render(format.Truncate(p.data.Err, width-4)), width, height)
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("LIVE CONNECTIONS")
	summary := lipgloss.NewStyle().Foreground(theme.Muted).Render(fmt.Sprintf(
		"  %d active   ↑ %s/s   ↓ %s/s   total %s",
		len(p.data.Connections),
		format.Bytes(p.data.Traffic.UpRate),
		format.Bytes(p.data.Traffic.DownRate),
		format.Bytes(p.data.Traffic.UpTotal+p.data.Traffic.DownTotal),
	))

	rows := make([][]string, 0, len(p.data.Connections))
	for _, connection := range p.data.Connections {
		host := connection.Host
		if host == "" {
			host = connection.DestinationIP
		}
		chain := strings.Join(connection.Chain, " → ")
		rows = append(rows, []string{
			format.Truncate(host, width/3),
			connection.Type,
			connection.Network,
			format.Truncate(chain, 22),
			format.Bytes(connection.Upload),
			format.Bytes(connection.Download),
			age(connection.Start),
		})
	}
	nameWidth := width/3 - 2
	if nameWidth < 14 {
		nameWidth = 14
	}
	widths := []int{nameWidth, 8, 6, 22, 8, 9, 6}
	table := components.Table(theme, []string{"HOST", "TYPE", "NET", "CHAIN", "UP", "DOWN", "AGE"},
		rows, widths, p.selected, p.offset, height-3, width)

	return layout.Fit(title+"\n"+summary+"\n"+table, width, height)
}

func age(start time.Time) string {
	if start.IsZero() {
		return "—"
	}
	delta := time.Since(start)
	switch {
	case delta < time.Minute:
		return fmt.Sprintf("%ds", int(delta.Seconds()))
	case delta < time.Hour:
		return fmt.Sprintf("%dm", int(delta.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(delta.Hours()))
	}
}
