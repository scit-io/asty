package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/core/types"
)

// agentRestartTimeout caps how long we wait for an agent to ack a
// restart command. 30 s mirrors the start-command timeout — generous
// for slow artifact pulls but well under any realistic deployment
// total budget.
const agentRestartTimeout = 30 * time.Second

// waitForBatchHealth ticks every deployHealthPollInterval, accumulates
// time-while-healthy, and returns true once it reaches plan.MinHealthyTime
// before plan.HealthyDeadline expires.
func (d *Deployer) waitForBatchHealth(ctx context.Context, batch []*types.ServiceAllocation, plan *DeploymentPlan) bool {
	deadline := time.Now().Add(plan.HealthyDeadline)
	healthyFor := time.Duration(0)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(deployHealthPollInterval):
			if d.checkAllocationsHealth(batch) {
				healthyFor += deployHealthPollInterval
				if healthyFor >= plan.MinHealthyTime {
					return true
				}
			} else {
				healthyFor = 0
			}
		}
	}
	return false
}

// checkAllocationsHealth returns true only when every allocation in
// allocs is currently in status "running". Any other state, or a KV
// read error, fails the check and resets the healthy timer.
func (d *Deployer) checkAllocationsHealth(allocs []*types.ServiceAllocation) bool {
	for _, alloc := range allocs {
		current, err := d.clusterState.GetAllocation(alloc.ServiceName, alloc.NodeID)
		if err != nil {
			return false
		}
		if current.Status != "running" {
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
