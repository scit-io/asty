package rest

import (
	"net/http"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
)

// streamNode is the SSE companion to GET /nodes/{id}. Emits the
// node's allocations + per-node CPU/Memory/RPS on every snapshot tick.
func (api *API) streamNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	if nodeID == "" {
		http.Error(w, "node ID required", http.StatusBadRequest)
		return
	}
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		emitNodeView(w, snap, nodeID, api.ctx.MetricsStore().GetLatestRPS(nodeID))
	})
}

func emitNodeView(w http.ResponseWriter, snap *types.ClusterSnapshot, nodeID string, rps float64) {
	emitStatus(w, snap)

	// Find the node and emit it as its own event so the page doesn't
	// need a parallel global subscription to read its NodeInfo.
	for _, n := range snap.Nodes {
		if n.ID == nodeID {
			sseEvent(w, "node", mustJSON(map[string]any{"node": n}))
			break
		}
	}

	// Services list — needed for resource-limit lookups on the per-
	// node allocations table.
	sseEvent(w, "services", mustJSON(map[string]any{"services": snap.Services}))

	allocs := snap.AllocsByNode[nodeID]
	if allocs == nil {
		allocs = []*types.ServiceAllocation{}
	}
	sseEvent(w, "allocations", mustJSON(map[string]any{"allocations": allocs}))

	cpuPct, memPct := nodeUsagePercents(snap, nodeID)
	now := snap.Timestamp
	sseEvent(w, "metrics", mustJSON(map[string]any{
		"cpu":    []autometrics.MetricPoint{{Timestamp: now, Value: cpuPct}},
		"memory": []autometrics.MetricPoint{{Timestamp: now, Value: memPct}},
		"rps":    []autometrics.MetricPoint{{Timestamp: now, Value: rps}},
	}))
}

func nodeUsagePercents(snap *types.ClusterSnapshot, nodeID string) (cpuPct, memPct float64) {
	for _, node := range snap.Nodes {
		if node.ID != nodeID {
			continue
		}
		if node.CPUTotal > 0 {
			cpuPct = float64(node.CPUTotal-node.CPUAvailable) / float64(node.CPUTotal) * 100
		}
		if node.MemoryTotal > 0 {
			memPct = float64(node.MemoryTotal-node.MemoryAvailable) / float64(node.MemoryTotal) * 100
		}
		return cpuPct, memPct
	}
	return 0, 0
}
