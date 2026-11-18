// Package layout provides terminal geometry, theming and the persistent
// frame (header, sidebar, footer) of the TUI.
package layout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
)

// Hint is one footer key binding.
type Hint struct {
	Key   string
	Label string
}

// Theme is a resolved colour palette.
type Theme struct {
	Name    string
	Primary lipgloss.Color
	Accent  lipgloss.Color
	Muted   lipgloss.Color
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color
	Border  lipgloss.Color
	Text    lipgloss.Color
}

// DarkTheme is the default palette.
func DarkTheme() Theme {
	return Theme{
		Name:    "dark",
		Primary: lipgloss.Color("39"),
		Accent:  lipgloss.Color("45"),
		Muted:   lipgloss.Color("244"),
		Success: lipgloss.Color("42"),
		Warning: lipgloss.Color("214"),
		Error:   lipgloss.Color("203"),
		Border:  lipgloss.Color("240"),
		Text:    lipgloss.Color("252"),
	}
}

// LightTheme is optimised for light terminal backgrounds.
func LightTheme() Theme {
	return Theme{
		Name:    "light",
		Primary: lipgloss.Color("25"),
		Accent:  lipgloss.Color("31"),
		Muted:   lipgloss.Color("240"),
		Success: lipgloss.Color("28"),
		Warning: lipgloss.Color("130"),
		Error:   lipgloss.Color("160"),
		Border:  lipgloss.Color("250"),
		Text:    lipgloss.Color("235"),
	}
}

// ThemeByName resolves a stored theme name.
func ThemeByName(name string) Theme {
	if strings.EqualFold(name, "light") {
		return LightTheme()
	}
	return DarkTheme()
}

// Geometry is the resolved frame geometry.
type Geometry struct {
	Width         int
	Height        int
	HeaderHeight  int
	FooterHeight  int
	SidebarWidth  int
	ContentWidth  int
	ContentHeight int
	TooSmall      bool
}

// Minimum terminal size the layout supports.
const (
	MinWidth  = 60
	MinHeight = 16
)

// Compute derives the frame geometry for a terminal size.
func Compute(width, height int) Geometry {
	geometry := Geometry{Width: width, Height: height, HeaderHeight: 2, FooterHeight: 3}
	if width < MinWidth || height < MinHeight {
		geometry.TooSmall = true
		geometry.SidebarWidth = 0
		geometry.ContentWidth = width
		geometry.ContentHeight = height
		return geometry
	}
	sidebar := width / 5
	if sidebar < 16 {
		sidebar = 16
	}
	if sidebar > 26 {
		sidebar = 26
	}
	geometry.SidebarWidth = sidebar
	geometry.ContentWidth = width - sidebar - 1
	geometry.ContentHeight = height - geometry.HeaderHeight - geometry.FooterHeight
	return geometry
}

// Header renders the top bar with core status and throughput.
func Header(theme Theme, coreLabel string, running bool, apiReady bool, up, down int64, width int) string {
	title := "PROXY TUI"
	statusText := "OFFLINE"
	statusColour := theme.Muted
	if running {
		statusText = "RUNNING"
		statusColour = theme.Success
		if !apiReady {
			statusText = "STARTING"
			statusColour = theme.Warning
		}
	}
	speedText := "↑ " + format.Speed(up) + "   ↓ " + format.Speed(down)
	speedWidth := lipgloss.Width(speedText)

	coreBudget := width - speedWidth - lipgloss.Width(title) - lipgloss.Width(statusText) - 8
	if coreBudget < 0 {
		coreBudget = 0
	}
	coreText := format.Truncate(coreLabel, coreBudget)

	line := lipgloss.NewStyle().Bold(true).Foreground(theme.Primary).Render(title) + "   " +
		lipgloss.NewStyle().Foreground(theme.Text).Render(coreText) + " " +
		lipgloss.NewStyle().Foreground(statusColour).Render(statusText)

	padding := width - lipgloss.Width(line) - speedWidth
	if padding < 1 {
		padding = 1
	}
	speeds := lipgloss.NewStyle().Foreground(theme.Muted).Render(speedText)
	headerLine := line + strings.Repeat(" ", padding) + speeds
	if lipgloss.Width(headerLine) > width {
		headerLine = format.Truncate(headerLine, width)
	}
	return headerLine + "\n" + Separator(theme, width)
}

