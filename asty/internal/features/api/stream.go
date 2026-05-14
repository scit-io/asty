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

// maxStreamInterval caps the client-requested throttle. Beyond a few
// minutes the connection looks idle to proxies even with keepalives,
// and the user is better served by a fresh page load.
const maxStreamInterval = 5 * time.Minute

// sseEvent writes a single SSE event ("event: name", "data: json",
// blank line). The blank line is what tells the client the event is
// complete.
func sseEvent(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// emitStatus writes the compact cluster-status event that every per-
// resource stream emits so the SPA's Header (leader, node counts) can
// stay live on any page without a parallel global subscription.
func emitStatus(w http.ResponseWriter, snap *types.ClusterSnapshot) {
	sseEvent(w, "status", mustJSON(map[string]any{
		"cluster":   snap.Cluster,
		"services":  map[string]any{"loaded": len(snap.Services)},
		"timestamp": snap.Timestamp,
	}))
}

// parseIntervalQuery reads ?interval=… (Go duration syntax) and clamps
// to [0, maxStreamInterval]. Zero means "no throttle" — emit on every
// snapshot, which is the historical behaviour.
func parseIntervalQuery(r *http.Request) time.Duration {
	v := r.URL.Query().Get("interval")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0
	}
	if d > maxStreamInterval {
		d = maxStreamInterval
	}
	return d
}

// runSnapshotStream is the shared driver for the four SSE handlers
// that emit on each snapshot tick. It subscribes to the streamHub,
// invokes emit on every snapshot, and sends keepalives on idle. The
// caller's emit closure decides which fields to send and is the only
// per-endpoint piece of code. Honours ?interval=Xs to throttle pages
// that don't want the natural tick rate.
func (api *API) runSnapshotStream(w http.ResponseWriter, r *http.Request, emit func(*types.ClusterSnapshot)) {
	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.ctx.StreamHub().Subscribe()
	defer unsub()

	interval := parseIntervalQuery(r)
	var lastEmit time.Time

	emitAndFlush := func(snap *types.ClusterSnapshot) {
		if interval > 0 && !lastEmit.IsZero() && time.Since(lastEmit) < interval {
			return
		}
		emit(snap)
		flusher.Flush()
		lastEmit = time.Now()
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
