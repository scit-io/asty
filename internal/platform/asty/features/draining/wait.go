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
		return alloc.Status == types.AllocStopped || alloc.Status == types.AllocFailed
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
		return alloc != nil && alloc.Status == types.AllocRunning
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

// waitForHealthyReplacement is used when placeReplacement couldn't
// pick a target up-front (no nearest peer eligible) and let the
// controller place the new copy. We don't know which node will host it,
// so we subscribe to allocation changes service-wide and exit on the
// first event reporting "running" on a node other than drainedNode.
//
// First we check current state — the replacement might already exist
// before our watcher is set up (NATS Watch replays history but we
// don't want to depend on that for correctness).
func (dm *DrainManager) waitForHealthyReplacement(ctx context.Context, drainedNode string, svc *types.ServiceDefinition) error {
	cs := dm.deps.GetClusterState()
	if dm.healthyReplacementExists(cs, svc.Name, drainedNode) {
		return nil
	}

	dctx, cancel := context.WithTimeout(ctx, drainHealthDeadline)
	defer cancel()

	found := make(chan struct{}, 1)
	go func() {
		_ = cs.WatchAllocations(dctx, func(a *types.ServiceAllocation) {
			if a.ServiceName != svc.Name {
				return
			}
			if a.NodeID == drainedNode {
				return
			}
			if a.Status != types.AllocRunning {
				return
			}
			select {
			case found <- struct{}{}:
			default:
			}
		})
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-dctx.Done():
		if dctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout waiting for healthy replacement")
		}
		return dctx.Err()
	case <-found:
		return nil
	}
}

// healthyReplacementExists is the pre-watcher fast path: if the
// replacement already exists in KV, return immediately without
// spinning up a watcher.
func (dm *DrainManager) healthyReplacementExists(cs allocLister, serviceName, drainedNode string) bool {
	allocs, err := cs.ListAllocations(serviceName)
	if err != nil {
		return false
	}
	for _, a := range allocs {
		if a.NodeID == drainedNode {
			continue
		}
		if a.Status == types.AllocRunning {
			return true
		}
	}
	return false
}

// allocLister is the small contract waitForHealthyReplacement needs
// from cluster state. Defining it locally avoids importing the full
// state package interface for one method.
type allocLister interface {
	ListAllocations(serviceName string) ([]*types.ServiceAllocation, error)
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
