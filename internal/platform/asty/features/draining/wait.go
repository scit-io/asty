package draining

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/core/types"
)

// drainHealthDeadline — overall budget for "wait for replacement copy
// to be healthy". 2 min covers slow artifact downloads or cold caches;
// past that, something is wrong and the operator should investigate.
const drainHealthDeadline = 2 * time.Minute

// drainHealthPoll — fallback polling cadence for the legacy
// waitForHealthyReplacement path that doesn't know which target node
// to watch. The other waits use event-driven WatchAllocation.
const drainHealthPoll = 200 * time.Millisecond

// drainStopMinSlack — the agent's kill_timeout governs how long Stop
// can run; we add this slack on top before declaring the wait failed.
const drainStopMinSlack = 10 * time.Second

// waitForStopped blocks until the allocation transitions to "stopped"
// or "failed" via KV watch, or the kill_timeout + slack expires.
func (dm *DrainManager) waitForStopped(ctx context.Context, nodeID string, svc *types.ServiceDefinition) error {
	dctx, cancel := context.WithTimeout(ctx, svc.GetKillTimeout()+drainStopMinSlack)
	defer cancel()

	err := dm.deps.GetClusterState().WatchAllocation(dctx, svc.Name, nodeID, func(alloc *types.ServiceAllocation) bool {
		if alloc == nil {
			return true
		}
		return alloc.Status == "stopped" || alloc.Status == "failed"
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if dctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout waiting for %s to stop on %s", svc.Name, nodeID)
	}
	return nil
}

// waitForHealthyOnNode blocks until the allocation on targetNode is
// "running" via KV watch, or drainHealthDeadline expires.
func (dm *DrainManager) waitForHealthyOnNode(ctx context.Context, targetNode string, svc *types.ServiceDefinition) error {
	dctx, cancel := context.WithTimeout(ctx, drainHealthDeadline)
	defer cancel()

	err := dm.deps.GetClusterState().WatchAllocation(dctx, svc.Name, targetNode, func(alloc *types.ServiceAllocation) bool {
		return alloc != nil && alloc.Status == "running"
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if dctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout waiting for healthy replacement on %s", targetNode)
	}
	return nil
}

// waitForHealthyReplacement is the fallback used when placeReplacement
// couldn't pick a target up-front (no nearest peer eligible). We don't
// know which node will host the new copy, so we poll every
// drainHealthPoll until any non-drained node has a "running" copy. The
// audit doc plans to replace this with event-driven WatchAllocations.
func (dm *DrainManager) waitForHealthyReplacement(ctx context.Context, drainedNode string, svc *types.ServiceDefinition) error {
	deadline := time.After(drainHealthDeadline)
	ticker := time.NewTicker(drainHealthPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for healthy replacement")
		case <-ticker.C:
			allocs, err := dm.deps.GetClusterState().ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == drainedNode {
					continue
				}
				if alloc.Status == "running" {
					return nil
				}
			}
		}
	}
}

// publishDrainEvent broadcasts the current drain status on the
// asty.v1.drain.progress NATS subject so streamHub can fan it out to
// SSE clients.
func (dm *DrainManager) publishDrainEvent(status DrainStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = dm.deps.GetNATSConn().Publish("asty.v1.drain.progress", data)
}