// Separator renders a full width horizontal rule.
func Separator(theme Theme, width int) string {
	return lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", width))
}

// Sidebar renders the navigation column.
func Sidebar(theme Theme, items []string, selected int, width, height int) string {
	title := lipgloss.NewStyle().Foreground(theme.Muted).Render("NAVIGATION")
	lines := []string{title, ""}
	for i, item := range items {
		label := format.Truncate(item, width-4)
		if i == selected {
			marker := lipgloss.NewStyle().Foreground(theme.Accent).Render("▌")
			text := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render(" " + label)
			lines = append(lines, marker+text)
			continue
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Text).Render("  "+label))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	padded := make([]string, 0, len(lines))
	for _, line := range lines {
		padded = append(padded, Fit(line, width, 1))
	}
	return strings.Join(padded, "\n")
}

// Footer renders the key hints line and status line.
func Footer(theme Theme, hints []Hint, status string, width int) string {
	// Reserve room for at least the first hint.
	parts := make([]string, 0, len(hints))
	total := 0
	for _, hint := range hints {
		plain := "[" + hint.Key + "] " + hint.Label
		need := lipgloss.Width(plain) + 2
		if total+need > width-1 && len(parts) > 0 {
			break
		}
		parts = append(parts, hint.Key+"\x00"+hint.Label)
		total += need
	}
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		key, label, _ := strings.Cut(part, "\x00")
		rendered = append(rendered, lipgloss.NewStyle().Foreground(theme.Accent).Render("["+key+"]")+" "+
			lipgloss.NewStyle().Foreground(theme.Muted).Render(label))
	}
	line := strings.Join(rendered, "  ")
	return Separator(theme, width) + "\n" + Fit(line, width, 1) + "\n" + Fit(status, width, 1)
}

// Fit pads or truncates s to exactly width columns and height lines.
func Fit(s string, width, height int) string {
	if width <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		plainWidth := lipgloss.Width(line)
		switch {
		case plainWidth > width:
			out = append(out, clipStyled(line, width))
		case plainWidth < width:
			out = append(out, line+strings.Repeat(" ", width-plainWidth))
		default:
			out = append(out, line)
		}
	}
	for height > 0 && len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return strings.Join(out, "\n")
}

// clipStyled truncates s to width columns, dropping styling when the string
// contains ANSI escapes so no escape sequence is cut in half.
func clipStyled(s string, width int) string {
	if strings.Contains(s, "\x1b") {
		return format.Truncate(stripANSI(s), width)
	}
	return format.Truncate(s, width)
}

func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Center places block inside a width x height area.
func Center(block string, width, height int) string {
	blockWidth := lipgloss.Width(block)
	blockHeight := strings.Count(block, "\n") + 1
	if blockWidth > width {
		block = Fit(block, width, 0)
		blockWidth = width
	}
	if blockHeight > height {
		block = Fit(block, blockWidth, height)
		blockHeight = height
	}
	left := (width - blockWidth) / 2
	top := (height - blockHeight) / 2
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	lines := strings.Split(block, "\n")
	out := make([]string, 0, height)
	for i := 0; i < top; i++ {
		out = append(out, strings.Repeat(" ", width))
	}
	for _, line := range lines {
		out = append(out, strings.Repeat(" ", left)+Fit(line, width-left, 1))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return strings.Join(out[:height], "\n")
}

// JoinColumns places left and right side by side with a divider.
func JoinColumns(left, right string, leftWidth, rightWidth, height int) string {
	leftLines := strings.Split(Fit(left, leftWidth, height), "\n")
	rightLines := strings.Split(Fit(right, rightWidth, height), "\n")
	divider := "│"
	out := make([]string, height)
	for i := 0; i < height; i++ {
		out[i] = leftLines[i] + divider + rightLines[i]
	}
	return strings.Join(out, "\n")
}

// TooSmall renders a message for terminals below the minimum size.
func TooSmall(theme Theme, width, height int) string {
	message := lipgloss.NewStyle().Foreground(theme.Warning).Render(
		"Terminal too small — resize to at least 60x16")
	return Center(message, width, height)
}
