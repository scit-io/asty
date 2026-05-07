package asty

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ServiceController is the leader-side reconciliation engine. It watches
// alloc and node KV, maps each event onto the affected service-name keys,
// and processes those keys through a workqueue. Reconcile is idempotent and
// covers four phases per service:
//
//   1. Scheduler.ReconcileService — top up to MinCopies, geo-spread.
//   2. dispatchPending — flip pending→starting and send start commands.
//   3. pruneFailed — drop allocs that exhausted Restart.Attempts.
//   4. autoscale — grow/shrink based on metrics (services only).
//
// Workers pull a service-name key, run all four phases for that one service,
// then Done. Multiple workers may run concurrently for different services
// without coordination because per-service KV writes are CAS-guarded and
// per-service alloc keys (alloc.<svc>.<node>) don't overlap across services.
type ServiceController struct {
	state      *ClusterState
	scheduler  *Scheduler
	autoscaler *Autoscaler
	services   []*ServiceDefinition
	dispatcher CommandDispatcher

	queue       *Workqueue
	workers     int
	resyncEvery time.Duration

	// failureLimit caps how many consecutive errors a key can accrue before
	// we drop it from the rate-limited retry path. Until then errors keep
	// bouncing through AddRateLimited with exponential backoff.
	failureLimit int

	// onEvent is called after significant lifecycle events (scale, failure).
	// May be nil — callers must nil-guard.
	onEvent func(ClusterEvent)
}

// CommandDispatcher abstracts the agent RPC. Lets the controller live without
// pulling in *Server (which depends on the autoscaler that depends on the
// controller-friendly types — circular import otherwise).
type CommandDispatcher interface {
	SendStartCommand(nodeID string, svc *ServiceDefinition) error
}

// NewServiceController wires the controller. workers <=0 defaults to 2;
// resyncEvery <=0 defaults to 60s.
func NewServiceController(
	state *ClusterState,
	scheduler *Scheduler,
	autoscaler *Autoscaler,
	services []*ServiceDefinition,
	dispatcher CommandDispatcher,
	workers int,
	resyncEvery time.Duration,
) *ServiceController {
	if workers <= 0 {
		workers = 2
	}
	if resyncEvery <= 0 {
		resyncEvery = 60 * time.Second
	}
	return &ServiceController{
		state:        state,
		scheduler:    scheduler,
		autoscaler:   autoscaler,
		services:     services,
		dispatcher:   dispatcher,
		queue:        NewWorkqueue(),
		workers:      workers,
		resyncEvery:  resyncEvery,
		failureLimit: 8,
	}
}

// Run starts watchers, periodic resync, and worker goroutines. Blocks until
// ctx is cancelled (typically on loss of leadership). On exit the workqueue
// is drained: in-flight reconciles complete, queued ones are discarded.
func (c *ServiceController) Run(ctx context.Context) {
	log.Info().
		Int("workers", c.workers).
		Dur("resync", c.resyncEvery).
		Msg("controller running")
	defer log.Info().Msg("controller stopped")

	// Initial resync — process every service once on startup so we don't
	// have to wait for a watcher event to converge state.
	c.enqueueAllServices()

	go c.watchAllocsToQueue(ctx)
	go c.watchNodesToQueue(ctx)
	go c.periodicResync(ctx)

	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c.runWorker(ctx, id)
		}(i)
	}

	<-ctx.Done()
	c.queue.ShutDown()
	wg.Wait()
}

// runWorker is a long-lived goroutine that pulls keys and reconciles. On
// reconcile error the key is rate-limited-requeued; on success Forget clears
// the backoff counter.
func (c *ServiceController) runWorker(ctx context.Context, id int) {
	for {
		key, ok := c.queue.Get()
		if !ok {
			return
		}
		err := c.reconcile(ctx, key)
		if err != nil {
			log.Error().Err(err).Str("key", key).Int("worker", id).Msg("reconcile failed, requeuing with backoff")
			c.queue.AddRateLimited(key)
		} else {
			c.queue.Forget(key)
		}
		c.queue.Done(key)
	}
}

// reconcile runs all four phases for the service named by key. Each phase
// is independently idempotent so partial failures (e.g. dispatch fails for
// one node) are handled by the next pass; Reconcile itself returns the
// scheduler error since that's the foundational step.
func (c *ServiceController) reconcile(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return nil // shutting down — don't requeue
	}
	svc := c.findService(key)
	if svc == nil {
		// Unknown service key (probably a watcher event for a removed
		// service). Drop silently — Forget will reset any backoff.
		return nil
	}
	if err := c.scheduler.ReconcileService(ctx, svc); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	c.dispatchPending(ctx, svc)
	c.pruneFailed(svc)
	if svc.Type == ServiceTypeService {
		c.autoscaleOnce(ctx, svc)
	}
	return nil
}

