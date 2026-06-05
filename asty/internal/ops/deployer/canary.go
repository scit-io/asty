package deployer

import (
	"context"
	"fmt"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

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
		if err := d.markPending(plan, alloc, plan.TargetVersion); err != nil {
			return false, err
		}
		if err := d.sendUpdateCommand(alloc.NodeID, plan, plan.TargetVersion); err != nil {
			return false, fmt.Errorf("failed to send update command: %w", err)
		}
		d.recordTouched(alloc)
	}
	return d.waitForBatchHealth(ctx, canaryAllocs, plan), nil
}

// deployCanaryWithRetries wraps deployCanary with the CanaryRetries
// budget. Each retry re-dispatches the canary batch and waits for
// health; the loop exits the first time health passes or the budget
// is exhausted. Errors from dispatch propagate immediately because
// they signal transport/KV failures unlikely to clear by retrying.
func (d *Deployer) deployCanaryWithRetries(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus) (bool, error) {
	attempts := plan.UpdateStrategy.CanaryRetries + 1 // initial try + retries
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		ok, err := d.deployCanary(ctx, plan, status)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
		if i < attempts-1 {
			log.Warn().
				Str("service", plan.ServiceName).
				Int("attempt", i+1).
				Int("budget", attempts).
				Msg("canary unhealthy, retrying within budget")
		}
	}
	return false, nil
}

// markPending atomically pins an allocation to the given version and
// sets status to Pending — that's the signal for the agent to (re)start
// the process. Used by both forward dispatch (TargetVersion) and
// rollback (CurrentVersion).
func (d *Deployer) markPending(plan *DeploymentPlan, alloc *types.ServiceAllocation, version string) error {
	return d.clusterState.MutateAllocation(plan.ServiceName, alloc.NodeID, func(a *types.ServiceAllocation) bool {
		a.Version = version
		a.Status = types.AllocPending
		return true
	})
}
