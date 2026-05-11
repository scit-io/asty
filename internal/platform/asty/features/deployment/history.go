package deployment

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// historyCap — how many past deployments are kept in memory. Old
// records roll off oldest-first when capacity is reached. Operators
// usually want recent history for debugging; long-term auditing is
// expected to come from logs, not this in-memory ring.
const historyCap = 100

// beginRecord adds a new "running" entry to history at the start of a
// deployment.
func (d *Deployer) beginRecord(plan *DeploymentPlan) {
	record := DeploymentRecord{
		ID:        fmt.Sprintf("deploy-%s-%d", plan.ServiceName, time.Now().UnixNano()),
		Service:   plan.ServiceName,
		Version:   plan.TargetVersion,
		Strategy:  "rolling",
		Status:    "running",
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

func (d *Deployer) updateLastRecord(status string, progress int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.history) == 0 {
		return
	}
	last := &d.history[len(d.history)-1]
	last.Status = status
	last.Progress = progress
	if status != "running" {
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

// revertDeployment marks a deployment as reverted in both history and
// the live status. The actual rollback (downgrading allocations back
// to the previous version) is not implemented — see refactoring-audit.md.
func (d *Deployer) revertDeployment(status *DeploymentStatus, reason string) (*DeploymentStatus, error) {
	log.Warn().
		Str("service", status.ServiceName).
		Str("reason", reason).
		Msg("reverting deployment")

	status.Status = "reverted"
	status.Phase = "revert"
	status.Error = reason
	status.EndTime = time.Now()
	d.updateLastRecord("reverted", 0)
	return status, fmt.Errorf("deployment reverted: %s", reason)
}

// failDeployment finalises a failed deployment. The progress field is
// computed from how far we got before the failure to give operators a
// sense of where things broke.
func (d *Deployer) failDeployment(status *DeploymentStatus, err error) (*DeploymentStatus, error) {
	status.Status = "failed"
	status.Error = err.Error()
	status.EndTime = time.Now()
	d.updateLastRecord("failed", status.Updated*100/max(status.Total, 1))

	log.Error().
		Err(err).
		Str("service", status.ServiceName).
		Msg("deployment failed")
	return status, err
}
