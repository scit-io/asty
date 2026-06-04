package reconciler

import (
	"context"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// watchRetryDelay — how long to wait before re-establishing a KV watch
// after a transient error. Short enough that a brief NATS hiccup doesn't
// stall reconciliation; long enough that we don't busy-loop on a real
// outage.
const watchRetryDelay = 2 * time.Second

// watchAllocsToQueue subscribes to allocation changes and enqueues the
// owning service for re-reconciliation. We track the last seen status
// per allocation to suppress no-op updates, and the
// AllocStarting→AllocPending transition that the controller itself
// causes during dispatchOne rollbacks.
func (c *ServiceController) watchAllocsToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]types.AllocationStatus)
		err := c.state.WatchAllocations(ctx, func(a *types.ServiceAllocation) {
			id := a.ServiceName + "/" + a.NodeID
			if a.Status == types.AllocDeleted {
				delete(seen, id)
				c.queue.Add(a.ServiceName)
				return
			}
			prev, had := seen[id]
			seen[id] = a.Status
			if !had || prev != a.Status {
				if prev == types.AllocStarting && a.Status == types.AllocPending {
					return
				}
				c.queue.Add(a.ServiceName)
			}
		})
		if ctx.Err() != nil {
			return
		}
		// Re-establish on a clean close too (see watchNodesToQueue) — exiting
		// would silently stop reacting to allocation changes until the resync.
		if err != nil {
			log.Error().Err(err).Msg("alloc watcher errored, re-establishing")
			select {
			case <-ctx.Done():
				return
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

// watchNodesToQueue enqueues every service when any node's status
// changes — node membership affects placement for all services.
func (c *ServiceController) watchNodesToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]types.NodeStatus)
		err := c.state.WatchNodes(ctx, func(n *types.NodeInfo) {
			if n.Status == types.NodeDeleted {
				delete(seen, n.ID)
				c.enqueueAllServices()
				return
			}
			prev, had := seen[n.ID]
			seen[n.ID] = n.Status
			if !had || prev != n.Status {
				c.enqueueAllServices()
			}
		})
		if ctx.Err() != nil {
			return
		}
		// WatchNodes returned while the ctx is alive — an error, or a clean
		// close (the KV watcher's channel closes when its consumer is
		// disrupted during churn). RE-ESTABLISH rather than exit: a dead watch
		// would silently stop re-enqueuing services on membership changes
		// until the periodic resync, so placement would lag every join and
		// leave. The fresh watch replays node.* so nothing is missed.
		if err != nil {
			log.Error().Err(err).Msg("node watcher errored, re-establishing")
			select {
			case <-ctx.Done():
				return
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

// periodicResync is the safety net: even if every watcher works, we
// re-enqueue all services every resyncEvery to catch any drift between
// in-memory expectations and KV reality.
func (c *ServiceController) periodicResync(ctx context.Context) {
	t := time.NewTicker(c.resyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.enqueueAllServices()
		}
	}
}
