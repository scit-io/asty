package stream

import (
	"fmt"
	"net/http"
	"time"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
)

// Cluster is the SSE feed for GET /dashboard/v1/ (Accept: text/event-stream).
// Publishes status, nodes, services, cluster-level metrics, drain
// progress, and cluster events. Bigger than the per-resource streams
// because it folds in extra subscriptions (drain, events).
func Cluster(ctx Context, w http.ResponseWriter, r *http.Request) {
	flusher := Setup(w)
	if flusher == nil {
		return
	}

	hub := ctx.StreamHub()
	snapshots, unsubSnap := hub.Subscribe()
	defer unsubSnap()
	drainCh, unsubDrain := hub.SubscribeDrain()
	defer unsubDrain()
	eventCh, unsubEvent := hub.SubscribeEvents()
	defer unsubEvent()

	emit := func(snap *types.ClusterSnapshot) {
		emitClusterSnapshot(ctx, w, snap)
		flusher.Flush()
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
			emit(snap)
		case data, ok := <-drainCh:
			if !ok {
				return
			}
			Event(w, "drain_progress", data)
			flusher.Flush()
		case data, ok := <-eventCh:
			if !ok {
				return
			}
			Event(w, "cluster_event", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// emitClusterSnapshot writes the per-snapshot events: status, nodes,
// services, and aggregate cluster metrics.
func emitClusterSnapshot(ctx Context, w http.ResponseWriter, snap *types.ClusterSnapshot) {
	Event(w, "status", types.MustJSON(map[string]any{
		"cluster":   snap.Cluster,
		"services":  map[string]any{"loaded": len(snap.Services)},
		"timestamp": snap.Timestamp,
	}))
	Event(w, "nodes", types.MustJSON(map[string]any{"nodes": snap.Nodes}))
	Event(w, "services", types.MustJSON(map[string]any{"services": snap.Services}))

	cpu, mem, rps := aggregateClusterMetrics(ctx, snap)
	now := snap.Timestamp
	Event(w, "cluster_metrics", types.MustJSON(map[string]any{
		"cpu":    []autometrics.MetricPoint{{Timestamp: now, Value: cpu}},
		"memory": []autometrics.MetricPoint{{Timestamp: now, Value: mem}},
		"rps":    []autometrics.MetricPoint{{Timestamp: now, Value: rps}},
	}))
}

// aggregateClusterMetrics sums CPU/Memory used vs total across all
// ready nodes (returning percentages) and adds up the latest RPS
// samples per node into a cluster-wide RPS.
func aggregateClusterMetrics(ctx Context, snap *types.ClusterSnapshot) (cpuPct, memPct, rps float64) {
	var cpuUsed, cpuTotal, memUsed, memTotal float64
	ms := ctx.MetricsStore()
	for _, node := range snap.Nodes {
		if node.Status != types.NodeReady {
			continue
		}
		cpuUsed += float64(node.CPUTotal - node.CPUAvailable)
		cpuTotal += float64(node.CPUTotal)
		memUsed += float64(node.MemoryTotal - node.MemoryAvailable)
		memTotal += float64(node.MemoryTotal)
		if ms != nil {
			rps += ms.GetLatestRPS(node.ID)
		}
	}
	if cpuTotal > 0 {
		cpuPct = cpuUsed / cpuTotal * 100
	}
	if memTotal > 0 {
		memPct = memUsed / memTotal * 100
	}
	return cpuPct, memPct, rps
}

// Nodes is the list-view SSE companion to GET /dashboard/v1/nodes.
// Emits the full node slice + compact status on each tick so the page
// can run without a parallel global subscription.
func Nodes(ctx Context, w http.ResponseWriter, r *http.Request) {
	RunSnapshotStream(ctx, w, r, func(snap *types.ClusterSnapshot) {
		EmitStatus(w, snap)
		Event(w, "nodes", types.MustJSON(map[string]any{
			"nodes": snap.Nodes,
			"count": len(snap.Nodes),
		}))
	})
}

// Services is the list-view SSE companion to GET /dashboard/v1/services.
// Same self-sufficiency contract as Nodes.
func Services(ctx Context, w http.ResponseWriter, r *http.Request) {
	RunSnapshotStream(ctx, w, r, func(snap *types.ClusterSnapshot) {
		EmitStatus(w, snap)
		Event(w, "services", types.MustJSON(map[string]any{
			"services": snap.Services,
			"count":    len(snap.Services),
		}))
	})
}
