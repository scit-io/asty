package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/draining"
)

// nodeAllocCounts walks every service's allocations once and groups
// (planned, running) counts by node ID. Returning a map (instead of
// computing per-node) keeps complexity at O(allocations) regardless of
// how many nodes are queried afterwards.
func (api *API) nodeAllocCounts() map[string]struct{ Planned, Running int } {
	out := make(map[string]struct{ Planned, Running int })
	for _, svc := range api.ctx.Services() {
		allocs, err := api.ctx.ClusterState().ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, a := range allocs {
			c := out[a.NodeID]
			c.Planned++
			if a.Status == "running" {
				c.Running++
			}
			out[a.NodeID] = c
		}
	}
	return out
}

func applyAllocCounts(node *types.NodeInfo, counts map[string]struct{ Planned, Running int }) {
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
