package api

import (
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
		history := api.ctx.LogBuffer().GetLast("cluster", nLines)
		lines := make([]string, len(history))
		for i, e := range history {
			lines[i] = e.Line
		}
		api.writeJSON(w, http.StatusOK, map[string]any{
			"logs": lines, "line_count": len(lines),
		})
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}
	api.emitBufferedLines(w, "cluster", nLines)
	flusher.Flush()
	api.streamFromNATS(w, r, flusher, "asty.v1.server.logs", false)
}
