package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/asty/internal/core/types"
)

// agentRestartTimeout caps how long we wait for an agent to ack a
// restart command. 30 s mirrors the start-command timeout — generous
// for slow artifact pulls but well under any realistic deployment
// total budget.
const agentRestartTimeout = 30 * time.Second

// allocWatcher is the small interface the deployer needs from
// ClusterState — listing initial state plus reacting to changes. The
// real ClusterState satisfies it; tests can supply a stub.
type allocWatcher interface {
	GetAllocation(serviceName, nodeID string) (*types.ServiceAllocation, error)
	WatchAllocations(ctx context.Context, onChange func(*types.ServiceAllocation)) error
}

// waitForBatchHealth blocks until either:
//   - every allocation in batch has been status="running" continuously
//     for plan.UpdateStrategy.MinHealthyTime (returns true), or
//   - plan.UpdateStrategy.HealthyDeadline elapses without that condition (returns false).
//
// It reacts to KV change events instead of polling — this is the path
// the audit's Phase 6.3 requires. Falls back gracefully when the state
// accessor doesn't expose a watcher.
func (d *Deployer) waitForBatchHealth(ctx context.Context, batch []*types.ServiceAllocation, plan *DeploymentPlan) bool {
	w, ok := d.clusterState.(allocWatcher)
	if !ok {
		return d.waitForBatchHealthPolling(ctx, batch, plan)
	}
	return waitBatchEventDriven(ctx, w, batch, plan)
}

// waitForBatchHealthPolling is the legacy path used when the state
// accessor doesn't support watching (tests with stubs). Kept here for
// safety; the real implementation always takes the event-driven path.
func (d *Deployer) waitForBatchHealthPolling(ctx context.Context, batch []*types.ServiceAllocation, plan *DeploymentPlan) bool {
	deadline := time.Now().Add(plan.UpdateStrategy.HealthyDeadline)
	healthyFor := time.Duration(0)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(deployHealthPollInterval):
			if d.checkAllocationsHealth(batch) {
				healthyFor += deployHealthPollInterval
				if healthyFor >= plan.UpdateStrategy.MinHealthyTime {
					return true
				}
			} else {
				healthyFor = 0
			}
		}
	}
	return false
}

// waitBatchEventDriven implements the event-driven half of
// waitForBatchHealth. Pulled into a free function so the algorithm is
// testable without a Deployer instance.
func waitBatchEventDriven(ctx context.Context, w allocWatcher, batch []*types.ServiceAllocation, plan *DeploymentPlan) bool {
	keys := batchKeys(batch)
	tracker := newHealthTracker(keys, plan.UpdateStrategy.MinHealthyTime)

	// Seed with current state — events that arrived before we started
	// watching are still reflected in KV reads.
	for _, a := range batch {
		if cur, err := w.GetAllocation(a.ServiceName, a.NodeID); err == nil {
			tracker.update(allocKey(a), cur.Status)
		}
	}
	if tracker.healthy() {
		// Already healthy at the start; we still need to confirm it
		// stays healthy for MinHealthyTime.
		tracker.markHealthyNow()
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, plan.UpdateStrategy.HealthyDeadline)
	defer cancel()

	updates := make(chan struct{}, 1)
	go w.WatchAllocations(deadlineCtx, func(a *types.ServiceAllocation) {
		k := allocKey(a)
		if !keys[k] {
			return
		}
		tracker.update(k, a.Status)
		select {
		case updates <- struct{}{}:
		default:
		}
	})

	for {
		// Each select cycle re-checks the timer the tracker computed.
		until := tracker.until()
		var timerCh <-chan time.Time
		if until > 0 {
			t := time.NewTimer(until)
			timerCh = t.C
			defer t.Stop()
		}

		select {
		case <-deadlineCtx.Done():
			return false
		case <-ctx.Done():
			return false
		case <-updates:
			// Loop body re-evaluates tracker.until below.
		case <-timerCh:
			if tracker.satisfied() {
				return true
			}
		}
	}
}

// checkAllocationsHealth returns true only when every allocation in
// allocs is currently in status "running". Used by the polling
// fallback; the event-driven path keeps the same definition of
// "healthy" via healthTracker.allRunningLocked.
func (d *Deployer) checkAllocationsHealth(allocs []*types.ServiceAllocation) bool {
	for _, alloc := range allocs {
		current, err := d.clusterState.GetAllocation(alloc.ServiceName, alloc.NodeID)
		if err != nil {
			return false
		}
		if current.Status != types.AllocRunning {
			return false
		}
	}
	return true
}

// sendUpdateCommand asks the agent at nodeID to restart the service at
// the new version. Currently the agent has no "restart" handler;
// Phase 6.4 of the audit covers either implementing it or returning
// a 501 here.
func (d *Deployer) sendUpdateCommand(nodeID, serviceName, version string) error {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", nodeID)
	cmd := types.Command{
		Type: "restart",
		Data: []byte(fmt.Sprintf(`{"service_name":%q,"version":%q}`, serviceName, version)),
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	_, err = d.nc.Request(subject, data, agentRestartTimeout)
	return err
}
