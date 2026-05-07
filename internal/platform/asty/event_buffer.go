package asty

import (
	"sync"
	"time"
)

// ClusterEvent is a single cluster lifecycle event stored in EventBuffer.
type ClusterEvent struct {
	Timestamp int64  `json:"ts"`
	Type      string `json:"type"`              // scale_up, scale_down, alloc_failed, node_join, node_leave
	Service   string `json:"service,omitempty"` // populated for service-related events
	NodeID    string `json:"node_id,omitempty"`
	Details   string `json:"details,omitempty"` // human-readable reason / extra info
}

func newEvent(typ, service, nodeID, details string) ClusterEvent {
	return ClusterEvent{
		Timestamp: time.Now().Unix(),
		Type:      typ,
		Service:   service,
		NodeID:    nodeID,
		Details:   details,
	}
}

// EventBuffer is a fixed-size ring buffer of ClusterEvents. Thread-safe.
type EventBuffer struct {
	mu   sync.RWMutex
	buf  []ClusterEvent
	maxN int
}

func NewEventBuffer(maxN int) *EventBuffer {
	return &EventBuffer{
		buf:  make([]ClusterEvent, 0, maxN),
		maxN: maxN,
	}
}

// Add appends an event, evicting the oldest entry when the buffer is full.
func (eb *EventBuffer) Add(e ClusterEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.buf = append(eb.buf, e)
	if len(eb.buf) > eb.maxN {
		eb.buf = eb.buf[len(eb.buf)-eb.maxN:]
	}
}

// GetLast returns the last n events (or all if n <= 0 or n >= len).
// Returns a copy; never nil.
func (eb *EventBuffer) GetLast(n int) []ClusterEvent {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if len(eb.buf) == 0 {
		return []ClusterEvent{}
	}
	if n <= 0 || n >= len(eb.buf) {
		out := make([]ClusterEvent, len(eb.buf))
		copy(out, eb.buf)
		return out
	}
	out := make([]ClusterEvent, n)
	copy(out, eb.buf[len(eb.buf)-n:])
	return out
}
