package asty

import (
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/observability/events"
)

// Type alias for backward compatibility
type ClusterEvent = types.ClusterEvent

var newEvent = types.NewEvent

// EventBuffer is a fixed-size ring buffer of ClusterEvents. Thread-safe.
type EventBuffer struct {
	inner *events.Buffer
}

func NewEventBuffer(maxN int) *EventBuffer {
	return &EventBuffer{
		inner: events.NewBuffer(maxN),
	}
}

// Add appends an event, evicting the oldest entry when the buffer is full.
func (eb *EventBuffer) Add(e ClusterEvent) {
	eb.inner.Add(e)
}

// GetLast returns the last n events (or all if n <= 0 or n >= len).
func (eb *EventBuffer) GetLast(n int) []ClusterEvent {
	return eb.inner.GetLast(n)
}
