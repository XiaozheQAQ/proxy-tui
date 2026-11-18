package dialogs

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Info is a read-only text dialog, used for diagnostics.
type Info struct {
	Heading string
	Lines   []string
}

// NewInfo builds an information dialog.
func NewInfo(heading string, lines []string) *Info {
	return &Info{Heading: heading, Lines: lines}
}

// Title implements Dialog.
func (i *Info) Title() string { return i.Heading }

// Update implements Dialog.
func (i *Info) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && !keyMsg.Paste {
		return i, Intent{Kind: IntentCancel}, nil
	}
	return i, Intent{}, nil
}

// Hints implements Dialog.
func (i *Info) Hints() []layout.Hint {
	return []layout.Hint{{Key: "any key", Label: "close"}}
}

// View implements Dialog.
func (i *Info) View(theme layout.Theme, width int) string {
	lines := make([]string, 0, len(i.Lines))
	for _, line := range i.Lines {
		style := lipgloss.NewStyle().Foreground(theme.Text)
		switch {
		case strings.HasPrefix(line, "FAIL"), strings.HasPrefix(line, "error"):
			style = lipgloss.NewStyle().Foreground(theme.Error)
		case strings.HasPrefix(line, "warning"):
			style = lipgloss.NewStyle().Foreground(theme.Warning)
		case strings.HasPrefix(line, "ok"), strings.HasPrefix(line, "note"):
			style = lipgloss.NewStyle().Foreground(theme.Muted)
		}
		lines = append(lines, style.Render("  "+format.Truncate(line, width-8)))
	}
	return Box(theme, i.Heading, strings.Join(lines, "\n"), "press any key", width)
}
