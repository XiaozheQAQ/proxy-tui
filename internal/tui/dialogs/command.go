package dialogs

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/tui/commands"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Command is the slash command palette.
type Command struct {
	input    textinput.Model
	matches  []commands.Command
	selected int
	err      string
}

// NewCommand builds the palette.
func NewCommand() *Command {
	input := textinput.New()
	input.Placeholder = "type a command, e.g. /core singbox"
	input.Prompt = "/ "
	input.CharLimit = 200
	input.Width = 56
	input.Focus()
	dialog := &Command{input: input}
	dialog.refresh()
	return dialog
}

func (c *Command) refresh() {
	c.matches = commands.Match(c.input.Value())
	if c.selected >= len(c.matches) {
		c.selected = maxInt(len(c.matches)-1, 0)
	}
}

// Title implements Dialog.
func (c *Command) Title() string { return "COMMAND PALETTE" }

// Update implements Dialog.
func (c *Command) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		switch keyMsg.String() {
		case "esc":
			return c, Intent{Kind: IntentCancel}, nil
		case "up", "ctrl+p":
			if c.selected > 0 {
				c.selected--
			}
			return c, Intent{}, nil
		case "down", "ctrl+n":
			if c.selected < len(c.matches)-1 {
				c.selected++
			}
			return c, Intent{}, nil
		case "tab":
			if len(c.matches) > 0 {
				c.input.SetValue(c.matches[c.selected].Name)
				c.input.CursorEnd()
				c.refresh()
			}
			return c, Intent{}, nil
		case "enter":
			line := strings.TrimSpace(c.input.Value())
			if line == "" && len(c.matches) > 0 {
				line = c.matches[c.selected].Name
			}
			if line == "" {
				return c, Intent{}, nil
			}
			return c, Intent{Kind: IntentSubmit, Payload: CommandResult{Line: line}}, nil
		}
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	c.refresh()
	return c, Intent{}, cmd
}

// Hints implements Dialog.
func (c *Command) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "enter", Label: "run"},
		{Key: "tab", Label: "complete"},
		{Key: "esc", Label: "cancel"},
	}
}

// View implements Dialog.
func (c *Command) View(theme layout.Theme, width int) string {
	inputWidth := width - 16
	if inputWidth < 24 {
		inputWidth = 24
	}
	if inputWidth > 56 {
		inputWidth = 56
	}
	if c.input.Width != inputWidth {
		c.input.Width = inputWidth
	}
	lines := []string{"  " + c.input.View(), ""}
	if len(c.matches) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Warning).Render("  no matching command"))
	} else {
		for i, cmd := range c.matches {
			label := cmd.Name
			if cmd.Usage != "" {
				label = cmd.Usage
			}
			if i == c.selected {
				style := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
				lines = append(lines, style.Render("  › "+label))
				lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).Render("      "+cmd.Help))
				continue
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(theme.Text).Render("    "+label))
		}
	}
	if c.err != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Error).Render("  "+c.err))
	}
	body := strings.Join(lines, "\n")
	return Box(theme, c.Title(), body, "enter run   tab complete   esc cancel", width)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
