package server

import (
	"context"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Run is the hub's main loop. It subscribes to drain events, seeds the
// alloc/node index from KV (waiting for the initial replay so the first
// snapshot is complete), then maintains snapshots reactively (on
// debounced KV events) with a periodic safety-net rebuild.
func (h *streamHub) Run(ctx context.Context) {
	if drainSub, err := h.server.nc.Subscribe("asty.v1.drain.progress", func(msg *nats.Msg) {
		h.drainSubs.fanout(msg.Data)
	}); err != nil {
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

	nodesReady, allocsReady := h.startWatchers(ctx, triggerRefresh)

	if !waitFor(ctx, nodesReady) || !waitFor(ctx, allocsReady) {
		return
	}

	h.refresh()
	h.driveLoop(ctx, notify)
}

// driveLoop is the post-replay state machine: every snapshot rebuild
// happens here, either via debounced notify or via the safety-net
// ticker.
func (h *streamHub) driveLoop(ctx context.Context, notify <-chan struct{}) {
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
				debounceTimer = time.AfterFunc(snapshotDebounce, func() {
					select {
					case debounceCh <- struct{}{}:
					default:
					}
				})
			} else {
				debounceTimer.Reset(snapshotDebounce)
			}
		case <-debounceCh:
			h.refresh()
		case <-ticker.C:
			h.refresh()
		}
	}
}

// startWatchers spawns the node and allocation KV watchers and returns
// channels that close once the initial history replay completes. The
// watchers retry on transient errors and exit cleanly when ctx ends.
func (h *streamHub) startWatchers(ctx context.Context, triggerRefresh func()) (nodesReady, allocsReady <-chan struct{}) {
	nodesC := make(chan struct{}, 1)
	allocsC := make(chan struct{}, 1)

	go h.watchNodes(ctx, nodesC, triggerRefresh)
	go h.watchAllocs(ctx, allocsC)

	return nodesC, allocsC
}

func (h *streamHub) watchNodes(ctx context.Context, ready chan<- struct{}, triggerRefresh func()) {
	for ctx.Err() == nil {
		err := h.server.clusterState.WatchNodesInit(ctx,
			func(n *types.NodeInfo) {
				isNew := !h.idx.hasNode(n.ID)
				isDel := n.Status == types.NodeDeleted
				h.idx.onNode(n)
				triggerRefresh()
				switch {
				case isDel:
					h.server.addClusterEvent(types.NewEvent("node_leave", "", n.ID, ""))
				case isNew:
					h.server.addClusterEvent(types.NewEvent("node_join", "", n.ID, n.Datacenter))
				}
			},
			func() {
				select {
				case ready <- struct{}{}:
				default:
				}
			},
		)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Msg("streamHub: node watcher error, retrying")
			time.Sleep(watcherRetryDelay)
		}
	}
}

func (h *streamHub) watchAllocs(ctx context.Context, ready chan<- struct{}) {
	for ctx.Err() == nil {
		err := h.server.clusterState.WatchAllocationsInit(ctx,
			func(a *types.ServiceAllocation) { h.idx.onAlloc(a) },
			func() {
				select {
				case ready <- struct{}{}:
				default:
				}
			},
		)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Msg("streamHub: alloc watcher error, retrying")
			time.Sleep(watcherRetryDelay)
		}
	}
}

// waitFor blocks until ch fires or ctx is cancelled. Returns true if
// the signal arrived in time.
func waitFor(ctx context.Context, ch <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ch:
		return true
	}
}
