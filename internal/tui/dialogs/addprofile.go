package dialogs

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

const (
	fieldCore = iota
	fieldName
	fieldContent
)

// AddProfile is the "Add Subscription" modal.
type AddProfile struct {
	core   model.CoreKind
	field  int
	name   textinput.Model
	body   textarea.Model
	busy   bool
	status string
	err    string
	width  int
}

// NewAddProfile builds the dialog for a target core.
func NewAddProfile(core model.CoreKind) *AddProfile {
	name := textinput.New()
	name.Placeholder = "My Subscription"
	name.CharLimit = 96
	name.Width = 56
	name.Prompt = "> "

	body := textarea.New()
	body.Placeholder = "https://example.com/api/v1/client/subscribe?token=...\n" +
		"or paste share links (ss:// vmess:// vless:// trojan:// hysteria2://), YAML or JSON"
	body.SetWidth(56)
	body.SetHeight(7)
	body.CharLimit = 200000
	body.ShowLineNumbers = false

	dialog := &AddProfile{core: core, name: name, body: body}
	dialog.focusField(fieldName)
	return dialog
}

func (d *AddProfile) focusField(field int) {
	d.field = field
	d.name.Blur()
	d.body.Blur()
	switch field {
	case fieldName:
		d.name.Focus()
	case fieldContent:
		d.body.Focus()
	}
}

// Title implements Dialog.
func (d *AddProfile) Title() string { return "ADD SUBSCRIPTION" }

// SetBusy marks the dialog as waiting for an import.
func (d *AddProfile) SetBusy(status string) {
	d.busy = true
	d.status = status
	d.err = ""
}

// SetStatus replaces the status line.
func (d *AddProfile) SetStatus(status string) {
	d.busy = false
	d.status = status
}

// SetError shows a failure without closing the dialog.
func (d *AddProfile) SetError(message string) {
	d.busy = false
	d.err = message
}

// Update implements Dialog.
func (d *AddProfile) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if isKey {
		// Bracketed paste must reach the focused text field verbatim.
		if keyMsg.Paste {
			return d.forwardPaste(msg)
		}
		switch keyMsg.String() {
		case "esc":
			return d, Intent{Kind: IntentCancel}, nil
		case "tab", "shift+tab":
			if keyMsg.String() == "shift+tab" {
				d.focusField((d.field + 2) % 3)
			} else {
				d.focusField((d.field + 1) % 3)
			}
			return d, Intent{}, nil
		case "ctrl+s":
			return d.submit()
		case "left", "right":
			if d.field == fieldCore {
				if d.core == model.CoreMihomo {
					d.core = model.CoreSingBox
				} else {
					d.core = model.CoreMihomo
				}
			}
			return d, Intent{}, nil
		case "enter":
			if d.field != fieldContent {
				d.focusField(d.field + 1)
				return d, Intent{}, nil
			}
		}
	}

	var cmd tea.Cmd
	switch d.field {
	case fieldName:
		d.name, cmd = d.name.Update(msg)
	case fieldContent:
		d.body, cmd = d.body.Update(msg)
	}
	return d, Intent{}, cmd
}

func (d *AddProfile) forwardPaste(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	var cmd tea.Cmd
	switch d.field {
	case fieldName:
		d.name, cmd = d.name.Update(msg)
	case fieldContent:
		d.body, cmd = d.body.Update(msg)
	}
	return d, Intent{}, cmd
}

func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (d *AddProfile) submit() (Dialog, Intent, tea.Cmd) {
	if d.busy {
		return d, Intent{}, nil
	}
	content := strings.TrimSpace(d.body.Value())
	if content == "" {
		d.SetError("paste a subscription URL or node list first")
		return d, Intent{}, nil
	}
	return d, Intent{Kind: IntentSubmit, Payload: AddProfileResult{
		Core:    string(d.core),
		Name:    strings.TrimSpace(d.name.Value()),
		Content: content,
	}}, nil
}

// Hints implements Dialog.
func (d *AddProfile) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "tab", Label: "next field"},
		{Key: "ctrl+s", Label: "import"},
		{Key: "esc", Label: "cancel"},
	}
}

// View implements Dialog.
func (d *AddProfile) View(theme layout.Theme, width int) string {
	fieldWidth := width - 16
	if fieldWidth < 24 {
		fieldWidth = 24
	}
	if fieldWidth > 56 {
		fieldWidth = 56
	}
	if d.name.Width != fieldWidth {
		d.name.Width = fieldWidth
	}
	if d.body.Width() != fieldWidth {
		d.body.SetWidth(fieldWidth)
	}

	labelStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)

	coreLabel := labelStyle.Render("Target core  ")
	if d.field == fieldCore {
		coreLabel = activeStyle.Render("Target core  ")
	}
	coreValue := lipgloss.NewStyle().Foreground(theme.Text).
		Render("◄ " + d.core.DisplayName() + " ►")
	coreLine := coreLabel + coreValue + labelStyle.Render("   (←/→ to change)")

	nameLabel := labelStyle.Render("Profile name ")
	if d.field == fieldName {
		nameLabel = activeStyle.Render("Profile name ")
	}
	contentLabel := labelStyle.Render("Subscription URL / content")
	if d.field == fieldContent {
		contentLabel = activeStyle.Render("Subscription URL / content")
	}

	bodyStyle := lipgloss.NewStyle().Width(d.name.Width)

	status := d.status
	statusLine := ""
	switch {
	case d.err != "":
		statusLine = lipgloss.NewStyle().Foreground(theme.Error).Render("✗ " + format.Truncate(d.err, d.name.Width))
	case d.busy:
		statusLine = lipgloss.NewStyle().Foreground(theme.Warning).Render("… " + status)
	case status != "":
		statusLine = lipgloss.NewStyle().Foreground(theme.Success).Render("✓ " + format.Truncate(status, d.name.Width))
	default:
		statusLine = labelStyle.Render("Paste one URL, several URLs (one per line) or raw share links.")
	}

	body := strings.Join([]string{
		coreLine,
		"",
		nameLabel,
		"  " + d.name.View(),
		"",
		contentLabel,
		indentLines(bodyStyle.Render(d.body.View()), "  "),
		"",
		statusLine,
	}, "\n")

	d.width = width
	footerParts := make([]string, 0, len(d.Hints()))
	for _, hint := range d.Hints() {
		footerParts = append(footerParts, hint.Key+" "+hint.Label)
	}
	return Box(theme, d.Title(), body, strings.Join(footerParts, "   "), width)
}
