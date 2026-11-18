package pages

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Logs shows the application and core logs.
type Logs struct {
	entries []logging.Entry
	filter  string
	scroll  int
}

// NewLogs builds the logs page.
func NewLogs() *Logs { return &Logs{filter: ""} }

// ID implements Page.
func (p *Logs) ID() ID { return IDLogs }

// Title implements Page.
func (p *Logs) Title() string { return "Logs" }

// Update implements Page.
func (p *Logs) Update(msg tea.Msg) (Page, *Action, tea.Cmd) {
	switch typed := msg.(type) {
	case DataMsg:
		p.entries = typed.Logs.Entries
		return p, nil, nil
	case tea.KeyMsg:
		switch typed.String() {
		case "up", "k":
			p.scroll++
		case "down", "j":
			if p.scroll > 0 {
				p.scroll--
			}
		case "pgup", "ctrl+u":
			p.scroll += 10
		case "pgdown", "ctrl+d":
			p.scroll -= 10
			if p.scroll < 0 {
				p.scroll = 0
			}
		case "end", "G":
			p.scroll = 0
		case "f":
			switch p.filter {
			case "":
				p.filter = "warn"
			case "warn":
				p.filter = "error"
			default:
				p.filter = ""
			}
			p.scroll = 0
		case "c":
			return p, &Action{Kind: ActionClearLogs}, nil
		}
	}
	return p, nil, nil
}

// Hints implements Page.
func (p *Logs) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "↑↓", Label: "scroll"},
		{Key: "f", Label: "filter"},
		{Key: "c", Label: "clear"},
	}
}

// View implements Page.
func (p *Logs) View(theme layout.Theme, width, height int) string {
	filterLabel := "all"
	switch p.filter {
	case "warn":
		filterLabel = "warn+"
	case "error":
		filterLabel = "error"
	}
	filtered := p.filtered()
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("LOGS") +
		lipgloss.NewStyle().Foreground(theme.Muted).Render("  filter: "+filterLabel+
			"   count: "+format.Truncate(itoa(len(filtered)), 8))
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	end := len(filtered) - p.scroll
	if end > len(filtered) {
		end = len(filtered)
	}
	if end < 0 {
		end = 0
	}
	start := end - visible
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, visible)
	for _, entry := range filtered[start:end] {
		lines = append(lines, p.renderEntry(theme, entry, width))
	}
	body := strings.Join(lines, "\n")
	return layout.Fit(title+"\n"+body, width, height)
}

func (p *Logs) filtered() []logging.Entry {
	if p.filter == "" {
		return p.entries
	}
	out := make([]logging.Entry, 0, len(p.entries))
	for _, entry := range p.entries {
		if p.filter == "warn" && entry.Level == "info" {
			continue
		}
		if p.filter == "error" && entry.Level != "error" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (p *Logs) renderEntry(theme layout.Theme, entry logging.Entry, width int) string {
	colour := theme.Text
	switch entry.Level {
	case "warn":
		colour = theme.Warning
	case "error":
		colour = theme.Error
	case "debug":
		colour = theme.Muted
	}
	line := entry.Time.Format("15:04:05") + " " + format.PadRight(strings.ToUpper(entry.Level), 5) + " " +
		format.PadRight(entry.Source, 8) + " " + entry.Text
	return lipgloss.NewStyle().Foreground(colour).Render(format.Truncate(line, width))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
