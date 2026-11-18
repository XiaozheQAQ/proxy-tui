package dialogs

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// ProxyPicker browses proxy groups recursively and selects a member.
type ProxyPicker struct {
	kind    model.CoreKind
	proxies []core.Proxy
	byName  map[string]core.Proxy
	groups  []core.Proxy

	groupAt  int
	memberAt int
	focus    int // 0 = groups, 1 = members
	stack    []string

	searching bool
	query     string
	status    string
}

// NewProxyPicker builds the picker from a proxy list.
func NewProxyPicker(kind model.CoreKind, proxies []core.Proxy) *ProxyPicker {
	picker := &ProxyPicker{kind: kind, proxies: proxies, byName: map[string]core.Proxy{}}
	for _, proxy := range proxies {
		picker.byName[proxy.Name] = proxy
	}
	picker.groups = picker.rootGroups()
	picker.clamp()
	return picker
}

func (p *ProxyPicker) rootGroups() []core.Proxy {
	referenced := map[string]bool{}
	for _, proxy := range p.proxies {
		if !proxy.Group || proxy.Name == "GLOBAL" {
			continue
		}
		for _, member := range proxy.All {
			if child, ok := p.byName[member]; ok && child.Group {
				referenced[child.Name] = true
			}
		}
	}
	var roots []core.Proxy
	for _, proxy := range p.proxies {
		if !proxy.Group || proxy.Name == "GLOBAL" || referenced[proxy.Name] {
			continue
		}
		roots = append(roots, proxy)
	}
	if len(roots) == 0 {
		for _, proxy := range p.proxies {
			if proxy.Group && proxy.Name != "GLOBAL" {
				roots = append(roots, proxy)
			}
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		si, sj := roots[i].Type == "Selector", roots[j].Type == "Selector"
		if si != sj {
			return si
		}
		return roots[i].Name < roots[j].Name
	})
	return roots
}

// Title implements Dialog.
func (p *ProxyPicker) Title() string { return p.kind.DisplayName() + " / PROXIES" }

func (p *ProxyPicker) currentGroup() (core.Proxy, bool) {
	if len(p.stack) > 0 {
		group, ok := p.byName[p.stack[len(p.stack)-1]]
		return group, ok
	}
	if p.groupAt >= 0 && p.groupAt < len(p.groups) {
		return p.groups[p.groupAt], true
	}
	return core.Proxy{}, false
}

func (p *ProxyPicker) members() []core.Proxy {
	group, ok := p.currentGroup()
	if !ok {
		return nil
	}
	out := make([]core.Proxy, 0, len(group.All))
	for _, name := range group.All {
		if p.query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(p.query)) {
			continue
		}
		if proxy, ok := p.byName[name]; ok {
			out = append(out, proxy)
			continue
		}
		out = append(out, core.Proxy{Name: name, Delay: -1})
	}
	return out
}

func (p *ProxyPicker) clamp() {
	if p.groupAt >= len(p.groups) {
		p.groupAt = len(p.groups) - 1
	}
	if p.groupAt < 0 {
		p.groupAt = 0
	}
	if members := p.members(); p.memberAt >= len(members) {
		p.memberAt = len(members) - 1
	}
	if p.memberAt < 0 {
		p.memberAt = 0
	}
	if len(p.stack) > 0 {
		if _, ok := p.byName[p.stack[len(p.stack)-1]]; !ok {
			p.stack = p.stack[:len(p.stack)-1]
		}
	}
}

func (p *ProxyPicker) currentMember() (core.Proxy, bool) {
	members := p.members()
	if p.memberAt >= 0 && p.memberAt < len(members) {
		return members[p.memberAt], true
	}
	return core.Proxy{}, false
}

// Update implements Dialog.
func (p *ProxyPicker) Update(msg tea.Msg) (Dialog, Intent, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || keyMsg.Paste {
		return p, Intent{}, nil
	}
	if p.searching {
		switch keyMsg.String() {
		case "esc":
			p.searching = false
			p.query = ""
			p.clamp()
		case "enter":
			p.searching = false
		case "backspace":
			if len(p.query) > 0 {
				p.query = p.query[:len(p.query)-1]
				p.clamp()
			}
		default:
			if len(keyMsg.Runes) > 0 {
				p.query += string(keyMsg.Runes)
				p.clamp()
			}
		}
		return p, Intent{}, nil
	}

	switch keyMsg.String() {
	case "esc":
		if len(p.stack) > 0 {
			p.stack = p.stack[:len(p.stack)-1]
			p.memberAt = 0
			p.status = ""
			p.clamp()
			return p, Intent{}, nil
		}
		return p, Intent{Kind: IntentCancel}, nil
	case "tab", "left", "right", "h", "l":
		p.focus = 1 - p.focus
	case "up", "k":
		if p.focus == 0 {
			if p.groupAt > 0 {
				p.groupAt--
				p.stack = nil
				p.memberAt = 0
				p.status = ""
				p.clamp()
			}
		} else if p.memberAt > 0 {
			p.memberAt--
		}
	case "down", "j":
		if p.focus == 0 {
			if p.groupAt < len(p.groups)-1 {
				p.groupAt++
				p.stack = nil
				p.memberAt = 0
				p.status = ""
				p.clamp()
			}
		} else if p.memberAt < len(p.members())-1 {
			p.memberAt++
		}
	case "/":
		p.searching = true
		p.query = ""
		p.status = ""
	case "t":
		member, ok := p.currentMember()
		if !ok {
			return p, Intent{}, nil
		}
		if member.Group {
			p.status = "test a node, not a group"
			return p, Intent{}, nil
		}
		return p, Intent{Kind: IntentTest, Payload: ProxyTestRequest{Core: string(p.kind), Name: member.Name}}, nil
	case "enter":
		if p.focus == 0 {
			if len(p.groups) == 0 {
				return p, Intent{Kind: IntentCancel}, nil
			}
			p.focus = 1
			p.stack = nil
			p.memberAt = 0
			p.status = ""
			return p, Intent{}, nil
		}
		member, ok := p.currentMember()
		if !ok {
			return p, Intent{}, nil
		}
		if member.Group {
			p.stack = append(p.stack, member.Name)
			p.memberAt = 0
			p.status = ""
			p.clamp()
			return p, Intent{}, nil
		}
		return p.selectMember(member)
	case "s":
		member, ok := p.currentMember()
		if !ok {
			return p, Intent{}, nil
		}
		return p.selectMember(member)
	}
	return p, Intent{}, nil
}

