package asty

import (
	"net/http"
)

// handleAllocations returns service allocations
func (api *API) handleAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceName := r.URL.Query().Get("service")
	nodeID := r.URL.Query().Get("node_id")

	if serviceName != "" {
		allocs, err := api.server.clusterState.ListAllocations(serviceName)
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
		var allAllocs []*ServiceAllocation
		for _, svc := range api.server.services {
			allocs, err := api.server.clusterState.ListAllocations(svc.Name)
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

// handleAllocationWithID handles /api/v1/allocations/:id and /api/v1/allocations/:id/action
func (api *API) handleAllocationWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/allocations/"):]
	if path == "" {
		api.handleAllocations(w, r)
		return
	}

	var allocID, action string
	for i, ch := range path {
		if ch == '/' {
			allocID = path[:i]
			action = path[i+1:]
			break
		}
	}
	if allocID == "" {
		allocID = path
	}

	if action != "" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch action {
		case "restart":
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"allocation_id": allocID,
				"action":        "restart",
				"message":       "restart initiated (not yet fully implemented)",
			})
			return
		case "stop":
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"allocation_id": allocID,
				"action":        "stop",
				"message":       "stop initiated (not yet fully implemented)",
			})
			return
		default:
			api.writeError(w, http.StatusBadRequest, "unknown action", nil)
			return
		}
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	services := api.server.services
	for _, svc := range services {
		allocs, err := api.server.clusterState.ListAllocations(svc.Name)
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
