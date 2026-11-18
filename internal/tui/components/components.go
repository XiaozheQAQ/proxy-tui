// Package components holds the reusable widgets shared by every page.
package components

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

// Badge renders a coloured status pill.
func Badge(theme layout.Theme, label string, tone string) string {
	colour := theme.Muted
	switch tone {
	case "ok":
		colour = theme.Success
	case "warn":
		colour = theme.Warning
	case "err":
		colour = theme.Error
	case "accent":
		colour = theme.Accent
	}
	return lipgloss.NewStyle().Foreground(colour).Render("● " + label)
}

// Card renders a bordered panel with a title. The frame is drawn manually so
// every line has exactly the requested width regardless of styled content.
func Card(theme layout.Theme, title, body string, width, height int) string {
	if width < 10 {
		width = 10
	}
	innerWidth := width - 4
	if innerWidth < 4 {
		innerWidth = 4
	}
	titleLine := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render(
		format.Truncate(title, innerWidth))
	bodyLines := strings.Split(layout.Fit(body, innerWidth, 0), "\n")
	lines := append([]string{titleLine}, bodyLines...)

	available := height - 2
	if height <= 0 {
		available = len(lines)
	}
	if available < 1 {
		available = 1
	}
	if len(lines) > available {
		lines = lines[:available]
	}
	for len(lines) < available {
		lines = append(lines, "")
	}

	var builder strings.Builder
	border := lipgloss.NewStyle().Foreground(theme.Border)
	builder.WriteString(border.Render("╭" + strings.Repeat("─", width-2) + "╮"))
	builder.WriteString("\n")
	for _, line := range lines {
		builder.WriteString(border.Render("│"))
		builder.WriteString(" ")
		builder.WriteString(layout.Fit(line, innerWidth, 1))
		builder.WriteString(" ")
		builder.WriteString(border.Render("│"))
		builder.WriteString("\n")
	}
	builder.WriteString(border.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return builder.String()
}

// LabelValue renders a muted label followed by a value.
func LabelValue(theme layout.Theme, label, value string) string {
	return lipgloss.NewStyle().Foreground(theme.Muted).Render(label+": ") +
		lipgloss.NewStyle().Foreground(theme.Text).Render(value)
}

// Table renders rows with a caret selected row and a scrolling window.
// widths are the per-column widths; a cursor column of two cells is prepended.
func Table(theme layout.Theme, headers []string, rows [][]string, widths []int, selected, offset, visible, totalWidth int) string {
	if visible <= 0 {
		visible = len(rows)
	}
	if offset < 0 {
		offset = 0
	}
	if len(rows) > 0 && selected >= len(rows) {
		selected = len(rows) - 1
	}
	if selected < 0 {
		selected = 0
	}
	if len(rows) > visible && selected >= offset+visible {
		offset = selected - visible + 1
	}
	if selected < offset {
		offset = selected
	}

	lines := []string{renderRow(theme, headers, widths, false, true, totalWidth)}
	end := offset + visible - 1
	if end > len(rows) {
		end = len(rows)
	}
	for i := offset; i < end; i++ {
		lines = append(lines, renderRow(theme, rows[i], widths, i == selected, false, totalWidth))
	}
	if len(rows) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).Render("  (empty)"))
	} else if len(rows) > visible {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Muted).
			Render("  "+strconv.Itoa(offset+1)+"-"+strconv.Itoa(end)+" of "+strconv.Itoa(len(rows))))
	}
	for i := range lines {
		lines[i] = layout.Fit(lines[i], totalWidth, 1)
	}
	return strings.Join(lines, "\n")
}

func renderRow(theme layout.Theme, cells []string, widths []int, selected, header bool, totalWidth int) string {
	cursor := "  "
	if selected {
		cursor = "› "
	}
	parts := make([]string, 0, len(cells))
	for i, cell := range cells {
		width := 0
		if i < len(widths) {
			width = widths[i]
		}
		text := cell
		if width > 0 {
			text = format.PadRight(text, width)
		}
		parts = append(parts, text)
	}
	line := format.PadRight(cursor+strings.Join(parts, " "), totalWidth)
	switch {
	case header:
		return lipgloss.NewStyle().Bold(true).Foreground(theme.Muted).Render(line)
	case selected:
		return lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render(line)
	default:
		return lipgloss.NewStyle().Foreground(theme.Text).Render(line)
	}
}

// Bar renders a labelled progress bar.
func Bar(theme layout.Theme, label string, ratio float64, width int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	if width < 12 {
		width = 12
	}
	filled := int(float64(width-8) * ratio)
	if filled > width-8 {
		filled = width - 8
	}
	if filled < 0 {
		filled = 0
	}
	bar := lipgloss.NewStyle().Foreground(theme.Accent).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("░", width-8-filled))
	return label + " " + bar
}

// Hints joins footer hints for compact display.
func Hints(theme layout.Theme, hints []layout.Hint) string {
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.Accent).Render(hint.Key)+
			lipgloss.NewStyle().Foreground(theme.Muted).Render(" "+hint.Label))
	}
	return strings.Join(parts, "  ")
}

// KeyValueLines renders aligned label/value rows.
func KeyValueLines(theme layout.Theme, pairs [][2]string, width int) string {
	labelWidth := 0
	for _, pair := range pairs {
		if len(pair[0]) > labelWidth {
			labelWidth = len(pair[0])
		}
	}
	lines := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		label := lipgloss.NewStyle().Foreground(theme.Muted).Render(format.PadRight(pair[0], labelWidth))
		value := lipgloss.NewStyle().Foreground(theme.Text).Render(pair[1])
		lines = append(lines, format.Truncate("  "+label+"  "+value, width))
	}
	return strings.Join(lines, "\n")
}

// Empty renders a muted empty-state message.
func Empty(theme layout.Theme, message string) string {
	return lipgloss.NewStyle().Foreground(theme.Muted).Render("  " + message)
}
