package api

import (
	"net/http"
)

// handleLogsCluster returns cluster-wide server logs. Without
// ?follow=true it returns the recent buffer as JSON; with it, the
// connection stays open and streams new lines as they arrive.
func (api *API) handleLogsCluster(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nLines := readQueryLines(r)
	follow := r.URL.Query().Get("follow") == "true"

	if !follow {
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
