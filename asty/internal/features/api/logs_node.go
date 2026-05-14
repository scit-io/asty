package api

import (
	"fmt"
	"net/http"
)

// handleNodeLogs serves GET /nodes/{id}/logs — the agent's own logs
// for one node (not the services it runs). Accept-negotiated: SSE
// streams new lines from `asty.v1.agent.{id}.logs.agent`, JSON
// returns the recent in-memory buffer.
func (api *API) handleNodeLogs(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if _, err := api.ctx.ClusterState().GetNode(nodeID); err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}
	nLines := readQueryLines(r)

	if !wantsSSE(r) {
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
