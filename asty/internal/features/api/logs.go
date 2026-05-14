package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// defaultLogLines — how many lines `?lines=` defaults to when omitted.
// 100 is enough to read the recent context of an incident without
// flooding the UI.
const defaultLogLines = 100

// agentLogsRequestTimeout — how long we wait for an agent to respond
// to a "fetch logs" RPC. Short because the agent just reads a local
// file; if it hangs longer, something is wrong with NATS or the agent.
const agentLogsRequestTimeout = 5 * time.Second

// sseSetup writes SSE headers and returns a flusher. On unsupported
// writers it sends an error response and returns nil.
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

// formatLogEntry turns a parsed zerolog JSON entry into a readable
// display string. Unknown fields are appended as a JSON object to keep
// structured context visible without flooding the main line.
func formatLogEntry(entry map[string]interface{}) string {
	level, _ := entry["level"].(string)
	message, _ := entry["message"].(string)

	var timeStr string
	switch v := entry["time"].(type) {
	case string:
		timeStr = v
	default:
		if ts, ok := entry["timestamp"].(float64); ok {
			timeStr = time.Unix(int64(ts), 0).Format(time.RFC3339)
		} else {
			timeStr = time.Now().Format(time.RFC3339)
		}
	}

	line := fmt.Sprintf("[%s] [%s] %s", timeStr, level, message)

	extra := make(map[string]interface{})
	for k, v := range entry {
		switch k {
		case "level", "message", "time", "timestamp":
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		if b, err := json.Marshal(extra); err == nil {
			line += " " + string(b)
		}
	}
	return line
}

// readQueryLines extracts the ?lines=N parameter, falling back to
// defaultLogLines when missing or unparseable.
func readQueryLines(r *http.Request) int {
	n := defaultLogLines
	if l := r.URL.Query().Get("lines"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			n = v
		}
	}
	return n
}

// streamFromNATS subscribes to subject and pushes each message as an
// SSE "data:" line. It blocks until r.Context() is cancelled. Errors
// reported by the parser are passed through verbatim — handlers
// override this where they want stricter behaviour.
func (api *API) streamFromNATS(w http.ResponseWriter, r *http.Request, flusher http.Flusher, subject string, fallthroughOnParse bool) {
	sub, err := api.ctx.NATSConn().Subscribe(subject, func(msg *nats.Msg) {
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			if fallthroughOnParse {
				fmt.Fprintf(w, "data: %s\n\n", msg.Data)
				flusher.Flush()
			}
			return
		}
		line := formatLogEntry(entry)
		data, _ := json.Marshal(map[string]interface{}{"line": line, "timestamp": entry["timestamp"]})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to log stream")
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// emitBufferedLines writes the most recent N lines for source from the
// in-memory log buffer as SSE data lines. Used to seed an SSE stream
// with history before the live tail starts.
func (api *API) emitBufferedLines(w http.ResponseWriter, source string, lines int) {
	for _, entry := range api.ctx.LogBuffer().GetLast(source, lines) {
		data, _ := json.Marshal(map[string]interface{}{"line": entry.Line, "timestamp": entry.Timestamp})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
}
