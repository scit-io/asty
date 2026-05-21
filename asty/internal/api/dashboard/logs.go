package dashboard

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"asty/asty/internal/infra/logs"

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
// SSE "data:" line carrying the structured Event JSON the UI knows
// how to render. fallthroughOnRaw=true forwards the original bytes
// when decode fails (covers raw stdout frames published before the
// Event schema landed).
func (api *API) streamFromNATS(w http.ResponseWriter, r *http.Request, flusher http.Flusher, subject string, fallthroughOnRaw bool) {
	sub, err := api.ctx.NATSConn().Subscribe(subject, func(msg *nats.Msg) {
		e, err := logs.ParseEvent(msg.Data)
		if err != nil {
			if fallthroughOnRaw {
				fmt.Fprintf(w, "data: %s\n\n", msg.Data)
				flusher.Flush()
			}
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", e.MarshalWire())
		flusher.Flush()
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to log stream")
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// emitBufferedEvents writes the most recent N events for source from the
// in-memory log buffer as SSE data lines. Seeds an SSE stream with
// history before the live tail starts so the UI shows context, not a
// blank screen, on first open.
func (api *API) emitBufferedEvents(w http.ResponseWriter, source string, lines int) {
	for _, e := range api.ctx.LogBuffer().GetLast(source, lines) {
		fmt.Fprintf(w, "data: %s\n\n", e.MarshalWire())
	}
}
