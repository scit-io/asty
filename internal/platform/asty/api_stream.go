package asty

import (
	"fmt"
	"net/http"
	"time"
)

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

// sseEvent writes a single SSE event (event: name, data: json, blank line).
func sseEvent(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// handleStream handles the global SSE stream — cluster status, nodes, services
// (with usage), cluster metrics (delta), and drain progress.
func (api *API) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	hub := api.server.streamHub
	snapshots, unsubSnap := hub.Subscribe()
	defer unsubSnap()
	drainCh, unsubDrain := hub.SubscribeDrain()
	defer unsubDrain()
	eventCh, unsubEvent := hub.SubscribeEvents()
	defer unsubEvent()

	emit := func(snap *clusterSnapshot) {
		sseEvent(w, "status", mustJSON(map[string]interface{}{
			"cluster":   snap.Cluster,
			"services":  map[string]interface{}{"loaded": len(snap.Services)},
			"timestamp": snap.Timestamp,
		}))
		sseEvent(w, "nodes", mustJSON(map[string]interface{}{"nodes": snap.Nodes}))
		sseEvent(w, "services", mustJSON(map[string]interface{}{"services": snap.Services}))

		var totalCPUUsed, totalCPUTotal, totalMemUsed, totalMemTotal, clusterRPS float64
		ms := api.server.metricsStore
		for _, node := range snap.Nodes {
			if node.Status != "ready" {
				continue
			}
			totalCPUUsed += float64(node.CPUTotal - node.CPUAvailable)
			totalCPUTotal += float64(node.CPUTotal)
			totalMemUsed += float64(node.MemoryTotal - node.MemoryAvailable)
			totalMemTotal += float64(node.MemoryTotal)
			clusterRPS += ms.GetLatestRPS(node.ID)
		}
		clusterCPU := 0.0
		if totalCPUTotal > 0 {
			clusterCPU = totalCPUUsed / totalCPUTotal * 100
		}
		clusterMem := 0.0
		if totalMemTotal > 0 {
			clusterMem = totalMemUsed / totalMemTotal * 100
		}
		now := snap.Timestamp
		sseEvent(w, "cluster_metrics", mustJSON(map[string]interface{}{
			"cpu":    []MetricPoint{{Timestamp: now, Value: clusterCPU}},
			"memory": []MetricPoint{{Timestamp: now, Value: clusterMem}},
			"rps":    []MetricPoint{{Timestamp: now, Value: clusterRPS}},
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
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

// handleStreamNode streams allocations + metrics for a single node.
func (api *API) handleStreamNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeID := r.URL.Path[len("/api/v1/stream/node/"):]
	if nodeID == "" {
		http.Error(w, "node ID required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		allocs := snap.AllocsByNode[nodeID]
		if allocs == nil {
			allocs = []*ServiceAllocation{}
		}
		sseEvent(w, "allocations", mustJSON(map[string]interface{}{"allocations": allocs}))

		var cpuPct, memPct float64
		for _, node := range snap.Nodes {
			if node.ID == nodeID {
				if node.CPUTotal > 0 {
					cpuPct = float64(node.CPUTotal-node.CPUAvailable) / float64(node.CPUTotal) * 100
				}
				if node.MemoryTotal > 0 {
					memPct = float64(node.MemoryTotal-node.MemoryAvailable) / float64(node.MemoryTotal) * 100
				}
				break
			}
		}
		rpsVal := api.server.metricsStore.GetLatestRPS(nodeID)
		now := snap.Timestamp
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":    []MetricPoint{{Timestamp: now, Value: cpuPct}},
			"memory": []MetricPoint{{Timestamp: now, Value: memPct}},
			"rps":    []MetricPoint{{Timestamp: now, Value: rpsVal}},
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
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
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamService streams definition + allocations + metrics for a service.
func (api *API) handleStreamService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceName := r.URL.Path[len("/api/v1/stream/service/"):]
	if serviceName == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		var svcDef *ServiceDefinition
		var avgCPU, avgMem float64
		var running int
		for _, svc := range snap.Services {
			if svc.Name == serviceName {
				svcDef = svc.ServiceDefinition
				avgCPU = svc.AvgCPUPercent
				avgMem = svc.AvgMemoryMB
				running = svc.CurrentCopies
				break
			}
		}
		allocs := snap.AllocsByService[serviceName]
		if allocs == nil {
			allocs = []*ServiceAllocation{}
		}
		sseEvent(w, "detail", mustJSON(map[string]interface{}{
			"service":     svcDef,
			"allocations": allocs,
		}))

		now := snap.Timestamp
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":               []MetricPoint{{Timestamp: now, Value: avgCPU}},
			"memory":            []MetricPoint{{Timestamp: now, Value: avgMem}},
			"allocations_count": []MetricPoint{{Timestamp: now, Value: float64(running)}},
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
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
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamAllocation streams a single allocation's detail + metrics.
func (api *API) handleStreamAllocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allocID := r.URL.Path[len("/api/v1/stream/allocation/"):]
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		alloc := snap.AllocByID[allocID]
		sseEvent(w, "detail", mustJSON(map[string]interface{}{"allocation": alloc}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
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
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
