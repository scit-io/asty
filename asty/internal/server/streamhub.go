package server

import (
	"sync"
	"time"

	"asty/asty/internal/core/types"
)

// snapshotDebounce — when watchers see a burst of changes, we wait this
// long after the last one before rebuilding a snapshot. This collapses
// hundreds of "alloc updated" events into a single rebuild.
const snapshotDebounce = 500 * time.Millisecond

// watcherRetryDelay — back-off when a KV watcher errors out before
// re-establishing.
const watcherRetryDelay = 2 * time.Second

// streamHub is the single source of truth for SSE handlers. One
// goroutine maintains the in-memory cluster index from KV watchers and
// fans out a fresh snapshot on changes (debounced) and on a periodic
// safety-net interval. SSE handlers subscribe and read from snapshots
// instead of independently querying KV on every tick.
type streamHub struct {
	server   *Server
	interval time.Duration

	idx *allocIndex

	mu       sync.RWMutex
	snapshot *types.ClusterSnapshot

	snapSubs  *subscribers[*types.ClusterSnapshot]
	drainSubs *subscribers[[]byte]
	eventSubs *subscribers[[]byte]
}

func newStreamHub(server *Server, interval time.Duration) *streamHub {
	return &streamHub{
		server:    server,
		interval:  interval,
		idx:       newAllocIndex(),
		snapSubs:  newSubscribers[*types.ClusterSnapshot](),
		drainSubs: newSubscribers[[]byte](),
		eventSubs: newSubscribers[[]byte](),
	}
}

// Snapshot returns the latest cluster snapshot, or nil if the hub
// hasn't built one yet.
func (h *streamHub) Snapshot() *types.ClusterSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshot
}

// refresh rebuilds the snapshot from the in-memory index and fans it
// out to subscribers. Called from the debouncer and the safety-net
// ticker.
func (h *streamHub) refresh() {
	snap := h.buildSnapshot()
	h.mu.Lock()
	h.snapshot = snap
	h.mu.Unlock()
	h.snapSubs.fanout(snap)
}
