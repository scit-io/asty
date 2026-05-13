package api

import (
	"net/http"
	"strings"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
)

// handleStreamService streams definition + allocations + metrics for a
// service.
func (api *API) handleStreamService(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}
	serviceName := strings.TrimPrefix(r.URL.Path, "/api/v1/stream/service/")
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
	sseEvent(w, "detail", mustJSON(map[string]interface{}{
		"service":     svcDef,
		"allocations": allocs,
	}))

	now := snap.Timestamp
	sseEvent(w, "metrics", mustJSON(map[string]interface{}{
		"cpu":               []autometrics.MetricPoint{{Timestamp: now, Value: avgCPU}},
		"memory":            []autometrics.MetricPoint{{Timestamp: now, Value: avgMem}},
		"allocations_count": []autometrics.MetricPoint{{Timestamp: now, Value: float64(running)}},
	}))
}
