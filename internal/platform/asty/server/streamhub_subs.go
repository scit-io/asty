package server

import "sync"

// subscriberBuffer is the channel buffer per subscriber. Events that
// don't fit (slow consumer) are dropped silently — losing a tick is
// preferable to back-pressuring the snapshot loop. SSE clients always
// recover from a missed message via the next snapshot or keepalive.
const subscriberBuffer = 16

// snapshotSubscriberBuffer is smaller because snapshots are big and
// emitted on a steady cadence; a slow consumer that lags by more than
// 4 ticks is already in trouble and dropping is fine.
const snapshotSubscriberBuffer = 4

// subscribers is a tiny generic fan-out helper: register channels by
// auto-incrementing ID, deliver values non-blockingly, drop on full
// buffer. We use it three times (snapshots, drain progress, cluster
// events) to avoid maintaining three near-identical mutex+map+nextID
// blocks.
type subscribers[T any] struct {
	mu     sync.Mutex
	chans  map[int]chan T
	nextID int
}

func newSubscribers[T any]() *subscribers[T] {
	return &subscribers[T]{chans: make(map[int]chan T)}
}

// add registers a new subscriber and returns the bare buffered channel
// (so the caller can prime it with an initial value before exposing it
// as receive-only) plus an unregister function. The caller is
// responsible for type-narrowing the channel before returning it to
// outside code.
func (s *subscribers[T]) add(buffer int) (chan T, func()) {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	ch := make(chan T, buffer)
	s.chans[id] = ch
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		if existing, ok := s.chans[id]; ok {
			delete(s.chans, id)
			close(existing)
		}
		s.mu.Unlock()
	}
}

// fanout delivers v to every subscriber, dropping for any whose buffer
// is full.
func (s *subscribers[T]) fanout(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.chans {
		select {
		case ch <- v:
		default:
		}
	}
}
