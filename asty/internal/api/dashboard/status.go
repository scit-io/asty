package dashboard

import (
	"asty/asty/internal/api/stream"
	"net/http"
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

// fetchClusterJSON returns the cluster's high-level state in JSON. Reads
// from the streamHub's in-memory snapshot — a sub-millisecond map lookup
// — instead of going to KV.ListNodes / KV.GetLeader on every request.
//
// The previous direct-KV path took >3 s during a leader-kill cascade in
// Phase B and Phase C runs (the asty-cluster ListNodes is a stream-watch
// drain that blocks while the bucket's RAFT group is mid-election), which
// raced the test script's 3 s curl timeout and surfaced as "no leader
// reported — aborting" even when the cluster held a healthy leader a
// second later. The streamHub snapshot is event-driven (WatchNodes +
// WatchAllocations + WatchLeadership via the GetLeader cache), so it
// stays as fresh as the KV does without hitting a slow read path.
func (api *API) fetchClusterJSON(w http.ResponseWriter, _ *http.Request) {
	snap := api.ctx.StreamHub().Snapshot()
	cluster := map[string]any{
		"served_by": api.ctx.Config().NodeID,
	}
	if snap != nil {
		cluster["leader"] = snap.Cluster.Leader
		cluster["leader_ip"] = snap.Cluster.LeaderIP
		cluster["leader_dc"] = snap.Cluster.LeaderDC
		cluster["leader_host"] = snap.Cluster.LeaderHost
		cluster["is_leader"] = snap.Cluster.IsLeader
		cluster["nodes_total"] = snap.Cluster.NodesTotal
		cluster["nodes_healthy"] = snap.Cluster.NodesHealthy
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"cluster": cluster,
		"services": map[string]any{
			"loaded": len(api.ctx.Services()),
		},
	})
}

// /health and /metrics live in their own packages (api/health,
// api/prometheus); this file holds only the cluster overview handlers.
