package api

import (
	"net/http"
	"strings"

	"asty/asty/internal/core/types"
)

// handleStreamAllocation streams a single allocation's detail + metrics.
func (api *API) handleStreamAllocation(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}
	allocID := strings.TrimPrefix(r.URL.Path, "/api/v1/stream/allocation/")
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}

	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		alloc := snap.AllocByID[allocID]
		sseEvent(w, "detail", mustJSON(map[string]interface{}{"allocation": alloc}))
	})
}
