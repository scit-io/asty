package events

import (
	"sync"

	"asty/internal/platform/asty/core/types"
)

// Buffer is a fixed-size ring buffer of ClusterEvents. Thread-safe.
type Buffer struct {
	mu   sync.RWMutex
	buf  []types.ClusterEvent
	maxN int
}

func NewBuffer(maxN int) *Buffer {
	return &Buffer{
		buf:  make([]types.ClusterEvent, 0, maxN),
		maxN: maxN,
	}
}

// Add appends an event, evicting the oldest entry when the buffer is full.
func (b *Buffer) Add(e types.ClusterEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, e)
	if len(b.buf) > b.maxN {
		b.buf = b.buf[len(b.buf)-b.maxN:]
	}
}

// GetLast returns the last n events (or all if n <= 0 or n >= len).
func (b *Buffer) GetLast(n int) []types.ClusterEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.buf) == 0 {
		return []types.ClusterEvent{}
	}
	if n <= 0 || n >= len(b.buf) {
		out := make([]types.ClusterEvent, len(b.buf))
		copy(out, b.buf)
		return out
	}
	out := make([]types.ClusterEvent, n)
	copy(out, b.buf[len(b.buf)-n:])
	return out
}
