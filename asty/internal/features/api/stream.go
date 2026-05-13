package api

import (
	"fmt"
	"net/http"
	"time"

	"asty/asty/internal/core/types"
)

// ssePingInterval — how often we send a keepalive comment line over an
// idle SSE connection. Browsers and proxies tend to close streams after
// 60–120 s of silence; 30 s gives plenty of margin.
const ssePingInterval = 30 * time.Second

// sseEvent writes a single SSE event ("event: name", "data: json",
// blank line). The blank line is what tells the client the event is
// complete.
func sseEvent(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// runSnapshotStream is the shared driver for the four SSE handlers
// that emit on each snapshot tick. It subscribes to the streamHub,
// invokes emit on every snapshot, and sends keepalives on idle. The
// caller's emit closure decides which fields to send and is the only
// per-endpoint piece of code.
func (api *API) runSnapshotStream(w http.ResponseWriter, r *http.Request, emit func(*types.ClusterSnapshot)) {
	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.ctx.StreamHub().Subscribe()
	defer unsub()

	emitAndFlush := func(snap *types.ClusterSnapshot) {
		emit(snap)
		flusher.Flush()
	}

	ping := time.NewTicker(ssePingInterval)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			emitAndFlush(snap)
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
