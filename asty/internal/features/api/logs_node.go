package api

import (
	"fmt"
	"net/http"
	"strings"
)

// handleLogsNode returns logs for a node (the agent's own logs, not the
// services running on it). With ?follow=true it streams via SSE.
func (api *API) handleLogsNode(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nodeID := strings.TrimPrefix(r.URL.Path, "/api/v1/logs/node/")
	if nodeID == "" {
		api.writeError(w, http.StatusBadRequest, "node ID required", nil)
		return
	}
	if _, err := api.ctx.ClusterState().GetNode(nodeID); err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	nLines := readQueryLines(r)
	follow := r.URL.Query().Get("follow") == "true"

	if !follow {
		history := api.ctx.LogBuffer().GetLast("node."+nodeID, nLines)
		lines := make([]string, len(history))
		for i, e := range history {
			lines[i] = e.Line
		}
		api.writeJSON(w, http.StatusOK, map[string]any{
			"node_id": nodeID, "logs": lines, "line_count": len(lines),
		})
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}
	api.emitBufferedLines(w, "node."+nodeID, nLines)
	flusher.Flush()

	subject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", nodeID)
	api.streamFromNATS(w, r, flusher, subject, false)
}
