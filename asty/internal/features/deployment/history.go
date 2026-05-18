package deployment

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// historyCap — how many past deployments are kept in memory. Old
// records roll off oldest-first when capacity is reached. Operators
// usually want recent history for debugging; long-term auditing is
// expected to come from logs, not this in-memory ring.
const historyCap = 100

// beginRecord adds a new StateRunning entry to history at the start of a
// deployment.
func (d *Deployer) beginRecord(plan *DeploymentPlan) {
	record := DeploymentRecord{
		ID:        fmt.Sprintf("deploy-%s-%d", plan.ServiceName, time.Now().UnixNano()),
		Service:   plan.ServiceName,
		Version:   plan.TargetVersion,
		Strategy:  "rolling",
		Status:    StateRunning,
		StartedAt: time.Now(),
		Progress:  0,
	}
	if plan.UpdateStrategy.Canary > 0 {
		record.Strategy = "canary"
	}
	d.addRecord(record)
}

func (d *Deployer) addRecord(record DeploymentRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.history) >= historyCap {
		d.history = d.history[1:]
	}
	d.history = append(d.history, record)
}

func (d *Deployer) updateLastRecord(status State, progress int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.history) == 0 {
		return
	}
	last := &d.history[len(d.history)-1]
	last.Status = status
	last.Progress = progress
	if status != StateRunning {
		last.CompletedAt = time.Now()
	}
}

// GetHistory returns deployment history, newest-first. Capped at
// historyCap entries.
func (d *Deployer) GetHistory() []DeploymentRecord {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]DeploymentRecord, len(d.history))
	copy(out, d.history)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// revertDeployment performs an actual rollback: for every allocation
// dispatched at TargetVersion during this run, re-dispatch it at
// plan.CurrentVersion and wait for the batch to come back to healthy
// state. On success the deployment is recorded as Reverted; on failure
// the deployment is recorded as RollbackFailed and an error is
// returned — operator intervention is required in that case, and the
// service is in mixed-version limbo.
//
// Empty plan.CurrentVersion is treated as a fatal: there is no version
// to roll back to. This happens only when the caller pointed Deploy at
// an empty cluster (no prior version), which means there is nothing
// to undo anyway — Failed is the correct terminal state.
func (d *Deployer) revertDeployment(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus, reason string) (*DeploymentStatus, error) {
	touched := d.touchedSnapshot()

	log.Warn().
		Str("service", status.ServiceName).
		Str("reason", reason).
		Str("from", plan.TargetVersion).
		Str("to", plan.CurrentVersion).
		Int("allocs", len(touched)).
		Msg("reverting deployment")

	status.Phase = PhaseRevert
	status.Error = reason

	if plan.CurrentVersion == "" || plan.CurrentVersion == "unknown" {
		log.Error().
			Str("service", status.ServiceName).
			Msg("revert requested but no previous version is known — failing instead")
		return d.failDeployment(status, fmt.Errorf("revert requested but plan.CurrentVersion is empty: %s", reason))
	}

	if len(touched) == 0 {
		log.Info().Str("service", status.ServiceName).Msg("nothing was dispatched yet, marking as reverted without action")
		status.Status = StateReverted
		status.EndTime = time.Now()
		d.updateLastRecord(StateReverted, 0)
		return status, fmt.Errorf("deployment reverted: %s", reason)
	}

	for _, alloc := range touched {
		if err := d.markPending(plan, alloc, plan.CurrentVersion); err != nil {
			return d.markRollbackFailed(status, fmt.Errorf("rollback markPending failed for %s/%s: %w", alloc.ServiceName, alloc.NodeID, err))
		}
		if err := d.sendUpdateCommand(alloc.NodeID, plan, plan.CurrentVersion); err != nil {
			return d.markRollbackFailed(status, fmt.Errorf("rollback sendUpdate failed for %s/%s: %w", alloc.ServiceName, alloc.NodeID, err))
		}
	}

	if !d.waitForBatchHealth(ctx, touched, plan) {
		return d.markRollbackFailed(status, fmt.Errorf("rollback batch did not become healthy within HealthyDeadline"))
	}

	status.Status = StateReverted
	status.EndTime = time.Now()
	d.updateLastRecord(StateReverted, 0)
	log.Info().Str("service", status.ServiceName).Msg("deployment rolled back successfully")
	return status, fmt.Errorf("deployment reverted: %s", reason)
}

// markRollbackFailed finalises the deployment in RollbackFailed state.
// The service is left in mixed-version limbo and the orchestrator
// should refuse to autoscale it until operator clears the flag.
func (d *Deployer) markRollbackFailed(status *DeploymentStatus, err error) (*DeploymentStatus, error) {
	log.Error().
		Err(err).
		Str("service", status.ServiceName).
		Msg("rollback failed — service in mixed-version state, operator intervention required")
	status.Status = StateRollbackFailed
	status.Error = err.Error()
	status.EndTime = time.Now()
	d.updateLastRecord(StateRollbackFailed, 0)
	return status, err
}

// failDeployment finalises a failed deployment. The progress field is
// computed from how far we got before the failure to give operators a
// sense of where things broke.
func (d *Deployer) failDeployment(status *DeploymentStatus, err error) (*DeploymentStatus, error) {
	status.Status = StateFailed
	status.Error = err.Error()
	status.EndTime = time.Now()
	d.updateLastRecord(StateFailed, status.Updated*100/max(status.Total, 1))

	log.Error().
		Err(err).
		Str("service", status.ServiceName).
		Msg("deployment failed")
	return status, err
}
