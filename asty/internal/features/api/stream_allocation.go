package api

import (
	"net/http"

	"asty/asty/internal/core/types"
)

// streamAllocation is the SSE companion to GET
// /nodes/{id}/allocations/{allocId}.
func (api *API) streamAllocation(w http.ResponseWriter, r *http.Request, allocID string) {
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		alloc := snap.AllocByID[allocID]
		sseEvent(w, "detail", mustJSON(map[string]any{"allocation": alloc}))
	})
}
