package asty

import (
	"encoding/json"
	"net/http"
)

// handleNodes returns cluster nodes
func (api *API) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes, err := api.server.clusterState.ListNodes()
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to list nodes", err)
		return
	}

	for _, node := range nodes {
		running := 0
		planned := 0

		for _, svc := range api.server.services {
			allocs, err := api.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == node.ID {
					planned++
					if alloc.Status == "running" {
						running++
					}
				}
			}
		}

		node.AllocationsRunning = running
		node.AllocationsPlanned = planned
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleNodesWithID handles /api/v1/nodes/:id and /api/v1/nodes/:id/action
func (api *API) handleNodesWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/nodes/"):]
	if path == "" {
		api.handleNodes(w, r)
		return
	}

	var nodeID, action string
	for i, ch := range path {
		if ch == '/' {
			nodeID = path[:i]
			action = path[i+1:]
			break
		}
	}
	if nodeID == "" {
		nodeID = path
	}

	if action != "" {
		switch action {
		case "drain":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			api.handleNodeDrain(w, r, nodeID)
			return
		case "drain/status":
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			api.handleNodeDrainStatus(w, r, nodeID)
			return
		case "pause":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"node_id": nodeID,
				"action":  "pause",
				"message": "pause initiated (not yet fully implemented)",
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

	node, err := api.server.clusterState.GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	running := 0
	planned := 0

	for _, svc := range api.server.services {
		allocs, err := api.server.clusterState.ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.NodeID == node.ID {
				planned++
				if alloc.Status == "running" {
					running++
				}
			}
		}
	}

	node.AllocationsRunning = running
	node.AllocationsPlanned = planned

	api.writeJSON(w, http.StatusOK, node)
}

// handleNodeDrain handles POST /api/v1/nodes/:id/drain
func (api *API) handleNodeDrain(w http.ResponseWriter, r *http.Request, nodeID string) {
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Enable = true
	}

	if !req.Enable {
		if err := api.server.drainManager.Resume(nodeID); err != nil {
			api.writeError(w, http.StatusBadRequest, "resume failed", err)
			return
		}
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id": nodeID,
			"status":  "ready",
			"message": "drain cancelled, node resumed",
		})
		return
	}

	status, err := api.server.drainManager.Start(nodeID)
	if err != nil {
		code := http.StatusBadRequest
		if status != nil {
			api.writeJSON(w, http.StatusOK, status)
			return
		}
		api.writeError(w, code, "drain failed", err)
		return
	}

	api.writeJSON(w, http.StatusOK, status)
}

// handleNodeDrainStatus handles GET /api/v1/nodes/:id/drain/status
func (api *API) handleNodeDrainStatus(w http.ResponseWriter, _ *http.Request, nodeID string) {
	status := api.server.drainManager.GetStatus(nodeID)
	if status == nil {
		node, err := api.server.clusterState.GetNode(nodeID)
		if err != nil {
			api.writeError(w, http.StatusNotFound, "node not found", err)
			return
		}
		api.writeJSON(w, http.StatusOK, &DrainStatus{
			NodeID:           nodeID,
			Status:           node.Status,
			TotalAllocations: 0,
			Migrated:         0,
			Remaining:        0,
			Errors:           []string{},
		})
		return
	}
	api.writeJSON(w, http.StatusOK, status)
}
