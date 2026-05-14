package api

import (
	"net/http"

	"asty/asty/internal/core/types"
)

// streamAllocation is the SSE companion to GET
// /nodes/{id}/allocations/{allocId}. Emits status (for Header), the
// allocation itself, and the service definition (so the SPA can
// compute resource percentages without a parallel /services
// subscription).
func (api *API) streamAllocation(w http.ResponseWriter, r *http.Request, allocID string) {
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		emitStatus(w, snap)
		alloc := snap.AllocByID[allocID]
		sseEvent(w, "detail", mustJSON(map[string]any{"allocation": alloc}))
		if alloc != nil {
			for i, svc := range snap.Services {
				if svc.Name == alloc.ServiceName {
					sseEvent(w, "service", mustJSON(map[string]any{"service": &snap.Services[i]}))
					break
				}
			}
		}
	})
}
