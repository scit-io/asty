package api

import (
	"net/http"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
)

// streamService is the SSE companion to GET /services/{name}.
func (api *API) streamService(w http.ResponseWriter, r *http.Request, serviceName string) {
	if serviceName == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		emitServiceView(w, snap, serviceName)
	})
}

func emitServiceView(w http.ResponseWriter, snap *types.ClusterSnapshot, name string) {
	var svcDef *types.ServiceDefinition
	var avgCPU, avgMem float64
	var running int
	for _, svc := range snap.Services {
		if svc.Name == name {
			svcDef = svc.ServiceDefinition
			avgCPU = svc.AvgCPUPercent
			avgMem = svc.AvgMemoryMB
			running = svc.CurrentCopies
			break
		}
	}
	allocs := snap.AllocsByService[name]
	if allocs == nil {
		allocs = []*types.ServiceAllocation{}
	}
	sseEvent(w, "detail", mustJSON(map[string]any{
		"service":     svcDef,
		"allocations": allocs,
	}))

	now := snap.Timestamp
	sseEvent(w, "metrics", mustJSON(map[string]any{
		"cpu":               []autometrics.MetricPoint{{Timestamp: now, Value: avgCPU}},
		"memory":            []autometrics.MetricPoint{{Timestamp: now, Value: avgMem}},
		"allocations_count": []autometrics.MetricPoint{{Timestamp: now, Value: float64(running)}},
	}))
}
