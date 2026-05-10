package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// handleLogsAllocation returns logs for an allocation via SSE.
func (api *API) handleLogsAllocation(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	allocID := r.URL.Path[len("/api/v1/logs/allocation/"):]
	if allocID == "" {
		api.writeError(w, http.StatusBadRequest, "allocation ID required", nil)
		return
	}

	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &lines)
	}

	follow := r.URL.Query().Get("follow") == "true"

	var allocation *types.ServiceAllocation
	for _, svc := range api.ctx.Services() {
		allocs, err := api.ctx.ClusterState().ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.ID == allocID {
				allocation = alloc
				break
			}
		}
		if allocation != nil {
			break
		}
	}

	if allocation == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}

	if !follow {
		cmdData, err := types.MarshalGetLogsCommand(allocation.ServiceName, lines, false)
		if err != nil {
			api.writeError(w, http.StatusInternalServerError, "failed to create logs command", err)
			return
		}
		subject := fmt.Sprintf("asty.v1.agent.%s.cmd", allocation.NodeID)
		msg, err := api.ctx.NATSConn().Request(subject, cmdData, 5*time.Second)
		if err != nil {
			log.Error().Err(err).Str("node_id", allocation.NodeID).Msg("failed to request logs from agent")
			api.writeError(w, http.StatusServiceUnavailable, "failed to retrieve logs from agent", err)
			return
		}
		var logsResp types.LogsResponse
		if err := json.Unmarshal(msg.Data, &logsResp); err != nil {
			api.writeError(w, http.StatusInternalServerError, "failed to parse logs response", err)
			return
		}
		if !logsResp.Success {
			api.writeError(w, http.StatusInternalServerError, "agent failed to retrieve logs", fmt.Errorf("%s", logsResp.Error))
			return
		}
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"allocation_id": allocID,
			"service_name":  allocation.ServiceName,
			"node_id":       allocation.NodeID,
			"logs":          logsResp.Logs,
			"line_count":    len(logsResp.Logs),
		})
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	bufKey := "node." + allocation.NodeID + ".svc." + allocation.ServiceName
	for _, entry := range api.ctx.LogBuffer().GetLast(bufKey, lines) {
		data, _ := json.Marshal(map[string]interface{}{"line": entry.Line, "timestamp": entry.Timestamp})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	streamSubject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", allocation.NodeID, allocation.ServiceName)
	sub, err := api.ctx.NATSConn().Subscribe(streamSubject, func(msg *nats.Msg) {
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			fmt.Fprintf(w, "data: %s\n\n", msg.Data)
		} else {
			line := formatLogEntry(entry)
			data, _ := json.Marshal(map[string]interface{}{"line": line, "timestamp": entry["timestamp"]})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()
	})
	if err != nil {
		log.Error().Err(err).Str("subject", streamSubject).Msg("failed to subscribe to log stream")
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleLogsNode returns logs for a node (agent logs).
func (api *API) handleLogsNode(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nodeID := r.URL.Path[len("/api/v1/logs/node/"):]
	if nodeID == "" {
		api.writeError(w, http.StatusBadRequest, "node ID required", nil)
		return
	}

	_, err := api.ctx.ClusterState().GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	nLines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &nLines)
	}
	follow := r.URL.Query().Get("follow") == "true"

	if !follow {
		history := api.ctx.LogBuffer().GetLast("node."+nodeID, nLines)
		lines := make([]string, len(history))
		for i, e := range history {
			lines[i] = e.Line
		}
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id": nodeID, "logs": lines, "line_count": len(lines),
		})
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	for _, entry := range api.ctx.LogBuffer().GetLast("node."+nodeID, nLines) {
		data, _ := json.Marshal(map[string]interface{}{"line": entry.Line, "timestamp": entry.Timestamp})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	streamSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", nodeID)
	sub, err := api.ctx.NATSConn().Subscribe(streamSubject, func(msg *nats.Msg) {
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			return
		}
		line := formatLogEntry(entry)
		data, _ := json.Marshal(map[string]interface{}{"line": line, "timestamp": entry["timestamp"]})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	})
	if err != nil {
		log.Error().Err(err).Str("subject", streamSubject).Msg("failed to subscribe to node log stream")
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleLogsCluster returns cluster-wide logs (server logs).
func (api *API) handleLogsCluster(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nLines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &nLines)
	}
	follow := r.URL.Query().Get("follow") == "true"

	if !follow {
		history := api.ctx.LogBuffer().GetLast("cluster", nLines)
		lines := make([]string, len(history))
		for i, e := range history {
			lines[i] = e.Line
		}
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"logs": lines, "line_count": len(lines),
		})
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	for _, entry := range api.ctx.LogBuffer().GetLast("cluster", nLines) {
		data, _ := json.Marshal(map[string]interface{}{"line": entry.Line, "timestamp": entry.Timestamp})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	sub, err := api.ctx.NATSConn().Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			return
		}
		line := formatLogEntry(entry)
		data, _ := json.Marshal(map[string]interface{}{"line": line, "timestamp": entry["timestamp"]})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to subscribe to cluster log stream")
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// sseSetup writes SSE headers and returns a flusher. On unsupported writers it
// sends an error response and returns nil.
func sseSetup(w http.ResponseWriter) http.Flusher {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return nil
	}
	return flusher
}

// formatLogEntry converts a parsed NATS log JSON entry to a display string.
func formatLogEntry(entry map[string]interface{}) string {
	level, _ := entry["level"].(string)
	message, _ := entry["message"].(string)

	var timeStr string
	if t, ok := entry["time"].(string); ok {
		timeStr = t
	} else if ts, ok := entry["timestamp"].(float64); ok {
		timeStr = time.Unix(int64(ts), 0).Format(time.RFC3339)
	} else {
		timeStr = time.Now().Format(time.RFC3339)
	}

	line := fmt.Sprintf("[%s] [%s] %s", timeStr, level, message)

	extra := make(map[string]interface{})
	for k, v := range entry {
		if k != "level" && k != "message" && k != "time" && k != "timestamp" {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		if b, err := json.Marshal(extra); err == nil {
			line += " " + string(b)
		}
	}
	return line
}
