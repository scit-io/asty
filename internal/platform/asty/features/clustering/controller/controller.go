package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling"

	"github.com/rs/zerolog/log"
)

// CommandDispatcher abstracts the agent RPC.
type CommandDispatcher interface {
	SendStartCommand(nodeID string, svc *types.ServiceDefinition) error
}

// ServiceController is the leader-side reconciliation engine.
type ServiceController struct {
	state      *state.ClusterState
	scheduler  *scheduling.Scheduler
	autoscaler *autoscaling.Autoscaler
	services   []*types.ServiceDefinition
	dispatcher CommandDispatcher

	queue       *Workqueue
	workers     int
	resyncEvery time.Duration

	failureLimit int

	OnEvent func(types.ClusterEvent)
}

// NewServiceController wires the controller.
func NewServiceController(
	st *state.ClusterState,
	sched *scheduling.Scheduler,
	as *autoscaling.Autoscaler,
	services []*types.ServiceDefinition,
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
		state:        st,
		scheduler:    sched,
		autoscaler:   as,
		services:     services,
		dispatcher:   dispatcher,
		queue:        NewWorkqueue(),
		workers:      workers,
		resyncEvery:  resyncEvery,
		failureLimit: 8,
	}
}

// Run starts watchers, periodic resync, and worker goroutines.
func (c *ServiceController) Run(ctx context.Context) {
	log.Info().
		Int("workers", c.workers).
		Dur("resync", c.resyncEvery).
		Msg("controller running")
	defer log.Info().Msg("controller stopped")

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

func (c *ServiceController) reconcile(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	svc := c.findService(key)
	if svc == nil {
		return nil
	}
	if err := c.scheduler.ReconcileService(ctx, svc); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	c.dispatchPending(ctx, svc)
	c.pruneFailed(svc)
	if svc.Type == types.ServiceTypeService {
		c.autoscaleOnce(ctx, svc)
	}
	return nil
}

func (c *ServiceController) findService(name string) *types.ServiceDefinition {
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

func (c *ServiceController) watchAllocsToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]string)
		err := c.state.WatchAllocations(ctx, func(a *types.ServiceAllocation) {
			id := a.ServiceName + "/" + a.NodeID
			if a.Status == "deleted" {
				delete(seen, id)
				c.queue.Add(a.ServiceName)
				return
			}
			prev, had := seen[id]
			seen[id] = a.Status
			if !had || prev != a.Status {
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

func (c *ServiceController) watchNodesToQueue(ctx context.Context) {
	for ctx.Err() == nil {
		seen := make(map[string]string)
		err := c.state.WatchNodes(ctx, func(n *types.NodeInfo) {
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

const startingStuckAfterController = 90 * time.Second

func (c *ServiceController) dispatchPending(ctx context.Context, svc *types.ServiceDefinition) {
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
		_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
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
		err := c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
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
			_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
				if a.Status != "starting" {
					return false
				}
				a.Status = "pending"
				return true
			})
			c.queue.AddRateLimited(svc.Name)
		}
	}
}

func (c *ServiceController) pruneFailed(svc *types.ServiceDefinition) {
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
		if c.OnEvent != nil {
			c.OnEvent(types.NewEvent("alloc_failed", svc.Name, alloc.NodeID,
				fmt.Sprintf("restarts=%d", alloc.Restarts)))
		}
	}
}

func (c *ServiceController) autoscaleOnce(ctx context.Context, svc *types.ServiceDefinition) {
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
	if c.OnEvent != nil {
		target := d.TargetNode
		if d.Action == "scale_down" {
			target = d.RemoveNode
		}
		c.OnEvent(types.NewEvent(d.Action, svc.Name, target, d.Reason))
	}
}
