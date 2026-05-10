package server

import (
	"context"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// streamHub is the single source of truth for SSE handlers. One goroutine
// refreshes a complete cluster snapshot on a fixed interval; SSE handlers
// subscribe and read from the latest snapshot instead of independently
// querying the KV store on every tick.
type streamHub struct {
	server   *Server
	interval time.Duration

	idx *allocIndex

	mu       sync.RWMutex
	snapshot *types.ClusterSnapshot

	subsMu sync.Mutex
	subs   map[int]chan *types.ClusterSnapshot
	nextID int

	drainSubsMu sync.Mutex
	drainSubs   map[int]chan []byte
	drainNextID int

	eventSubsMu sync.Mutex
	eventSubs   map[int]chan []byte
	eventNextID int
}

func newStreamHub(server *Server, interval time.Duration) *streamHub {
	return &streamHub{
		server:    server,
		interval:  interval,
		idx:       newAllocIndex(),
		subs:      make(map[int]chan *types.ClusterSnapshot),
		drainSubs: make(map[int]chan []byte),
		eventSubs: make(map[int]chan []byte),
	}
}

// Run is the hub's main loop: subscribes to drain events, seeds allocIndex
// via KV Watch with history replay, then refreshes on changes and periodically.
func (h *streamHub) Run(ctx context.Context) {
	drainSub, err := h.server.nc.Subscribe("asty.v1.drain.progress", func(msg *nats.Msg) {
		h.fanoutDrain(msg.Data)
	})
	if err != nil {
		log.Error().Err(err).Msg("streamHub: failed to subscribe drain events")
	} else {
		defer drainSub.Unsubscribe()
	}

	notify := make(chan struct{}, 1)
	triggerRefresh := func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}

	nodesReady := make(chan struct{}, 1)
	allocsReady := make(chan struct{}, 1)

	go func() {
		for ctx.Err() == nil {
			err := h.server.clusterState.WatchNodesInit(ctx,
				func(n *types.NodeInfo) {
					isNew := !h.idx.hasNode(n.ID)
					isDel := n.Status == "deleted"
					h.idx.onNode(n)
					triggerRefresh()
					if isDel {
						h.server.addClusterEvent(types.NewEvent("node_leave", "", n.ID, ""))
					} else if isNew {
						h.server.addClusterEvent(types.NewEvent("node_join", "", n.ID, n.Datacenter))
					}
				},
				func() {
					select {
					case nodesReady <- struct{}{}:
					default:
					}
				},
			)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Warn().Err(err).Msg("streamHub: node watcher error, retrying")
				time.Sleep(2 * time.Second)
			}
		}
	}()

	go func() {
		for ctx.Err() == nil {
			err := h.server.clusterState.WatchAllocationsInit(ctx,
				func(a *types.ServiceAllocation) { h.idx.onAlloc(a) },
				func() {
					select {
					case allocsReady <- struct{}{}:
					default:
					}
				},
			)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Warn().Err(err).Msg("streamHub: alloc watcher error, retrying")
				time.Sleep(2 * time.Second)
			}
		}
	}()

	select {
	case <-ctx.Done():
		return
	case <-nodesReady:
	}
	select {
	case <-ctx.Done():
		return
	case <-allocsReady:
	}

	h.refresh()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	debounceCh := make(chan struct{}, 1)
	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			if debounceTimer == nil {
				debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
					select {
					case debounceCh <- struct{}{}:
					default:
					}
				})
			} else {
				debounceTimer.Reset(500 * time.Millisecond)
			}
		case <-debounceCh:
			h.refresh()
		case <-ticker.C:
			h.refresh()
		}
	}
}

func (h *streamHub) refresh() {
	snap := h.buildSnapshot()
	h.mu.Lock()
	h.snapshot = snap
	h.mu.Unlock()
	h.fanout(snap)
}

// Snapshot returns the latest cluster snapshot.
func (h *streamHub) Snapshot() *types.ClusterSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshot
}

// Subscribe returns a channel that receives cluster snapshots and an unsubscribe function.
func (h *streamHub) Subscribe() (<-chan *types.ClusterSnapshot, func()) {
	ch := make(chan *types.ClusterSnapshot, 4)

	h.subsMu.Lock()
	id := h.nextID
	h.nextID++
	h.subs[id] = ch
	h.subsMu.Unlock()

	if snap := h.Snapshot(); snap != nil {
		ch <- snap
	}

	return ch, func() {
		h.subsMu.Lock()
		if existing, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(existing)
		}
		h.subsMu.Unlock()
	}
}

// SubscribeDrain returns a channel for drain progress events.
func (h *streamHub) SubscribeDrain() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	h.drainSubsMu.Lock()
	id := h.drainNextID
	h.drainNextID++
	h.drainSubs[id] = ch
	h.drainSubsMu.Unlock()

	return ch, func() {
		h.drainSubsMu.Lock()
		if existing, ok := h.drainSubs[id]; ok {
			delete(h.drainSubs, id)
			close(existing)
		}
		h.drainSubsMu.Unlock()
	}
}

// SubscribeEvents returns a channel for cluster event notifications.
func (h *streamHub) SubscribeEvents() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.eventSubsMu.Lock()
	id := h.eventNextID
	h.eventNextID++
	h.eventSubs[id] = ch
	h.eventSubsMu.Unlock()
	return ch, func() {
		h.eventSubsMu.Lock()
		if existing, ok := h.eventSubs[id]; ok {
			delete(h.eventSubs, id)
			close(existing)
		}
		h.eventSubsMu.Unlock()
	}
}

func (h *streamHub) fanout(snap *types.ClusterSnapshot) {
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- snap:
		default:
		}
	}
}

func (h *streamHub) fanoutDrain(data []byte) {
	h.drainSubsMu.Lock()
	defer h.drainSubsMu.Unlock()
	for _, ch := range h.drainSubs {
		select {
		case ch <- data:
		default:
		}
	}
}

// FanoutEvent marshals the event and sends to all event subscribers.
func (h *streamHub) FanoutEvent(e types.ClusterEvent) {
	data := types.MustJSON(e)
	h.eventSubsMu.Lock()
	defer h.eventSubsMu.Unlock()
	for _, ch := range h.eventSubs {
		select {
		case ch <- data:
		default:
		}
	}
}
