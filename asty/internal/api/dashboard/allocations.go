package dashboard

import (
	"context"
	"net/http"
	"time"

	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"
)

// allocStopWaitSlack — extra budget granted to the agent on top of the
// service's kill_timeout before we declare the post-stop wait failed.
// Mirrors drainer.drainStopMinSlack so dashboard-driven stops behave
// identically to drain-driven stops from the operator's POV.
const allocStopWaitSlack = 10 * time.Second

// allocStopWaitFallback — used when the service definition cannot be
// resolved (orphaned alloc, service file removed). The agent's
// process.Stop budget is bounded by kill_timeout which we cannot read
// here, so 60 s is a safe upper bound covering the default kill_timeout
// (30 s) plus slack for SIGKILL + KV update.
const allocStopWaitFallback = 60 * time.Second

// Snapshot-first allocation lookups (allocByID, allocsByNode,
// nodeAllocCounts) live in lookup.go.

// handleNodeAllocations serves GET /nodes/{id}/allocations.
func (api *API) handleNodeAllocations(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	allocs := api.allocsByNode(nodeID)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"node_id":     nodeID,
		"allocations": allocs,
		"count":       len(allocs),
	})
}

// handleAllocation serves GET /nodes/{id}/allocations/{allocId}.
// Falls back to looking up by alloc ID alone if the {id} path slot
// doesn't match the allocation's current node (e.g. UI deep-links a
// stale URL after a migration).
func (api *API) handleAllocation(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	if transportSSE(r) {
		stream.Allocation(api.streamCtx, w, r, allocID)
		return
	}
	alloc := api.allocByID(allocID)
	if alloc == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	api.writeJSON(w, http.StatusOK, alloc)
}

// handleAllocationRestart serves POST
// /nodes/{id}/allocations/{allocId}/restart. Dispatches a synchronous
// CmdRestart to the alloc's agent, which holds the slot in
// AllocRestarting throughout the stop+start so allocation identity and
// node placement are preserved. No KV mutation or reconcile from the
// dashboard side: the agent owns the FSM. Returns when the new copy is
// running, or surfaces the agent's error.
func (api *API) handleAllocationRestart(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	alloc := api.allocByID(allocID)
	if alloc == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	if err := api.ctx.RestartServiceOnNode(alloc.NodeID, alloc.ServiceName); err != nil {
		api.writeError(w, http.StatusInternalServerError, "restart dispatch failed", err)
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"allocation_id": allocID,
		"status":        types.AllocRunning,
	})
}

// handleAllocationStop serves POST
// /nodes/{id}/allocations/{allocId}/stop. Dispatches stop, waits for
// the agent to confirm exit, then deletes the KV record and lets the
// reconciler backfill the copy according to the service's desired
// count. Rejected for system services — those run exactly one per node
// and would be re-created by the scheduler on the next tick.
func (api *API) handleAllocationStop(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	alloc := api.allocByID(allocID)
	if alloc == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	if svc := api.findService(alloc.ServiceName); svc != nil && svc.Type == types.ServiceTypeSystem {
		api.writeError(w, http.StatusConflict,
			"cannot stop a system-service allocation; drain or pause the node instead", nil)
		return
	}
	if !api.stopAndDeleteAllocation(r.Context(), w, alloc.ServiceName, alloc.NodeID) {
		return
	}
	api.ctx.ReconcileService(alloc.ServiceName)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"allocation_id": allocID,
		"status":        "deleted",
	})
}

// stopAndDeleteAllocation dispatches CmdStop, waits for the agent to
// transition the alloc to Stopped/Failed (or for the wait budget to
// expire), then deletes the KV record. Writes its own error response
// on failure and returns false. Shared between the per-alloc stop
// handler and the scale-down victims loop so the two paths cannot drift.
func (api *API) stopAndDeleteAllocation(ctx context.Context, w http.ResponseWriter, serviceName, nodeID string) bool {
	if err := api.ctx.StopServiceOnNode(nodeID, serviceName); err != nil {
		api.writeError(w, http.StatusInternalServerError, "stop dispatch failed", err)
		return false
	}
	waitCtx, cancel := context.WithTimeout(ctx, allocStopWaitBudget(api.findService(serviceName)))
	defer cancel()
	if err := waitForAllocationStopped(waitCtx, api.ctx.ClusterState(), serviceName, nodeID); err != nil {
		api.writeError(w, http.StatusGatewayTimeout, "stop did not confirm", err)
		return false
	}
	if err := api.ctx.ClusterState().DeleteAllocation(serviceName, nodeID); err != nil {
		api.writeError(w, http.StatusInternalServerError, "delete allocation failed", err)
		return false
	}
	return true
}

// allocStopWaitBudget returns how long to wait for an agent to confirm
// a service stop. Uses the service's kill_timeout + slack when the
// definition is reachable, falling back to a conservative constant for
// orphaned allocs whose .asty file is gone.
func allocStopWaitBudget(svc *types.ServiceDefinition) time.Duration {
	if svc != nil {
		return svc.GetKillTimeout() + allocStopWaitSlack
	}
	return allocStopWaitFallback
}

// waitForAllocationStopped blocks until the alloc transitions to
// Stopped or Failed (clean exit / kill_timeout escalation respectively)
// or until ctx is done. Returns nil on terminal state, ctx.Err() on
// timeout.
func waitForAllocationStopped(ctx context.Context, cs *kv.ClusterState, serviceName, nodeID string) error {
	err := cs.WatchAllocation(ctx, serviceName, nodeID, func(alloc *types.ServiceAllocation) bool {
		if alloc == nil {
			return true
		}
		return alloc.Status == types.AllocStopped || alloc.Status == types.AllocFailed
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}
