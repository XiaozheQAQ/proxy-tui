package dialogs

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Help is the keyboard reference overlay.
type Help struct{}

// NewHelp builds the help dialog.
func NewHelp() *Help { return &Help{} }

// Title implements Dialog.
func (h *Help) Title() string { return "KEYBOARD REFERENCE" }

// Update implements Dialog.
func (h *Help) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && !keyMsg.Paste {
		return h, Intent{Kind: IntentCancel}, nil
	}
	return h, Intent{}, nil
}

// Hints implements Dialog.
func (h *Help) Hints() []layout.Hint {
	return []layout.Hint{{Key: "any key", Label: "close"}}
}

// View implements Dialog.
func (h *Help) View(theme layout.Theme, width int) string {
	rows := [][2]string{
		{"tab / shift+tab", "switch page"},
		{"↑ ↓", "move selection"},
		{"enter", "primary action on the current page"},
		{"A", "add subscription"},
		{"u", "update selected subscription"},
		{"d", "delete selected subscription"},
		{"s / x / r", "start / stop / restart the current core"},
		{"p", "pick a proxy node"},
		{"t", "test node latency"},
		{"/  ctrl+p", "command palette"},
		{"?", "this help"},
		{"q / ctrl+c", "quit"},
	}
	labelWidth := 18
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		label := lipgloss.NewStyle().Foreground(theme.Accent).Render(formatPad(row[0], labelWidth))
		value := lipgloss.NewStyle().Foreground(theme.Text).Render(row[1])
		lines = append(lines, "  "+label+value)
	}
	return Box(theme, h.Title(), strings.Join(lines, "\n"), "press any key", width)
}

func formatPad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
