package deployment

import (
	"context"
	"fmt"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/rs/zerolog/log"
)

// rollingUpdate updates the remaining allocations (after the canary
// batch, if any) in waves of plan.MaxParallel. Each wave waits for
// every member to be healthy before the next one starts.
func (d *Deployer) rollingUpdate(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus) error {
	startIdx := 0
	if plan.Canary > 0 {
		startIdx = min(plan.Canary, len(plan.Allocations))
		status.Updated = startIdx
	}
	remaining := plan.Allocations[startIdx:]

	log.Info().
		Str("service", plan.ServiceName).
		Int("remaining", len(remaining)).
		Int("max_parallel", plan.MaxParallel).
		Msg("starting rolling update")

	for i := 0; i < len(remaining); i += plan.MaxParallel {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(i+plan.MaxParallel, len(remaining))
		batch := remaining[i:end]

		log.Info().
			Str("service", plan.ServiceName).
			Int("batch", len(batch)).
			Int("progress", status.Updated).
			Int("total", status.Total).
			Msg("updating batch")

		if err := d.dispatchBatch(plan, batch); err != nil {
			return err
		}
		if !d.waitForBatchHealth(ctx, batch, plan) {
			return fmt.Errorf("batch update failed health check")
		}
		status.Updated += len(batch)

		// Short pause between batches so observers can see staged
		// progress; not a correctness requirement.
		time.Sleep(plan.MinHealthyTime)
	}
	return nil
}

func (d *Deployer) dispatchBatch(plan *DeploymentPlan, batch []*types.ServiceAllocation) error {
	for _, alloc := range batch {
		if err := d.markPending(plan, alloc); err != nil {
			return fmt.Errorf("failed to update allocation: %w", err)
		}
		if err := d.sendUpdateCommand(alloc.NodeID, plan.ServiceName, plan.TargetVersion); err != nil {
			return fmt.Errorf("failed to send update command: %w", err)
		}
	}
	return nil
}
