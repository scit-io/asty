package stream

import (
	"net/http"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
)

// Allocation is the SSE companion to GET
// /dashboard/v1/nodes/{id}/allocations/{allocId}. Emits status (for
// Header), the allocation itself, the service definition (so the SPA
// can compute resource percentages without a parallel /services
// subscription), and per-tick CPU/Memory/RPS samples for the charts.
func Allocation(ctx Context, w http.ResponseWriter, r *http.Request, allocID string) {
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}
	RunSnapshotStream(ctx, w, r, func(snap *types.ClusterSnapshot) {
		emitAllocationView(ctx, w, snap, allocID)
	})
}

func emitAllocationView(ctx Context, w http.ResponseWriter, snap *types.ClusterSnapshot, allocID string) {
	EmitStatus(w, snap)
	alloc := snap.AllocByID[allocID]
	Event(w, "detail", types.MustJSON(map[string]any{"allocation": alloc}))
	if alloc == nil {
		return
	}
	var svc *types.ServiceWithUsage
	for i := range snap.Services {
		if snap.Services[i].Name == alloc.ServiceName {
			svc = &snap.Services[i]
			break
		}
	}
	if svc != nil {
		Event(w, "service", types.MustJSON(map[string]any{"service": svc}))
	}
	cpuPct, memPct := allocUsagePercents(alloc, svc)
	var rps float64
	if ms := ctx.MetricsStore(); ms != nil {
		rps = ms.GetLatestServiceRPS(alloc.NodeID, alloc.ServiceName)
	}
	now := snap.Timestamp
	Event(w, "metrics", types.MustJSON(map[string]any{
		"cpu":    []autometrics.MetricPoint{{Timestamp: now, Value: cpuPct}},
		"memory": []autometrics.MetricPoint{{Timestamp: now, Value: memPct}},
		"rps":    []autometrics.MetricPoint{{Timestamp: now, Value: rps}},
	}))
}

// allocUsagePercents converts per-process CPU/Memory samples into
// percent-of-allocated. alloc.CPUUsage is sampled by the agent's
// metricsCollector as a process CPU%, which on multi-core can exceed
// 100 — the chart's Y-axis auto-scales, so we pass the raw value.
// alloc.MemoryUsage (MB) is normalised against svc.Resources.Memory
// so the chart reads as "% of the service's memory budget", matching
// how the node page renders memory utilisation.
func allocUsagePercents(alloc *types.ServiceAllocation, svc *types.ServiceWithUsage) (cpuPct, memPct float64) {
	cpuPct = float64(alloc.CPUUsage)
	if svc != nil && svc.Resources.Memory > 0 {
		memPct = float64(alloc.MemoryUsage) / float64(svc.Resources.Memory) * 100
	}
	return cpuPct, memPct
}