func (p *ProxyPicker) selectMember(member core.Proxy) (Dialog, Intent, tea.Cmd) {
	group, ok := p.currentGroup()
	if !ok {
		return p, Intent{}, nil
	}
	if group.Type != "Selector" {
		p.status = group.Type + " is automatic; selection is managed by the core"
		return p, Intent{}, nil
	}
	return p, Intent{Kind: IntentSubmit, Payload: ProxyResult{
		Core:  string(p.kind),
		Group: group.Name,
		Name:  member.Name,
	}}, nil
}

// Hints implements Dialog.
func (p *ProxyPicker) Hints() []layout.Hint {
	return []layout.Hint{
		{Key: "↑↓", Label: "move"},
		{Key: "tab", Label: "pane"},
		{Key: "enter", Label: "open/select"},
		{Key: "s", Label: "select group"},
		{Key: "t", Label: "test"},
		{Key: "/", Label: "search"},
		{Key: "esc", Label: "back"},
	}
}

// View implements Dialog.
func (p *ProxyPicker) View(theme layout.Theme, width int) string {
	if len(p.groups) == 0 {
		body := lipgloss.NewStyle().Foreground(theme.Warning).
			Render("The running core reports no proxy groups. Start the core, then press p again.")
		return Box(theme, p.Title(), body, "esc close", width)
	}
	group, _ := p.currentGroup()
	header := fmt.Sprintf("%s · %s · now: %s", group.Name, group.Type, dash(group.Now))
	if len(p.stack) > 0 {
		header = "PATH: " + strings.Join(p.stack, " / ") + "\n" + header
	}
	if group.Type != "Selector" {
		header += "\n" + lipgloss.NewStyle().Foreground(theme.Warning).
			Render("automatic group — testing is allowed, manual selection is not")
	}
	left := p.renderGroups(theme, width/2-6)
	right := p.renderMembers(theme, width/2-6)
	body := layout.JoinColumns(left, right, width/2-4, width/2-4, maxLines(left, right))
	body = header + "\n\n" + body
	if p.status != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(theme.Warning).Render(p.status)
	}
	if p.searching {
		body += "\n" + lipgloss.NewStyle().Foreground(theme.Accent).Render("search: "+p.query+"_")
	}
	footer := "↑↓ move · tab pane · enter open/select · s select group · t test · / search · esc back"
	return Box(theme, p.Title(), body, footer, width)
}

func (p *ProxyPicker) renderGroups(theme layout.Theme, width int) string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.Muted).Render("PROXY GROUPS")}
	for i, group := range p.groups {
		label := format.Truncate(group.Name, width-8)
		if group.Now != "" {
			label += "  → " + format.Truncate(group.Now, 14)
		}
		if i == p.groupAt {
			style := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
			if p.focus == 0 {
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

func (p *ProxyPicker) renderMembers(theme layout.Theme, width int) string {
	group, _ := p.currentGroup()
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.Muted).
		Render(format.Truncate("MEMBERS · "+group.Name, width))}
	members := p.members()
	if len(members) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).Render("  no members"))
		return strings.Join(lines, "\n")
	}
	for i, member := range members {
		marker := "  "
		if member.Name == group.Now {
			marker = "● "
		}
		label := marker + format.Truncate(member.Name, width-24) + "  " + proxyMeta(member)
		if i == p.memberAt {
			style := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent)
			if p.focus == 1 {
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

func proxyMeta(proxy core.Proxy) string {
	kind := proxy.Type
	if kind == "" {
		kind = "unknown"
	}
	if proxy.Group {
		return kind + " ▸"
	}
	delay := "—"
	switch {
	case proxy.Delay > 0:
		delay = fmt.Sprintf("%dms", proxy.Delay)
	case proxy.Delay == 0:
		delay = "failed"
	}
	health := "unknown"
	if proxy.Alive {
		health = "ok"
	} else if proxy.Delay >= 0 {
		health = "down"
	}
	return kind + " " + delay + " " + health
}

func dash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func maxLines(a, b string) int {
	countA := strings.Count(a, "\n")
	countB := strings.Count(b, "\n")
	if countA > countB {
		return countA + 1
	}
	return countB + 1
}
