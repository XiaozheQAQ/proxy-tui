// Package format renders numbers and durations for the terminal UI.
package format

import (
	"fmt"
	"strings"
	"time"
)

// Bytes renders a byte count with binary units.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

// Speed renders a bytes-per-second rate.
func Speed(bytesPerSecond int64) string {
	return Bytes(bytesPerSecond) + "/s"
}

// Duration renders a compact human duration.
func Duration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	seconds := int(d.Seconds())
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh%02dm", seconds/3600, (seconds%3600)/60)
	default:
		return fmt.Sprintf("%dd%02dh", seconds/86400, (seconds%86400)/3600)
	}
}

// DurationInterval renders a number of seconds as a compact interval.
func DurationInterval(seconds int) string {
	if seconds <= 0 {
		return "disabled"
	}
	return Duration(time.Duration(seconds) * time.Second)
}

// Relative renders how long ago t was.
func Relative(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	delta := time.Since(t)
	switch {
	case delta < 0:
		return "just now"
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	}
}

// Truncate shortens s to width columns, adding an ellipsis when cut.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// PadRight pads s with spaces to width columns.
func PadRight(s string, width int) string {
	if len([]rune(s)) >= width {
		return Truncate(s, width)
	}
	return s + strings.Repeat(" ", width-len([]rune(s)))
}
