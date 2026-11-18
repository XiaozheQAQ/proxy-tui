package dialogs

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Confirm is a yes/no modal.
type Confirm struct {
	Message string
	yes     bool
}

// NewConfirm builds a confirmation dialog.
func NewConfirm(message string) *Confirm {
	return &Confirm{Message: message}
}

// Title implements Dialog.
func (c *Confirm) Title() string { return "CONFIRM" }

// Update implements Dialog.
func (c *Confirm) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || keyMsg.Paste {
		return c, Intent{}, nil
	}
	switch keyMsg.String() {
	case "esc", "n", "q":
		return c, Intent{Kind: IntentCancel}, nil
	case "left", "right", "tab", "h", "l":
		c.yes = !c.yes
	case "y":
		return c, Intent{Kind: IntentSubmit, Payload: ConfirmResult{Confirmed: true}}, nil
	case "enter":
		return c, Intent{Kind: IntentSubmit, Payload: ConfirmResult{Confirmed: c.yes}}, nil
	}
	return c, Intent{}, nil
}

// Hints implements Dialog.
func (c *Confirm) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "←→", Label: "choose"},
		{Key: "enter", Label: "confirm"},
		{Key: "esc", Label: "cancel"},
	}
}

// View implements Dialog.
func (c *Confirm) View(theme layout.Theme, width int) string {
	yesStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	noStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	if c.yes {
		yesStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
	} else {
		noStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
	}
	buttons := yesStyle.Render("[ yes ]") + "   " + noStyle.Render("[ no ]")
	body := strings.Join([]string{
		lipgloss.NewStyle().Foreground(theme.Text).Render(c.Message),
		"",
		buttons,
	}, "\n")
	return Box(theme, c.Title(), body, "y/n or enter", width)
}
