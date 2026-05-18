package rest

import (
	"fmt"
	"net/http"

	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// handleAllocationLogs serves GET
// /nodes/{id}/allocations/{allocId}/logs. Accept-negotiated:
// SSE streams live lines from the agent's per-service log subject;
// JSON returns a snapshot fetched via NATS RPC.
func (api *API) handleAllocationLogs(w http.ResponseWriter, r *http.Request) {
	allocID := r.PathValue("allocId")
	allocation := api.allocByID(allocID)
	if allocation == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}
	nLines := readQueryLines(r)

	if transportPolling(r) {
		api.respondAllocSnapshot(w, allocation, allocID, nLines)
		return
	}

	flusher := stream.Setup(w)
	if flusher == nil {
		return
	}
	bufKey := "node." + allocation.NodeID + ".svc." + allocation.ServiceName
	api.emitBufferedLines(w, bufKey, nLines)
	flusher.Flush()
	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", allocation.NodeID, allocation.ServiceName)
	api.streamFromNATS(w, r, flusher, subject, true)
}

// respondAllocSnapshot sends a "fetch logs" RPC to the owning agent and
// returns the response as JSON.
func (api *API) respondAllocSnapshot(w http.ResponseWriter, allocation *types.ServiceAllocation, allocID string, nLines int) {
	cmdData, err := types.MarshalGetLogsCommand(allocation.ServiceName, nLines, false)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to create logs command", err)
		return
	}
	subject := types.CommandSubject(allocation.NodeID, types.CmdLogs)
	msg, err := api.ctx.NATSConn().Request(subject, cmdData, agentLogsRequestTimeout)
	if err != nil {
		log.Error().Err(err).Str("node_id", allocation.NodeID).Msg("failed to request logs from agent")
		api.writeError(w, http.StatusServiceUnavailable, "failed to retrieve logs from agent", err)
		return
	}
	var resp types.LogsResponse
	if err := codec.Wire.Unmarshal(msg.Data, &resp); err != nil {
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
