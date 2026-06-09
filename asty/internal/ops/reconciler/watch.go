package reconciler

import (
	"context"
	"time"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
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
// causes during dispatchOne rollbacks. The watch is re-established on a
// clean churn-close too — exiting would silently stop reacting until the
// periodic resync.
func (c *ServiceController) watchAllocsToQueue(ctx context.Context) {
	netutil.RetryWatchForever(ctx, "reconciler-allocs", watchRetryDelay, func(ctx context.Context) error {
		seen := make(map[string]types.AllocationStatus)
		return c.state.WatchAllocations(ctx, func(a *types.ServiceAllocation) {
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
	})
}

// watchNodesToQueue enqueues every service when any node's status
// changes — node membership affects placement for all services. Like
// watchAllocsToQueue, re-establishes on both errors and clean closes so
// placement doesn't lag a join/leave that lands during NATS churn.
func (c *ServiceController) watchNodesToQueue(ctx context.Context) {
	netutil.RetryWatchForever(ctx, "reconciler-nodes", watchRetryDelay, func(ctx context.Context) error {
		seen := make(map[string]types.NodeStatus)
		return c.state.WatchNodes(ctx, func(n *types.NodeInfo) {
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
	})
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
