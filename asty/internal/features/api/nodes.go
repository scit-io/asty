package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/draining"
)

// nodeAllocCounts lives in lookup.go (snapshot-first lookups).

func applyAllocCounts(node *types.NodeInfo, counts map[string]allocCounts) {
	c := counts[node.ID]
	node.AllocationsRunning = c.Running
	node.AllocationsPlanned = c.Planned
}

// handleNodes returns cluster nodes.
func (api *API) handleNodes(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nodes, err := api.ctx.ClusterState().ListNodes()
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to list nodes", err)
		return
	}

	counts := api.nodeAllocCounts()
	for _, node := range nodes {
		applyAllocCounts(node, counts)
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleNodesWithID handles /api/v1/nodes/:id and /api/v1/nodes/:id/action.
func (api *API) handleNodesWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/nodes/"):]
	if path == "" {
		api.handleNodes(w, r)
		return
	}

	nodeID, action, _ := strings.Cut(path, "/")

	if action != "" {
		switch action {
		case "drain":
			if !methodGuard(w, r, http.MethodPost) {
				return
			}
			api.handleNodeDrain(w, r, nodeID)
			return
		case "drain/status":
			if !methodGuard(w, r, http.MethodGet) {
				return
			}
			api.handleNodeDrainStatus(w, r, nodeID)
			return
		case "pause":
			if !methodGuard(w, r, http.MethodPost) {
				return
			}
			api.handleNodePause(w, r, nodeID)
			return
		default:
			api.writeError(w, http.StatusBadRequest, "unknown action", nil)
			return
		}
	}

	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	node, err := api.ctx.ClusterState().GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	applyAllocCounts(node, api.nodeAllocCounts())
	api.writeJSON(w, http.StatusOK, node)
}

// handleNodeDrain handles POST /api/v1/nodes/:id/drain.
func (api *API) handleNodeDrain(w http.ResponseWriter, r *http.Request, nodeID string) {
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Enable = true
	}

	if !req.Enable {
		if err := api.ctx.DrainManager().Resume(nodeID); err != nil {
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

	status, err := api.ctx.DrainManager().Start(nodeID)
	if err != nil {
		if status != nil {
			api.writeJSON(w, http.StatusOK, status)
			return
		}
		api.writeError(w, http.StatusBadRequest, "drain failed", err)
		return
	}

	api.writeJSON(w, http.StatusOK, status)
}

// handleNodePause toggles a node's status between NodePaused and NodeReady.
// Paused nodes keep existing allocations running but the scheduler skips
// them for new placements (FilterHealthyNodes excludes non-Ready statuses).
// Request body: `{"pause": true|false}`; missing body defaults to pause=true.
func (api *API) handleNodePause(w http.ResponseWriter, r *http.Request, nodeID string) {
	var req struct {
		Pause bool `json:"pause"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Pause = true
	}

	node, err := api.ctx.ClusterState().GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}
	if node.Status == types.NodeDown {
		api.writeError(w, http.StatusBadRequest, "node is down", nil)
		return
	}
	if node.Status == types.NodeDraining || node.Status == types.NodeDrained {
		api.writeError(w, http.StatusBadRequest, "node is draining/drained; resume drain first", nil)
		return
	}

	if req.Pause {
		node.Status = types.NodePaused
	} else {
		node.Status = types.NodeReady
	}
	if err := api.ctx.ClusterState().UpdateNode(node); err != nil {
		api.writeError(w, http.StatusInternalServerError, "update node failed", err)
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"node_id": nodeID,
		"status":  node.Status,
	})
}

// handleNodeDrainStatus handles GET /api/v1/nodes/:id/drain/status.
func (api *API) handleNodeDrainStatus(w http.ResponseWriter, _ *http.Request, nodeID string) {
	status := api.ctx.DrainManager().GetStatus(nodeID)
	if status == nil {
		node, err := api.ctx.ClusterState().GetNode(nodeID)
		if err != nil {
			api.writeError(w, http.StatusNotFound, "node not found", err)
			return
		}
		api.writeJSON(w, http.StatusOK, &draining.DrainStatus{
			NodeID:           nodeID,
			Status:           string(node.Status),
			TotalAllocations: 0,
			Migrated:         0,
			Remaining:        0,
			Errors:           []string{},
		})
		return
	}
	api.writeJSON(w, http.StatusOK, status)
}
