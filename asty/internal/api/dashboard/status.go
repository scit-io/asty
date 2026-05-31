package dashboard

import (
	"asty/asty/internal/api/stream"
	"net/http"
	"time"
)

// handleCluster serves GET /. Returns the cluster overview — either
// as a one-shot JSON snapshot (default) or as an SSE stream of
// snapshots (Accept: text/event-stream).
func (api *API) handleCluster(w http.ResponseWriter, r *http.Request) {
	if transportSSE(r) {
		stream.Cluster(api.streamCtx, w, r)
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
	var leaderNodeID, leaderDC, leaderHost string
	leaderHost = leaderInfo.Host
	now := time.Now()
	for _, node := range nodes {
		if node.IsHealthy(now) {
			healthyNodes++
		}
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
			leaderDC = node.Datacenter
			// Freshest Host comes from the leader's current heartbeat
			// (NodeInfo.Host), not the KV-stored leader.Host snapshot.
			if node.Host != "" {
				leaderHost = node.Host
			}
		}
	}
	if leaderNodeID == "" {
		leaderNodeID = leaderInfo.ID
	}

	api.writeJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"leader":        leaderNodeID,
			"leader_ip":     leaderInfo.IP,
			"leader_dc":     leaderDC,
			"leader_host":   leaderHost,
			"is_leader":     isLeader,
			"nodes_total":   len(nodes),
			"nodes_healthy": healthyNodes,
			"served_by":     api.ctx.Config().NodeID,
		},
		"services": map[string]any{
			"loaded": len(api.ctx.Services()),
		},
	})
}

// /health and /metrics live in their own packages (api/health,
// api/prometheus); this file holds only the cluster overview handlers.
