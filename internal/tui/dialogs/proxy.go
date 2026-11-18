package dialogs

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// ProxyPicker selects a node inside a proxy group.
type ProxyPicker struct {
	kind    model.CoreKind
	proxies []core.Proxy
	groups  []core.Proxy
	groupAt int
	nodeAt  int
	column  int
}

// NewProxyPicker builds the picker from a proxy list.
func NewProxyPicker(kind model.CoreKind, proxies []core.Proxy) *ProxyPicker {
	picker := &ProxyPicker{kind: kind}
	for _, proxy := range proxies {
		if proxy.Group {
			picker.groups = append(picker.groups, proxy)
		}
	}
	picker.clampNode()
	return picker
}

// Title implements Dialog.
func (p *ProxyPicker) Title() string { return "SELECT PROXY" }

func (p *ProxyPicker) currentGroup() (core.Proxy, bool) {
	if p.groupAt >= 0 && p.groupAt < len(p.groups) {
		return p.groups[p.groupAt], true
	}
	return core.Proxy{}, false
}

func (p *ProxyPicker) clampNode() {
	group, ok := p.currentGroup()
	if !ok {
		p.nodeAt = 0
		return
	}
	if p.nodeAt >= len(group.All) {
		p.nodeAt = len(group.All) - 1
	}
	if p.nodeAt < 0 {
		p.nodeAt = 0
	}
}

// Update implements Dialog.
func (p *ProxyPicker) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || keyMsg.Paste {
		return p, Intent{}, nil
	}
	switch keyMsg.String() {
	case "esc":
		return p, Intent{Kind: IntentCancel}, nil
	case "tab", "left", "right", "h", "l":
		p.column = 1 - p.column
	case "up", "k":
		if p.column == 0 {
			if p.groupAt > 0 {
				p.groupAt--
				p.clampNode()
			}
		} else if p.nodeAt > 0 {
			p.nodeAt--
		}
	case "down", "j":
		if p.column == 0 {
			if p.groupAt < len(p.groups)-1 {
				p.groupAt++
				p.clampNode()
			}
		} else {
			group, ok := p.currentGroup()
			if ok && p.nodeAt < len(group.All)-1 {
				p.nodeAt++
			}
		}
	case "enter":
		group, ok := p.currentGroup()
		if !ok {
			return p, Intent{Kind: IntentCancel}, nil
		}
		if p.nodeAt >= len(group.All) {
			return p, Intent{}, nil
		}
		return p, Intent{Kind: IntentSubmit, Payload: ProxyResult{
			Core:  string(p.kind),
			Group: group.Name,
			Name:  group.All[p.nodeAt],
		}}, nil
	}
	return p, Intent{}, nil
}

// Hints implements Dialog.
func (p *ProxyPicker) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "←→", Label: "column"},
		{Key: "enter", Label: "select"},
		{Key: "esc", Label: "cancel"},
	}
}

// View implements Dialog.
func (p *ProxyPicker) View(theme layout.Theme, width int) string {
	if len(p.groups) == 0 {
		return Box(theme, p.Title(), lipgloss.NewStyle().Foreground(theme.Warning).
			Render("The running core reports no proxy groups."), "", width)
	}
	left := p.renderGroups(theme, width/2-6)
	right := p.renderNodes(theme, width/2-6)
	body := layout.JoinColumns(left, right, width/2-4, width/2-4, maxLines(left, right))
	footer := "↑↓ move   tab column   enter select   esc cancel"
	return Box(theme, p.Title(), body, footer, width)
}

func (p *ProxyPicker) renderGroups(theme layout.Theme, width int) string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.Muted).Render("GROUPS")}
	for i, group := range p.groups {
		label := format.Truncate(group.Name, width-6)
		if i == p.groupAt {
			style := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
			if p.column == 0 {
				label = "› " + label
			} else {
				label = "  " + label
			}
			lines = append(lines, style.Render(label))
			continue
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Text).Render("  "+label))
	}
	return strings.Join(lines, "\n")
}

func (p *ProxyPicker) renderNodes(theme layout.Theme, width int) string {
	group, ok := p.currentGroup()
	if !ok {
		return ""
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.Muted).
		Render(format.Truncate("NODES · "+group.Name, width))}
	for i, name := range group.All {
		marker := "  "
		if name == group.Now {
			marker = "● "
		}
		label := format.Truncate(marker+name, width-2)
		if i == p.nodeAt {
			style := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
			if p.column == 1 {
				label = "›" + label
			} else {
				label = " " + label
			}
			lines = append(lines, style.Render(label))
			continue
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Text).Render(" "+label))
	}
	return strings.Join(lines, "\n")
}

func maxLines(a, b string) int {
	countA := strings.Count(a, "\n")
	countB := strings.Count(b, "\n")
	if countA > countB {
		return countA + 1
	}
	return countB + 1
}

var _ = fmt.Sprintf
