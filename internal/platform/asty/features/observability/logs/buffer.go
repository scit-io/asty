package logs

import (
	"sync"
)

// LogLine is a single buffered log entry.
type LogLine struct {
	Timestamp int64  `json:"ts"`
	Level     string `json:"level,omitempty"`
	Line      string `json:"line"`
}

// Buffer is a per-source ring buffer of recent log lines. Safe for
// concurrent use. Sources: "cluster", "node.{id}", "node.{id}.svc.{svc}".
type Buffer struct {
	mu   sync.RWMutex
	bufs map[string][]LogLine
	maxN int
}

func NewBuffer(maxN int) *Buffer {
	return &Buffer{bufs: make(map[string][]LogLine), maxN: maxN}
}

// Append adds a line to the named source buffer, evicting the oldest entry
// when the buffer is full.
func (b *Buffer) Append(source string, line LogLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	buf := b.bufs[source]
	buf = append(buf, line)
	if len(buf) > b.maxN {
		buf = buf[len(buf)-b.maxN:]
	}
	b.bufs[source] = buf
}

// GetLast returns the last n lines from the named source.
func (b *Buffer) GetLast(source string, n int) []LogLine {
	b.mu.RLock()
	defer b.mu.RUnlock()
	buf := b.bufs[source]
	if len(buf) == 0 {
		return []LogLine{}
	}
	if n <= 0 || n >= len(buf) {
		out := make([]LogLine, len(buf))
		copy(out, buf)
		return out
	}
	out := make([]LogLine, n)
	copy(out, buf[len(buf)-n:])
	return out
}
