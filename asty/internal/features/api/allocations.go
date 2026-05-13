package api

import (
	"net/http"
	"strings"

	"asty/asty/internal/core/types"
)

// handleAllocations returns service allocations.
func (api *API) handleAllocations(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	serviceName := r.URL.Query().Get("service")
	nodeID := r.URL.Query().Get("node_id")

	if serviceName != "" {
		allocs, err := api.ctx.ClusterState().ListAllocations(serviceName)
		if err != nil {
			api.writeError(w, http.StatusInternalServerError, "failed to list allocations", err)
			return
		}

		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"service":     serviceName,
			"allocations": allocs,
			"count":       len(allocs),
		})
		return
	}

	if nodeID != "" {
		var allAllocs []*types.ServiceAllocation
		for _, svc := range api.ctx.Services() {
			allocs, err := api.ctx.ClusterState().ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == nodeID {
					allAllocs = append(allAllocs, alloc)
				}
			}
		}

		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id":     nodeID,
			"allocations": allAllocs,
			"count":       len(allAllocs),
		})
		return
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "use ?service=<name> or ?node_id=<id> to get allocations",
	})
}

// handleAllocationWithID handles /api/v1/allocations/:id and /api/v1/allocations/:id/action.
func (api *API) handleAllocationWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/allocations/"):]
	if path == "" {
		api.handleAllocations(w, r)
		return
	}

	allocID, action, _ := strings.Cut(path, "/")

	if action != "" {
		if !methodGuard(w, r, http.MethodPost) {
			return
		}

		switch action {
		case "restart":
			api.handleAllocationRestart(w, r, allocID)
			return
		case "stop":
			api.handleAllocationStop(w, r, allocID)
			return
		default:
			api.writeError(w, http.StatusBadRequest, "unknown action", nil)
			return
		}
	}

	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	services := api.ctx.Services()
	for _, svc := range services {
		allocs, err := api.ctx.ClusterState().ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.ID == allocID {
				api.writeJSON(w, http.StatusOK, alloc)
				return
			}
		}
	}

	api.writeError(w, http.StatusNotFound, "allocation not found", nil)
}

// handleAllocationRestart stops the existing process and resets the alloc to
// pending so the controller re-dispatches a start command to the same node.
// Live copy count stays at N.
func (api *API) handleAllocationRestart(w http.ResponseWriter, _ *http.Request, allocID string) {
	alloc := api.findAllocByID(allocID)
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
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"allocation_id": allocID,
		"status":        types.AllocPending,
	})
}

// handleAllocationStop terminates the process and deletes the allocation
// record. The scheduler will pick a fresh node on the next reconcile if the
// service is still below its target copy count — useful for moving a workload
// off a noisy neighbour. To stop permanently, scale the service down or drain
// the node.
func (api *API) handleAllocationStop(w http.ResponseWriter, _ *http.Request, allocID string) {
	alloc := api.findAllocByID(allocID)
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
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"allocation_id": allocID,
		"status":        "deleted",
	})
}

// findAllocByID walks every service's allocations and returns the first match
// or nil. Used by per-allocation action handlers (restart, stop).
func (api *API) findAllocByID(allocID string) *types.ServiceAllocation {
	for _, svc := range api.ctx.Services() {
		allocs, err := api.ctx.ClusterState().ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, a := range allocs {
			if a.ID == allocID {
				return a
			}
		}
	}
	return nil
}
