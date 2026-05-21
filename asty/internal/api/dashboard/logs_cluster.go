package dashboard

import (
	"asty/asty/internal/api/stream"
	"net/http"
)

// handleClusterLogs serves GET /logs — orchestrator-level cluster
// health events (node joins/leaves, leader elections, scheduling
// decisions, deploy events, autoscaler decisions). Accept-negotiated:
// SSE flavour streams new lines from NATS `asty.v1.server.logs`, JSON
// flavour returns the recent in-memory buffer.
func (api *API) handleClusterLogs(w http.ResponseWriter, r *http.Request) {
	nLines := readQueryLines(r)

	if transportPolling(r) {
		events := api.ctx.LogBuffer().GetLast("cluster", nLines)
		api.writeJSON(w, http.StatusOK, map[string]any{
			"logs": events, "line_count": len(events),
		})
		return
	}

	flusher := stream.Setup(w)
	if flusher == nil {
		return
	}
	api.emitBufferedEvents(w, "cluster", nLines)
	flusher.Flush()
	api.streamFromNATS(w, r, flusher, "asty.v1.server.logs", false)
}
