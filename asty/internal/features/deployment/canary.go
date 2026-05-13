package deployment

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// deployHealthPollInterval — how often we re-check health during the
// canary and rolling phases. Phase 6.3 will replace this with
// WatchAllocations event-driven waits.
const deployHealthPollInterval = 5 * time.Second

// deployCanary updates the first plan.UpdateStrategy.Canary allocations to the target
// version, then waits until every canary is "running" and stays
// healthy for plan.MinHealthyTime, capped by plan.HealthyDeadline.
//
// Returns (true, nil) when the canary is healthy, (false, nil) when
// the deadline expired without healthy state, (_, err) on transport
// or KV errors.
func (d *Deployer) deployCanary(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus) (bool, error) {
	if plan.UpdateStrategy.Canary <= 0 {
		return true, nil
	}
	log.Info().
		Str("service", plan.ServiceName).
		Int("count", plan.UpdateStrategy.Canary).
		Msg("deploying canary")

	canaryAllocs := plan.Allocations[:min(plan.UpdateStrategy.Canary, len(plan.Allocations))]
	for _, alloc := range canaryAllocs {
		if err := d.markPending(plan, alloc); err != nil {
			return false, err
		}
		if err := d.sendUpdateCommand(alloc.NodeID, plan); err != nil {
			return false, fmt.Errorf("failed to send update command: %w", err)
		}
	}
	return d.waitForBatchHealth(ctx, canaryAllocs, plan), nil
}

// markPending atomically advances an allocation's status to "pending"
// at the new version — that's the signal for the agent to (re)start
// the process.
func (d *Deployer) markPending(plan *DeploymentPlan, alloc *types.ServiceAllocation) error {
	version := plan.TargetVersion
	return d.clusterState.MutateAllocation(plan.ServiceName, alloc.NodeID, func(a *types.ServiceAllocation) bool {
		a.Version = version
		a.Status = types.AllocPending
		return true
	})
}
