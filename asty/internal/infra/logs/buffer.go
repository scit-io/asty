package logs

import (
	"sync"

	"asty/asty/internal/core/util/ringbuf"
)

// Buffer is a per-source ring of recent structured Events. Safe for
// concurrent use. Source keys mirror the SSE routing the dashboard
// hands out: "cluster", "node.{id}", "node.{id}.svc.{svc}".
type Buffer struct {
	mu   sync.RWMutex
	bufs map[string]*ringbuf.Ring[Event]
	maxN int
}

func NewBuffer(maxN int) *Buffer {
	return &Buffer{bufs: make(map[string]*ringbuf.Ring[Event]), maxN: maxN}
}

// Append stores e under source, evicting the oldest entry once the
// per-source ring is full.
func (b *Buffer) Append(source string, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.bufs[source]
	if !ok {
		r = ringbuf.New[Event](b.maxN)
		b.bufs[source] = r
	}
	r.Push(e)
}

// GetLast returns the last n events from the named source, oldest
// first.
func (b *Buffer) GetLast(source string, n int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.bufs[source]
	if !ok {
		return []Event{}
	}
	return r.Last(n)
}
