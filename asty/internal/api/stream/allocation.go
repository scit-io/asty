package stream

import (
	"net/http"

	"asty/asty/internal/core/types"
)

// Allocation is the SSE companion to GET
// /dashboard/v1/nodes/{id}/allocations/{allocId}. Emits status (for
// Header), the allocation itself, and the service definition (so the
// SPA can compute resource percentages without a parallel /services
// subscription).
func Allocation(ctx Context, w http.ResponseWriter, r *http.Request, allocID string) {
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}
	RunSnapshotStream(ctx, w, r, func(snap *types.ClusterSnapshot) {
		EmitStatus(w, snap)
		alloc := snap.AllocByID[allocID]
		Event(w, "detail", types.MustJSON(map[string]any{"allocation": alloc}))
		if alloc != nil {
			for i, svc := range snap.Services {
				if svc.Name == alloc.ServiceName {
					Event(w, "service", types.MustJSON(map[string]any{"service": &snap.Services[i]}))
					break
				}
			}
		}
	})
}
