package deployer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"

	"github.com/rs/zerolog/log"
)

// jsonMarshal aliases encoding/json.Marshal so the persistLast
// publish path uses the same JSON encoding as drain.progress —
// deploy.progress and drain.progress are intentionally JSON, not
// CBOR, because SSE clients decode JSON natively.
var jsonMarshal = json.Marshal

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
	if len(d.history) >= historyCap {
		d.history = d.history[1:]
	}
	d.history = append(d.history, record)
	d.mu.Unlock()
	d.persistLast()
}

// ApplyRemoteRecord merges a deployment record received via NATS
// (subject asty.v1.deploy.progress.<service>) into the local history
// ring. If a record with the same ID already exists, its fields are
// overwritten so progressing status reflects on every node; otherwise
// the record is appended. Without this every server's GET /deploy
// returned only the locally-initiated runs — followers showed an
// empty history table even though deploys had completed elsewhere.
func (d *Deployer) ApplyRemoteRecord(rec DeploymentRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.history {
		if d.history[i].ID == rec.ID {
			d.history[i] = rec
			return
		}
	}
	if len(d.history) >= historyCap {
		d.history = d.history[1:]
	}
	d.history = append(d.history, rec)
}

func (d *Deployer) updateLastRecord(status State, progress int) {
	d.mu.Lock()
	if len(d.history) == 0 {
		d.mu.Unlock()
		return
	}
	last := &d.history[len(d.history)-1]
	last.Status = status
	last.Progress = progress
	if status != StateRunning {
		last.CompletedAt = time.Now()
	}
	d.mu.Unlock()
	d.persistLast()
}

// persistLast writes the most recent DeploymentRecord to KV at
// `service.<name>.deployment` AND publishes a JSON copy on
// `asty.v1.deploy.progress.<service>` so SSE-subscribed dashboards
// see live progress without polling KV. Both writes are best-effort
// — failure is logged at warn, the in-memory ring stays authoritative
// for the dashboard.
func (d *Deployer) persistLast() {
	d.mu.Lock()
	if len(d.history) == 0 {
		d.mu.Unlock()
		return
	}
	rec := d.history[len(d.history)-1]
	d.mu.Unlock()

	if payload, err := kv.MarshalDeploymentRecord(&rec); err != nil {
		log.Warn().Err(err).Str("service", rec.Service).Msg("deployment: marshal for KV persistence failed")
	} else if err := d.clusterState.PutDeployment(rec.Service, payload); err != nil {
		log.Warn().Err(err).Str("service", rec.Service).Msg("deployment: KV persist failed")
	}

	if d.nc == nil {
		return
	}
	jsonPayload, err := jsonMarshal(&rec)
	if err != nil {
		log.Warn().Err(err).Str("service", rec.Service).Msg("deployment: marshal for NATS publish failed")
		return
	}
	subject := "asty.v1.deploy.progress." + rec.Service
	if err := d.nc.Publish(subject, jsonPayload); err != nil {
		log.Warn().Err(err).Str("subject", subject).Msg("deployment: NATS publish failed")
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
			d.recordRollbackStep(alloc.NodeID, plan, "mark_pending", err)
			return d.markRollbackFailed(status, fmt.Errorf("rollback markPending failed for %s/%s: %w", alloc.ServiceName, alloc.NodeID, err))
		}
		d.recordRollbackStep(alloc.NodeID, plan, "mark_pending", nil)
		if err := d.sendUpdateCommand(alloc.NodeID, plan, plan.CurrentVersion); err != nil {
			d.recordRollbackStep(alloc.NodeID, plan, "send_update", err)
			return d.markRollbackFailed(status, fmt.Errorf("rollback sendUpdate failed for %s/%s: %w", alloc.ServiceName, alloc.NodeID, err))
		}
		d.recordRollbackStep(alloc.NodeID, plan, "send_update", nil)
	}

	if !d.waitForBatchHealth(ctx, touched, plan) {
		d.recordRollbackStep("", plan, "wait_health", fmt.Errorf("HealthyDeadline expired"))
		return d.markRollbackFailed(status, fmt.Errorf("rollback batch did not become healthy within HealthyDeadline"))
	}
	d.recordRollbackStep("", plan, "wait_health", nil)

	status.Status = StateReverted
	status.EndTime = time.Now()
	d.updateLastRecord(StateReverted, 0)
	d.pinVersion(plan.ServiceName, types.ServiceVersion{Current: plan.CurrentVersion})
	log.Info().Str("service", status.ServiceName).Msg("deployment rolled back successfully")
	return status, fmt.Errorf("deployment reverted: %s", reason)
}

// recordRollbackStep appends one entry to the active deployment record's
// RollbackSteps audit trail. NodeID is empty for the final batch-wait
// verdict (it applies to the whole touched set, not a single alloc).
func (d *Deployer) recordRollbackStep(nodeID string, plan *DeploymentPlan, action string, err error) {
	step := RollbackStep{
		Timestamp: time.Now(),
		NodeID:    nodeID,
		FromVer:   plan.TargetVersion,
		ToVer:     plan.CurrentVersion,
		Action:    action,
		Outcome:   "ok",
	}
	if err != nil {
		step.Outcome = "error"
		step.Error = err.Error()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.history) == 0 {
		return
	}
	last := &d.history[len(d.history)-1]
	last.RollbackSteps = append(last.RollbackSteps, step)
}

// markRollbackFailed finalises the deployment in RollbackFailed state.
// The service is left in mixed-version limbo, and the per-service
// RollbackFailed flag is set in KV so the autoscaler refuses to act
// on this service until the operator clears it. The deployer logs
// loudly because nothing else will — there is no automatic recovery
// path from here.
func (d *Deployer) markRollbackFailed(status *DeploymentStatus, err error) (*DeploymentStatus, error) {
	log.Error().
		Err(err).
		Str("service", status.ServiceName).
		Msg("rollback failed — service in mixed-version state, operator intervention required")
	if flagErr := d.clusterState.SetRollbackFailed(status.ServiceName, true); flagErr != nil {
		log.Warn().Err(flagErr).Str("service", status.ServiceName).Msg("failed to set rollback_failed flag in KV")
	}
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
