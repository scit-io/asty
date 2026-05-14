package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// handleLogsAllocation returns logs for an allocation. Without
// ?follow=true it RPCs the agent for a snapshot; with it, it streams
// live lines via SSE.
func (api *API) handleLogsAllocation(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}
	allocID := strings.TrimPrefix(r.URL.Path, "/api/v1/logs/allocation/")
	if allocID == "" {
		api.writeError(w, http.StatusBadRequest, "allocation ID required", nil)
		return
	}

	nLines := readQueryLines(r)
	follow := r.URL.Query().Get("follow") == "true"

	allocation := api.allocByID(allocID)
	if allocation == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}

	if !follow {
		api.respondAllocSnapshot(w, allocation, allocID, nLines)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	bufKey := "node." + allocation.NodeID + ".svc." + allocation.ServiceName
	api.emitBufferedLines(w, bufKey, nLines)
	flusher.Flush()

	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", allocation.NodeID, allocation.ServiceName)
	api.streamFromNATS(w, r, flusher, subject, true)
}

// allocByID lives in lookup.go (snapshot-first lookups).

// respondAllocSnapshot sends a "fetch logs" RPC to the owning agent and
// returns the response as JSON.
func (api *API) respondAllocSnapshot(w http.ResponseWriter, allocation *types.ServiceAllocation, allocID string, nLines int) {
	cmdData, err := types.MarshalGetLogsCommand(allocation.ServiceName, nLines, false)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to create logs command", err)
		return
	}
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", allocation.NodeID)
	msg, err := api.ctx.NATSConn().Request(subject, cmdData, agentLogsRequestTimeout)
	if err != nil {
		log.Error().Err(err).Str("node_id", allocation.NodeID).Msg("failed to request logs from agent")
		api.writeError(w, http.StatusServiceUnavailable, "failed to retrieve logs from agent", err)
		return
	}
	var resp types.LogsResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to parse logs response", err)
		return
	}
	if !resp.Success {
		api.writeError(w, http.StatusInternalServerError, "agent failed to retrieve logs", fmt.Errorf("%s", resp.Error))
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"allocation_id": allocID,
		"service_name":  allocation.ServiceName,
		"node_id":       allocation.NodeID,
		"logs":          resp.Logs,
		"line_count":    len(resp.Logs),
	})
}
