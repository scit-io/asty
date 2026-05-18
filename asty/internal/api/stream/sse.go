// Package stream provides the Server-Sent Events surface of the
// orchestrator. The per-resource handlers (Cluster, Node, Service,
// Allocation, Nodes, Services) live here; api/dashboard dispatches to
// them from its content-negotiating handlers. The logs handlers in
// api/dashboard also reuse Setup for SSE plumbing.
//
// The package defines its own minimal Context + Hub interfaces; the
// real *server.streamHub satisfies Hub through Go's structural typing,
// and the rest of ServerContext is reduced to RPSSource for the
// cluster-metrics aggregation. Tests can supply stubs.
package stream

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

// Context is the minimal capability set the stream handlers need.
// The real server context satisfies it through structural typing.
type Context interface {
	StreamHub() Hub
	MetricsStore() RPSSource
}

// Hub is the subset of *server.streamHub the stream handlers consume.
type Hub interface {
	Subscribe() (<-chan *types.ClusterSnapshot, func())
	SubscribeDrain() (<-chan []byte, func())
	SubscribeEvents() (<-chan []byte, func())
}

// RPSSource lets cluster aggregation read the latest RPS per node.
// MetricsStore in ops/autoscaler/metrics satisfies it.
type RPSSource interface {
	GetLatestRPS(nodeID string) float64
}

// Setup writes SSE response headers and returns the http.Flusher the
// handler needs. On a writer that doesn't implement Flusher it writes
// a 500 to the client and returns nil — caller should bail out.
func Setup(w http.ResponseWriter) http.Flusher {
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

// Event writes a single SSE event ("event: name\ndata: …\n\n"). The
// trailing blank line is what tells the client the event is complete.
func Event(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// EmitStatus writes the compact cluster-status event that every per-
// resource stream emits so the SPA's header (leader, node counts) can
// stay live on any page without a parallel global subscription.
func EmitStatus(w http.ResponseWriter, snap *types.ClusterSnapshot) {
	Event(w, "status", types.MustJSON(map[string]any{
		"cluster":   snap.Cluster,
		"services":  map[string]any{"loaded": len(snap.Services)},
		"timestamp": snap.Timestamp,
	}))
}

// ParseIntervalQuery reads ?interval=… (Go duration syntax) and clamps
// to [0, maxStreamInterval]. Zero means "no throttle" — emit on every
// snapshot, which is the historical behaviour.
func ParseIntervalQuery(r *http.Request) time.Duration {
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

// RunSnapshotStream is the shared driver for the per-resource SSE
// handlers that emit on each snapshot tick. It subscribes to the Hub,
// invokes emit on every snapshot, and sends keepalives on idle. The
// caller's emit closure decides which fields to send and is the only
// per-endpoint piece of code. Honours ?interval=Xs to throttle.
func RunSnapshotStream(ctx Context, w http.ResponseWriter, r *http.Request, emit func(*types.ClusterSnapshot)) {
	flusher := Setup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := ctx.StreamHub().Subscribe()
	defer unsub()

	interval := ParseIntervalQuery(r)
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

	rctx := r.Context()
	for {
		select {
		case <-rctx.Done():
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
