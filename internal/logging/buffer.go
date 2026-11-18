// Package logging collects process and application log lines for the TUI.
package logging

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Entry is one log line.
type Entry struct {
	Time   time.Time
	Level  string
	Source string
	Text   string
}

// Buffer is a bounded, goroutine safe log ring.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	max     int
	sink    func(Entry)
}

// NewBuffer creates a buffer holding at most max entries.
func NewBuffer(max int) *Buffer {
	if max <= 0 {
		max = 2000
	}
	return &Buffer{max: max}
}

// SetSink registers an additional consumer, e.g. the on-disk log file.
func (b *Buffer) SetSink(fn func(Entry)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sink = fn
}

// Add appends one entry.
func (b *Buffer) Add(level, source, text string) {
	entry := Entry{Time: time.Now(), Level: strings.ToLower(level), Source: source, Text: text}
	b.mu.Lock()
	b.entries = append(b.entries, entry)
	if len(b.entries) > b.max {
		over := len(b.entries) - b.max
		b.entries = append([]Entry(nil), b.entries[over:]...)
	}
	sink := b.sink
	b.mu.Unlock()
	if sink != nil {
		sink(entry)
	}
}

// Logger returns a logging function bound to a source name.
func (b *Buffer) Logger(source string) func(level, msg string) {
	return func(level, msg string) {
		b.Add(level, source, strings.TrimRight(msg, "\r\n"))
	}
}

// Entries returns a copy of every retained entry.
func (b *Buffer) Entries() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Entry, len(b.entries))
	copy(out, b.entries)
	return out
}

// Tail returns at most n newest entries.
func (b *Buffer) Tail(n int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || n >= len(b.entries) {
		out := make([]Entry, len(b.entries))
		copy(out, b.entries)
		return out
	}
	out := make([]Entry, n)
	copy(out, b.entries[len(b.entries)-n:])
	return out
}

// Clear removes every retained entry.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = nil
}

// Format renders an entry for plain text output.
func (e Entry) Format() string {
	return fmt.Sprintf("%s %-5s %-8s %s", e.Time.Format("15:04:05"), strings.ToUpper(e.Level), e.Source, e.Text)
}
