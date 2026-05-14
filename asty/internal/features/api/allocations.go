package api

import (
	"net/http"

	"asty/asty/internal/core/types"
)

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
	if wantsSSE(r) {
		api.streamAllocation(w, r, allocID)
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
// /nodes/{id}/allocations/{allocId}/restart. Stops the existing
// process and resets the alloc to pending so the controller
// re-dispatches a start command to the same node. Live copy count
// stays at N.
func (api *API) handleAllocationRestart(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	alloc := api.allocByID(allocID)
	if alloc == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	if err := api.ctx.StopServiceOnNode(alloc.NodeID, alloc.ServiceName); err != nil {
		api.writeError(w, http.StatusInternalServerError, "stop dispatch failed", err)
		return
	}
	err := api.ctx.ClusterState().MutateAllocation(alloc.ServiceName, alloc.NodeID, func(a *types.ServiceAllocation) bool {
		a.Status = types.AllocPending
		a.PID = 0
		a.ConsecutiveFailures = 0
		return true
	})
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "mutate allocation failed", err)
		return
	}
	api.ctx.ReconcileService(alloc.ServiceName)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"allocation_id": allocID,
		"status":        types.AllocPending,
	})
}

// handleAllocationStop serves POST
// /nodes/{id}/allocations/{allocId}/stop. Terminates the process and
// deletes the allocation record. The scheduler will pick a fresh node
// on the next reconcile if the service is still below its target copy
// count — useful for moving a workload off a noisy neighbour. To stop
// permanently, scale the service down or drain the node.
func (api *API) handleAllocationStop(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	alloc := api.allocByID(allocID)
	if alloc == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	if err := api.ctx.StopServiceOnNode(alloc.NodeID, alloc.ServiceName); err != nil {
		api.writeError(w, http.StatusInternalServerError, "stop dispatch failed", err)
		return
	}
	if err := api.ctx.ClusterState().DeleteAllocation(alloc.ServiceName, alloc.NodeID); err != nil {
		api.writeError(w, http.StatusInternalServerError, "delete allocation failed", err)
		return
	}
	api.ctx.ReconcileService(alloc.ServiceName)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"allocation_id": allocID,
		"status":        "deleted",
	})
}
