package api

import (
	"net/http"
	"strings"

	"asty/internal/platform/asty/core/types"
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
		case "restart", "stop":
			api.writeError(w, http.StatusNotImplemented, "allocation "+action+" is not implemented", nil)
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
