package server

import "asty/asty/internal/core/types"

// Subscribe returns a channel that receives cluster snapshots and an
// unsubscribe function. The current snapshot (if any) is delivered
// immediately to the new subscriber so SSE clients see state without
// waiting for the next change.
func (h *streamHub) Subscribe() (<-chan *types.ClusterSnapshot, func()) {
	ch, unsub := h.snapSubs.add(snapshotSubscriberBuffer)
	if snap := h.Snapshot(); snap != nil {
		ch <- snap // buffered channel, never blocks
	}
	return ch, unsub
}

// SubscribeDrain returns a channel for drain progress events.
func (h *streamHub) SubscribeDrain() (<-chan []byte, func()) {
	ch, unsub := h.drainSubs.add(subscriberBuffer)
	return ch, unsub
}

// SubscribeEvents returns a channel for cluster event notifications.
func (h *streamHub) SubscribeEvents() (<-chan []byte, func()) {
	ch, unsub := h.eventSubs.add(subscriberBuffer)
	return ch, unsub
}

// FanoutEvent marshals e and delivers it to every event subscriber.
// Slow subscribers drop the event.
func (h *streamHub) FanoutEvent(e types.ClusterEvent) {
	h.eventSubs.fanout(types.MustJSON(e))
}
