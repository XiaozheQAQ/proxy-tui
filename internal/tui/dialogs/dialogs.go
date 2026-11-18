// Package dialogs implements the modal windows of the TUI.
package dialogs

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// IntentKind reports what the user did with a dialog.
type IntentKind int

// Dialog intents.
const (
	IntentNone IntentKind = iota
	IntentCancel
	IntentSubmit
	IntentTest
)

// Intent is the result of one dialog update.
type Intent struct {
	Kind    IntentKind
	Payload any
}

// Dialog is a modal window.
type Dialog interface {
	Title() string
	Update(msg tea.Msg) (Dialog, Intent, tea.Cmd)
	View(theme layout.Theme, width int) string
	Hints() []layout.Hint
}

// StatusSetter lets the root model report asynchronous progress inside a
// dialog that stays open.
type StatusSetter interface {
	SetBusy(status string)
	SetError(message string)
	SetStatus(status string)
}

// AddProfileResult is the payload of the add-profile dialog.
type AddProfileResult struct {
	Core    string
	Name    string
	Content string
}

// ProxyResult is the payload of the proxy picker.
type ProxyResult struct {
	Core  string
	Group string
	Name  string
}

// ProxyTestRequest asks the root model to measure one proxy delay.
type ProxyTestRequest struct {
	Core string
	Name string
}

// ConfirmResult is the payload of a confirmation dialog.
type ConfirmResult struct {
	Confirmed bool
}

// CommandResult is the payload of the command palette.
type CommandResult struct {
	Line string
}

// Box renders the standard modal frame with deterministic widths.
func Box(theme layout.Theme, title, body string, footer string, width int) string {
	if width < 30 {
		width = 30
	}
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	border := lipgloss.NewStyle().Foreground(theme.Primary)
	divider := border.Render(repeat("─", innerWidth))
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(theme.Primary).Render(format.Truncate(title, innerWidth)),
		divider,
	}
	lines = append(lines, "")
	lines = append(lines, strings.Split(layout.Fit(body, innerWidth, 0), "\n")...)
	if footer != "" {
		lines = append(lines, divider,
			lipgloss.NewStyle().Foreground(theme.Muted).Render(format.Truncate(footer, innerWidth)))
	}

	var builder strings.Builder
	builder.WriteString(border.Render("╭" + repeat("─", width-2) + "╮"))
	builder.WriteString("\n")
	for _, line := range lines {
		builder.WriteString(border.Render("│"))
		builder.WriteString(" ")
		builder.WriteString(layout.Fit(line, innerWidth, 1))
		builder.WriteString(" ")
		builder.WriteString(border.Render("│"))
		builder.WriteString("\n")
	}
	builder.WriteString(border.Render("╰" + repeat("─", width-2) + "╯"))
	return builder.String()
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}