func (c *ServiceController) findService(name string) *ServiceDefinition {
	for _, s := range c.services {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (c *ServiceController) enqueueAllServices() {
	for _, svc := range c.services {
		c.queue.Add(svc.Name)
	}
}

// watchAllocsToQueue subscribes to alloc.* and enqueues the affected service
// only on transitions that warrant leader work. Metric-only updates (CPU,
// memory) and identical-status replays are dropped — they'd otherwise wake
// the controller multiple times per second per running allocation.
func (c *ServiceController) watchAllocsToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]string) // svc/node -> last status
		err := c.state.WatchAllocations(ctx, func(a *ServiceAllocation) {
			id := a.ServiceName + "/" + a.NodeID
			if a.Status == "deleted" {
				delete(seen, id)
				c.queue.Add(a.ServiceName)
				return
			}
			prev, had := seen[id]
			seen[id] = a.Status
			if !had || prev != a.Status {
				// Skip only starting→pending: this is dispatchPending's own
				// revert after a failed SendStartCommand. It already called
				// AddRateLimited — enqueuing here bypasses that rate limit
				// and creates a tight retry loop.
				// NOTE: node reordering in the UI ("nodes jumping") is a separate
				// frontend sorting issue unrelated to this filter — see realtime-plan.md Задача 4.
				if prev == "starting" && a.Status == "pending" {
					return
				}
				c.queue.Add(a.ServiceName)
			}
		})
		if ctx.Err() != nil || err == nil {
			return
		}
		log.Error().Err(err).Msg("alloc watcher errored, retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// watchNodesToQueue subscribes to node.* and enqueues every service when a
// node joins, leaves, or flips status. Heartbeat-only updates (LastSeen
// ticks) are filtered out.
func (c *ServiceController) watchNodesToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]string) // node ID -> last status
		err := c.state.WatchNodes(ctx, func(n *NodeInfo) {
			if n.Status == "deleted" {
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
		if ctx.Err() != nil || err == nil {
			return
		}
		log.Error().Err(err).Msg("node watcher errored, retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// periodicResync re-enqueues every service every resyncEvery as a safety
// net. Covers any KV event that slipped past a watcher reconnect, and gives
// the autoscaler a regular evaluation cadence (autoscale is metric-driven,
// not state-driven, so it doesn't fire from watchers alone).
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

// startingStuckAfterController is how long an alloc can sit in `starting`
// before we assume the agent never got around to flipping it to running and
// retry. Same value as the previous server-side constant.
const startingStuckAfterController = 90 * time.Second

// dispatchPending flips pending→starting and sends start commands. CAS guards
// every transition so concurrent agent writes (status=running with PID) win
// when they should. A first pass also unsticks `starting` allocs older than
// startingStuckAfterController so a crashed agent doesn't wedge a slot.
func (c *ServiceController) dispatchPending(ctx context.Context, svc *ServiceDefinition) {
	allocs, err := c.state.ListAllocations(svc.Name)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("list allocations failed")
		return
	}

	now := time.Now()
	for _, alloc := range allocs {
		if alloc.Status != "starting" {
			continue
		}
		if now.Sub(alloc.UpdatedAt) < startingStuckAfterController {
			continue
		}
		nodeID := alloc.NodeID
		_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *ServiceAllocation) bool {
			if a.Status != "starting" {
				return false
			}
			if time.Since(a.UpdatedAt) < startingStuckAfterController {
				return false
			}
			log.Warn().
				Str("service", svc.Name).
				Str("node_id", nodeID).
				Dur("stuck_for", time.Since(a.UpdatedAt)).
				Msg("alloc stuck in starting, reverting to pending")
			a.Status = "pending"
			return true
		})
	}

	allocs, err = c.state.ListAllocations(svc.Name)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("re-list allocations failed")
		return
	}

	for _, alloc := range allocs {
		if alloc.Status != "pending" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return
		}
		nodeID := alloc.NodeID

		var advanced bool
		err := c.state.MutateAllocation(svc.Name, nodeID, func(a *ServiceAllocation) bool {
			if a.Status != "pending" {
				return false
			}
			a.Status = "starting"
			advanced = true
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", svc.Name).Str("node_id", nodeID).Msg("guard transition failed")
			continue
		}
		if !advanced {
			continue
		}

		log.Info().
			Str("service", svc.Name).
			Str("node_id", nodeID).
			Msg("sending start command to agent")
		if err := c.dispatcher.SendStartCommand(nodeID, svc); err != nil {
			log.Error().Err(err).
				Str("service", svc.Name).
				Str("node_id", nodeID).
				Msg("start command failed")
			_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *ServiceAllocation) bool {
				if a.Status != "starting" {
					return false
				}
				a.Status = "pending"
				return true
			})
			// Re-queue the service with backoff so we don't spin on a node
			// whose agent is dead.
			c.queue.AddRateLimited(svc.Name)
		}
	}
}

// pruneFailed drops allocations the agent gave up on. The threshold is the
// service's own restart.attempts.
func (c *ServiceController) pruneFailed(svc *ServiceDefinition) {
	allocs, err := c.state.ListAllocations(svc.Name)
	if err != nil {
		return
	}
	threshold := svc.Restart.GetAttempts()
	for _, alloc := range allocs {
		if alloc.Status != "failed" || alloc.ConsecutiveFailures < threshold {
			continue
		}
		log.Warn().
			Str("service", svc.Name).
			Str("node_id", alloc.NodeID).
			Int("restarts", alloc.Restarts).
			Int("threshold", threshold).
			Msg("pruning permanently failed allocation")
		if err := c.state.DeleteAllocation(svc.Name, alloc.NodeID); err != nil {
			log.Error().Err(err).Msg("delete failed allocation")
			continue
		}
		if c.onEvent != nil {
			c.onEvent(newEvent("alloc_failed", svc.Name, alloc.NodeID,
				fmt.Sprintf("restarts=%d", alloc.Restarts)))
		}
	}
}

func (c *ServiceController) autoscaleOnce(ctx context.Context, svc *ServiceDefinition) {
	d, err := c.autoscaler.EvaluateService(ctx, svc)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("autoscaler evaluate failed")
		return
	}
	if d.Action == "none" {
		return
	}
	log.Info().
		Str("service", svc.Name).
		Str("action", d.Action).
		Str("reason", d.Reason).
		Msg("autoscaling decision")
	if err := c.autoscaler.ExecuteScalingDecision(ctx, d, svc); err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("autoscaler execute failed")
		return
	}
	if c.onEvent != nil {
		target := d.TargetNode
		if d.Action == "scale_down" {
			target = d.RemoveNode
		}
		c.onEvent(newEvent(d.Action, svc.Name, target, d.Reason))
	}
}
