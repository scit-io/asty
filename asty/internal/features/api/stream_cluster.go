package api

import (
	"fmt"
	"net/http"
	"time"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
)

// streamCluster is the SSE feed for GET / (Accept: text/event-stream).
// Publishes status, nodes, services, cluster-level metrics, drain
// progress, and cluster events. Bigger than the per-resource streams
// because it folds in extra subscriptions (drain, events).
func (api *API) streamCluster(w http.ResponseWriter, r *http.Request) {
	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	hub := api.ctx.StreamHub()
	snapshots, unsubSnap := hub.Subscribe()
	defer unsubSnap()
	drainCh, unsubDrain := hub.SubscribeDrain()
	defer unsubDrain()
	eventCh, unsubEvent := hub.SubscribeEvents()
	defer unsubEvent()

	emit := func(snap *types.ClusterSnapshot) {
		api.emitClusterSnapshot(w, snap)
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
			emit(snap)
		case data, ok := <-drainCh:
			if !ok {
				return
			}
			sseEvent(w, "drain_progress", data)
			flusher.Flush()
		case data, ok := <-eventCh:
			if !ok {
				return
			}
			sseEvent(w, "cluster_event", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// emitClusterSnapshot writes the per-snapshot events: status, nodes,
// services, and aggregate cluster metrics.
func (api *API) emitClusterSnapshot(w http.ResponseWriter, snap *types.ClusterSnapshot) {
	sseEvent(w, "status", mustJSON(map[string]any{
		"cluster":   snap.Cluster,
		"services":  map[string]any{"loaded": len(snap.Services)},
		"timestamp": snap.Timestamp,
	}))
	sseEvent(w, "nodes", mustJSON(map[string]any{"nodes": snap.Nodes}))
	sseEvent(w, "services", mustJSON(map[string]any{"services": snap.Services}))

	cpu, mem, rps := api.aggregateClusterMetrics(snap)
	now := snap.Timestamp
	sseEvent(w, "cluster_metrics", mustJSON(map[string]any{
		"cpu":    []autometrics.MetricPoint{{Timestamp: now, Value: cpu}},
		"memory": []autometrics.MetricPoint{{Timestamp: now, Value: mem}},
		"rps":    []autometrics.MetricPoint{{Timestamp: now, Value: rps}},
	}))
}

// aggregateClusterMetrics sums CPU/Memory used vs total across all
// ready nodes (returning percentages) and adds up the latest RPS
// samples per node into a cluster-wide RPS.
func (api *API) aggregateClusterMetrics(snap *types.ClusterSnapshot) (cpuPct, memPct, rps float64) {
	var cpuUsed, cpuTotal, memUsed, memTotal float64
	ms := api.ctx.MetricsStore()
	for _, node := range snap.Nodes {
		if node.Status != types.NodeReady {
			continue
		}
		cpuUsed += float64(node.CPUTotal - node.CPUAvailable)
		cpuTotal += float64(node.CPUTotal)
		memUsed += float64(node.MemoryTotal - node.MemoryAvailable)
		memTotal += float64(node.MemoryTotal)
		rps += ms.GetLatestRPS(node.ID)
	}
	if cpuTotal > 0 {
		cpuPct = cpuUsed / cpuTotal * 100
	}
	if memTotal > 0 {
		memPct = memUsed / memTotal * 100
	}
	return cpuPct, memPct, rps
}

// streamNodes is the list-view SSE companion to GET /nodes. Emits the
// full node slice on each snapshot tick.
func (api *API) streamNodes(w http.ResponseWriter, r *http.Request) {
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		sseEvent(w, "nodes", mustJSON(map[string]any{
			"nodes": snap.Nodes,
			"count": len(snap.Nodes),
		}))
	})
}

// streamServices is the list-view SSE companion to GET /services.
func (api *API) streamServices(w http.ResponseWriter, r *http.Request) {
	api.runSnapshotStream(w, r, func(snap *types.ClusterSnapshot) {
		sseEvent(w, "services", mustJSON(map[string]any{
			"services": snap.Services,
			"count":    len(snap.Services),
		}))
	})
}
