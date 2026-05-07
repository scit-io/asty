package asty

import (
	"sync"
	"time"
)

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp int64   `json:"timestamp"` // Unix timestamp
	Value     float64 `json:"value"`
}

// ScalingEvent represents an autoscaling decision event
type ScalingEvent struct {
	Timestamp int64  `json:"timestamp"`
	Service   string `json:"service"`
	Action    string `json:"action"` // "scale_up" | "scale_down"
	Reason    string `json:"reason"` // "traffic_rps" | "cpu_threshold" | "memory_threshold"
	FromCount int    `json:"from_count"`
	ToCount   int    `json:"to_count"`
	NodeID    string `json:"node_id,omitempty"`
}

// MetricsStore stores timeseries metrics in memory with bounded retention.
type MetricsStore struct {
	mu      sync.RWMutex
	metrics map[string][]MetricPoint // key: "cluster.cpu", "node.{id}.cpu", etc
	maxAge  time.Duration

	eventsMu sync.RWMutex
	events   []ScalingEvent // ring buffer, max 1000
}

func NewMetricsStore(maxAge time.Duration) *MetricsStore {
	return &MetricsStore{
		metrics: make(map[string][]MetricPoint),
		maxAge:  maxAge,
		events:  make([]ScalingEvent, 0, 1000),
	}
}

// Add appends a metric point and prunes values older than maxAge.
func (ms *MetricsStore) Add(key string, value float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	point := MetricPoint{Timestamp: time.Now().Unix(), Value: value}
	ms.metrics[key] = append(ms.metrics[key], point)
	ms.cleanOld(key)
}

// Get returns metric points for a key with timestamp >= since.
func (ms *MetricsStore) Get(key string, since time.Time) []MetricPoint {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	points := ms.metrics[key]
	sinceUnix := since.Unix()
	var result []MetricPoint
	for _, p := range points {
		if p.Timestamp >= sinceUnix {
			result = append(result, p)
		}
	}
	if result == nil {
		return []MetricPoint{}
	}
	return result
}

// GetAfter returns metric points for a key with timestamp strictly > after.
// Used for delta streaming: callers pass the time of the last send.
func (ms *MetricsStore) GetAfter(key string, after time.Time) []MetricPoint {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	points := ms.metrics[key]
	afterUnix := after.Unix()
	var result []MetricPoint
	for _, p := range points {
		if p.Timestamp > afterUnix {
			result = append(result, p)
		}
	}
	if result == nil {
		return []MetricPoint{}
	}
	return result
}

// cleanOld removes points older than maxAge (must be called with mu held).
func (ms *MetricsStore) cleanOld(key string) {
	cutoff := time.Now().Add(-ms.maxAge).Unix()
	points := ms.metrics[key]
	keepFrom := 0
	for i, p := range points {
		if p.Timestamp >= cutoff {
			keepFrom = i
			break
		}
	}
	ms.metrics[key] = points[keepFrom:]
}

// IngestSnapshot derives metrics from a hub snapshot. Called by streamHub on
// every refresh so metricsStore does not need its own KV-polling loop.
// Per-allocation time series are intentionally omitted (allocations are
// ephemeral; current values are embedded in the snapshot itself).
func (ms *MetricsStore) IngestSnapshot(snap *clusterSnapshot) {
	var totalCPUUsed, totalCPUTotal float64
	var totalMemUsed, totalMemTotal float64

	for _, node := range snap.Nodes {
		if node.Status != "ready" {
			continue
		}
		cpuUsed := float64(node.CPUTotal - node.CPUAvailable)
		memUsed := float64(node.MemoryTotal - node.MemoryAvailable)
		totalCPUUsed += cpuUsed
		totalCPUTotal += float64(node.CPUTotal)
		totalMemUsed += memUsed
		totalMemTotal += float64(node.MemoryTotal)

		cpuPct := 0.0
		if node.CPUTotal > 0 {
			cpuPct = cpuUsed / float64(node.CPUTotal) * 100
		}
		memPct := 0.0
		if node.MemoryTotal > 0 {
			memPct = memUsed / float64(node.MemoryTotal) * 100
		}
		ms.Add("node."+node.ID+".cpu", cpuPct)
		ms.Add("node."+node.ID+".memory", memPct)
	}

	clusterCPU := 0.0
	if totalCPUTotal > 0 {
		clusterCPU = totalCPUUsed / totalCPUTotal * 100
	}
	clusterMem := 0.0
	if totalMemTotal > 0 {
		clusterMem = totalMemUsed / totalMemTotal * 100
	}
	ms.Add("cluster.cpu", clusterCPU)
	ms.Add("cluster.memory", clusterMem)

	// cluster.rps: sum the most recent per-node RPS (added by gateway reports).
	cutoff := time.Now().Add(-30 * time.Second)
	var clusterRPS float64
	for _, node := range snap.Nodes {
		if node.Status != "ready" {
			continue
		}
		pts := ms.Get("node."+node.ID+".rps", cutoff)
		if len(pts) > 0 {
			clusterRPS += pts[len(pts)-1].Value
		}
	}
	ms.Add("cluster.rps", clusterRPS)

	// Per-service aggregates already computed in snapshot.
	for _, svc := range snap.Services {
		ms.Add("service."+svc.Name+".cpu", svc.AvgCPUPercent)
		ms.Add("service."+svc.Name+".memory", svc.AvgMemoryMB)
		ms.Add("service."+svc.Name+".alloc_count", float64(svc.CurrentCopies))
	}
}

// AddEvent records a scaling event.
func (ms *MetricsStore) AddEvent(event ScalingEvent) {
	ms.eventsMu.Lock()
	defer ms.eventsMu.Unlock()

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}
	if len(ms.events) >= 1000 {
		ms.events = ms.events[1:]
	}
	ms.events = append(ms.events, event)
}

// GetEvents returns scaling events, newest first, optionally filtered by service.
func (ms *MetricsStore) GetEvents(service string, limit int) []ScalingEvent {
	ms.eventsMu.RLock()
	defer ms.eventsMu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	var filtered []ScalingEvent
	for i := len(ms.events) - 1; i >= 0 && len(filtered) < limit; i-- {
		if service == "" || ms.events[i].Service == service {
			filtered = append(filtered, ms.events[i])
		}
	}
	return filtered
}
