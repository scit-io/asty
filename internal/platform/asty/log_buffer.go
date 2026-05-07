package asty

import (
	"sync"
)

// LogLine is a single buffered log entry.
type LogLine struct {
	Timestamp int64  `json:"ts"`
	Level     string `json:"level,omitempty"`
	Line      string `json:"line"` // pre-formatted display string
}

// LogBuffer is a per-source ring buffer of recent log lines. Safe for
// concurrent use. Sources: "cluster", "node.{id}", "node.{id}.svc.{svc}".
type LogBuffer struct {
	mu   sync.RWMutex
	bufs map[string][]LogLine
	maxN int
}

func NewLogBuffer(maxN int) *LogBuffer {
	return &LogBuffer{bufs: make(map[string][]LogLine), maxN: maxN}
}

// Append adds a line to the named source buffer, evicting the oldest entry
// when the buffer is full.
func (lb *LogBuffer) Append(source string, line LogLine) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	buf := lb.bufs[source]
	buf = append(buf, line)
	if len(buf) > lb.maxN {
		buf = buf[len(buf)-lb.maxN:]
	}
	lb.bufs[source] = buf
}

// GetLast returns the last n lines from the named source. Returns at most
// maxN entries. Returns an empty slice (never nil) when nothing is buffered.
func (lb *LogBuffer) GetLast(source string, n int) []LogLine {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	buf := lb.bufs[source]
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
