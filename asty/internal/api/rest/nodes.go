package rest

import (
	"encoding/json"
	"net/http"

	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/drainer"
)

// applyAllocCounts fills the in-memory AllocationsRunning/Planned
// counters on a node from a precomputed map (see lookup.go).
func applyAllocCounts(node *types.NodeInfo, counts map[string]allocCounts) {
	c := counts[node.ID]
	node.AllocationsRunning = c.Running
	node.AllocationsPlanned = c.Planned
}

// handleNodes serves GET /nodes — list every node, with allocation
// counts attached. SSE flavour streams the same shape on each tick.
func (api *API) handleNodes(w http.ResponseWriter, r *http.Request) {
	if transportSSE(r) {
		api.streamNodes(w, r)
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
	api.writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleNode serves GET /nodes/{id} — single node detail. SSE flavour
// streams the same node + its per-node metrics + allocations.
func (api *API) handleNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if transportSSE(r) {
		api.streamNode(w, r, nodeID)
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

// handleNodeDrain serves POST /nodes/{id}/drain — start or cancel a
// drain. Body `{"enable": true}` (default) starts; `{"enable":false}`
// resumes the node.
func (api *API) handleNodeDrain(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
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
		api.writeJSON(w, http.StatusOK, map[string]any{
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

// handleNodePause serves POST /nodes/{id}/pause — toggle the node's
// status between NodePaused and NodeReady. Paused nodes keep existing
// allocations running but the scheduler skips them for new placements.
// Body `{"pause": true|false}`; missing body defaults to pause=true.
func (api *API) handleNodePause(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
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
	api.writeJSON(w, http.StatusOK, map[string]any{
		"node_id": nodeID,
		"status":  node.Status,
	})
}

// handleNodeDrainStatus serves GET /nodes/{id}/drain — drain progress
// or the steady-state node status if no drain is running.
func (api *API) handleNodeDrainStatus(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	status := api.ctx.DrainManager().GetStatus(nodeID)
	if status == nil {
		node, err := api.ctx.ClusterState().GetNode(nodeID)
		if err != nil {
			api.writeError(w, http.StatusNotFound, "node not found", err)
			return
		}
		api.writeJSON(w, http.StatusOK, &drainer.DrainStatus{
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
