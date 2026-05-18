package rest

import (
	"net/http"
	"time"
)

// handleCluster serves GET /. Returns the cluster overview — either
// as a one-shot JSON snapshot (default) or as an SSE stream of
// snapshots (Accept: text/event-stream).
func (api *API) handleCluster(w http.ResponseWriter, r *http.Request) {
	if transportSSE(r) {
		api.streamCluster(w, r)
		return
	}
	api.fetchClusterJSON(w, r)
}

// fetchClusterJSON returns the cluster's high-level state in JSON.
// Mirrors what the SSE stream emits on every snapshot tick, just
// flattened into a single payload.
func (api *API) fetchClusterJSON(w http.ResponseWriter, _ *http.Request) {
	nodes, _ := api.ctx.ClusterState().ListNodes()
	leaderInfo, _ := api.ctx.LeaderElection().GetLeader()
	isLeader := api.ctx.LeaderElection().IsLeader()

	healthyNodes := 0
	var leaderNodeID string
	now := time.Now()
	for _, node := range nodes {
		if node.IsHealthy(now) {
			healthyNodes++
		}
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
		}
	}
	if leaderNodeID == "" {
		leaderNodeID = leaderInfo.ID
	}

	api.writeJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"leader":        leaderNodeID,
			"leader_ip":     leaderInfo.IP,
			"is_leader":     isLeader,
			"nodes_total":   len(nodes),
			"nodes_healthy": healthyNodes,
		},
		"services": map[string]any{
			"loaded": len(api.ctx.Services()),
		},
	})
}

// handleHealth serves GET /health — liveness probe.
func (api *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	api.writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

// handleMetrics delegates to promhttp.HandlerFor over the private
// Registry initialised in initProm. The Go runtime collector, process
// collector, and asty_* gauges (mirroring the UI's cluster/services
// counters) all live on that registry.
func (api *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	api.promHandler.ServeHTTP(w, r)
}
