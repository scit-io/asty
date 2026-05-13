package controller

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// startingStuckAfter — if an allocation has been in "starting" for at
// least this long without becoming "running", we revert it to "pending"
// and try again. The agent's normal start path takes seconds; 90 s gives
// generous slack for slow artifact downloads on first deploy.
const startingStuckAfter = 90 * time.Second

// reconcile is the per-key work unit: bring scheduling up to date,
// dispatch any pending allocations to agents, prune permanently failed
// ones, and drive autoscaler decisions for non-system services.
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

// dispatchPending walks svc's allocations, unsticks ones stuck in
// "starting" for too long, and turns "pending" into "starting" plus a
// start-command RPC to the agent.
func (c *ServiceController) dispatchPending(ctx context.Context, svc *types.ServiceDefinition) {
	allocs, err := c.state.ListAllocations(svc.Name)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("list allocations failed")
		return
	}

	c.unstickStarting(svc, allocs)

	allocs, err = c.state.ListAllocations(svc.Name)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("re-list allocations failed")
		return
	}
	for _, alloc := range allocs {
		if alloc.Status != types.AllocPending || ctx.Err() != nil {
			continue
		}
		c.dispatchOne(svc, alloc.NodeID)
	}
}

// unstickStarting reverts allocations stuck in "starting" past
// startingStuckAfter back to "pending" so reconcile can retry the start
// command.
func (c *ServiceController) unstickStarting(svc *types.ServiceDefinition, allocs []*types.ServiceAllocation) {
	now := time.Now()
	for _, alloc := range allocs {
		if alloc.Status != types.AllocStarting || now.Sub(alloc.UpdatedAt) < startingStuckAfter {
			continue
		}
		nodeID := alloc.NodeID
		_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
			if a.Status != types.AllocStarting || time.Since(a.UpdatedAt) < startingStuckAfter {
				return false
			}
			log.Warn().
				Str("service", svc.Name).
				Str("node_id", nodeID).
				Dur("stuck_for", time.Since(a.UpdatedAt)).
				Msg("alloc stuck in starting, reverting to pending")
			a.Status = types.AllocPending
			return true
		})
	}
}

// dispatchOne advances a single pending allocation to "starting" via CAS
// and sends the start command. On failure the status is rolled back so
// the next reconcile pass picks it up again.
func (c *ServiceController) dispatchOne(svc *types.ServiceDefinition, nodeID string) {
	var advanced bool
	err := c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
		if a.Status != types.AllocPending {
			return false
		}
		a.Status = types.AllocStarting
		advanced = true
		return true
	})
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Str("node_id", nodeID).Msg("guard transition failed")
		return
	}
	if !advanced {
		return
	}

	log.Info().Str("service", svc.Name).Str("node_id", nodeID).Msg("sending start command to agent")
	if err := c.dispatch(nodeID, svc); err != nil {
		log.Error().Err(err).
			Str("service", svc.Name).
			Str("node_id", nodeID).
			Msg("start command failed")
		_ = c.state.MutateAllocation(svc.Name, nodeID, func(a *types.ServiceAllocation) bool {
			if a.Status != types.AllocStarting {
				return false
			}
			a.Status = types.AllocPending
			return true
		})
		c.queue.AddRateLimited(svc.Name)
	}
}

// pruneFailed deletes allocations whose ConsecutiveFailures has hit the
// service's restart-attempts threshold. The scheduler will pick a fresh
// node on the next reconcile pass.
func (c *ServiceController) pruneFailed(svc *types.ServiceDefinition) {
	allocs, err := c.state.ListAllocations(svc.Name)
	if err != nil {
		return
	}
	threshold := svc.Restart.GetAttempts()
	for _, alloc := range allocs {
		if alloc.Status != types.AllocFailed || alloc.ConsecutiveFailures < threshold {
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
