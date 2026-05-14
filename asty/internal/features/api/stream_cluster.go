package api

import (
	"fmt"
	"net/http"
	"time"

	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
)

// handleStream is the cluster-wide SSE feed. It publishes status,
// nodes, services, cluster-level metrics, drain progress, and cluster
// events. Bigger than the per-resource streams because it folds in
// extra subscriptions (drain, events).
func (api *API) handleStream(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

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
// services, and aggregate cluster metrics. Pulled out so handleStream
// stays readable.
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
