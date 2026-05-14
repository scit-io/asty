package logs

import (
	"sync"

	"asty/asty/internal/core/util/ringbuf"
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
	bufs map[string]*ringbuf.Ring[LogLine]
	maxN int
}

func NewBuffer(maxN int) *Buffer {
	return &Buffer{bufs: make(map[string]*ringbuf.Ring[LogLine]), maxN: maxN}
}

// Append adds a line to the named source buffer, evicting the oldest entry
// when the buffer is full.
func (b *Buffer) Append(source string, line LogLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.bufs[source]
	if !ok {
		r = ringbuf.New[LogLine](b.maxN)
		b.bufs[source] = r
	}
	r.Push(line)
}

// GetLast returns the last n lines from the named source.
func (b *Buffer) GetLast(source string, n int) []LogLine {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.bufs[source]
	if !ok {
		return []LogLine{}
	}
	return r.Last(n)
}
