package events

import (
	"sync"

	"asty/asty/internal/core/types"
	"asty/asty/internal/core/util/ringbuf"
)

// Buffer is a fixed-size ring buffer of ClusterEvents. Thread-safe.
type Buffer struct {
	mu  sync.RWMutex
	buf *ringbuf.Ring[types.ClusterEvent]
}

func NewBuffer(maxN int) *Buffer {
	return &Buffer{buf: ringbuf.New[types.ClusterEvent](maxN)}
}

// Add appends an event, evicting the oldest entry when the buffer is full.
func (b *Buffer) Add(e types.ClusterEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Push(e)
}

// GetLast returns the last n events (or all if n <= 0 or n >= Len).
func (b *Buffer) GetLast(n int) []types.ClusterEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buf.Last(n)
}
