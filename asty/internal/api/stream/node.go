package stream

import (
	"net/http"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
)

// Node is the SSE companion to GET /dashboard/v1/nodes/{id}. Emits
// the node's allocations + per-node CPU/Memory/RPS on every snapshot
// tick.
func Node(ctx Context, w http.ResponseWriter, r *http.Request, nodeID string) {
	if nodeID == "" {
		http.Error(w, "node ID required", http.StatusBadRequest)
		return
	}
	RunSnapshotStream(ctx, w, r, func(snap *types.ClusterSnapshot) {
		var rps float64
		if ms := ctx.MetricsStore(); ms != nil {
			rps = ms.GetLatestRPS(nodeID)
		}
		emitNodeView(w, snap, nodeID, rps)
	})
}

func emitNodeView(w http.ResponseWriter, snap *types.ClusterSnapshot, nodeID string, rps float64) {
	EmitStatus(w, snap)

	// Find the node and emit it as its own event so the page doesn't
	// need a parallel global subscription to read its NodeInfo.
	for _, n := range snap.Nodes {
		if n.ID == nodeID {
			Event(w, "node", types.MustJSON(map[string]any{"node": n}))
			break
		}
	}

	// Services list — needed for resource-limit lookups on the per-
	// node allocations table.
	Event(w, "services", types.MustJSON(map[string]any{"services": snap.Services}))

	allocs := snap.AllocsByNode[nodeID]
	if allocs == nil {
		allocs = []*types.ServiceAllocation{}
	}
	Event(w, "allocations", types.MustJSON(map[string]any{"allocations": allocs}))

	cpuPct, memPct := nodeUsagePercents(snap, nodeID)
	now := snap.Timestamp
	Event(w, "metrics", types.MustJSON(map[string]any{
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
