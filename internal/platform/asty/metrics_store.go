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

// MetricsStore stores timeseries metrics in memory
type MetricsStore struct {
	mu      sync.RWMutex
	metrics map[string][]MetricPoint // key: "cluster.cpu", "node.agent-1.cpu", etc
	maxAge  time.Duration
}

// NewMetricsStore creates a new metrics store
func NewMetricsStore(maxAge time.Duration) *MetricsStore {
	return &MetricsStore{
		metrics: make(map[string][]MetricPoint),
		maxAge:  maxAge,
	}
}

// Add adds a metric point
func (ms *MetricsStore) Add(key string, value float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now().Unix()
	point := MetricPoint{
		Timestamp: now,
		Value:     value,
	}

	ms.metrics[key] = append(ms.metrics[key], point)
	ms.cleanOld(key)
}

// Get returns metric points for a key within the time range
func (ms *MetricsStore) Get(key string, since time.Time) []MetricPoint {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	points, ok := ms.metrics[key]
	if !ok {
		return []MetricPoint{}
	}

	sinceUnix := since.Unix()
	result := []MetricPoint{}
	for _, p := range points {
		if p.Timestamp >= sinceUnix {
			result = append(result, p)
		}
	}

	return result
}

// cleanOld removes old data points (called while holding lock)
func (ms *MetricsStore) cleanOld(key string) {
	cutoff := time.Now().Add(-ms.maxAge).Unix()
	points := ms.metrics[key]

	// Find first point that should be kept
	keepFrom := 0
	for i, p := range points {
		if p.Timestamp >= cutoff {
			keepFrom = i
			break
		}
	}

	ms.metrics[key] = points[keepFrom:]
}

// StartCollection starts periodic metrics collection
func (ms *MetricsStore) StartCollection(state *ClusterState, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			ms.collectMetrics(state)
		}
	}()
}

// collectMetrics collects current metrics from cluster state
func (ms *MetricsStore) collectMetrics(state *ClusterState) {
	nodes, err := state.ListNodes()
	if err != nil {
		return
	}

	var totalCPUUsed, totalCPUTotal float64
	var totalMemUsed, totalMemTotal float64

	for _, node := range nodes {
		if node.Status != "ready" {
			continue
		}

		cpuUsed := float64(node.CPUTotal - node.CPUAvailable)
		memUsed := float64(node.MemoryTotal - node.MemoryAvailable)

		totalCPUUsed += cpuUsed
		totalCPUTotal += float64(node.CPUTotal)
		totalMemUsed += memUsed
		totalMemTotal += float64(node.MemoryTotal)

		// Per-node metrics
		cpuPercent := 0.0
		if node.CPUTotal > 0 {
			cpuPercent = (cpuUsed / float64(node.CPUTotal)) * 100
		}
		memPercent := 0.0
		if node.MemoryTotal > 0 {
			memPercent = (memUsed / float64(node.MemoryTotal)) * 100
		}

		ms.Add("node."+node.ID+".cpu", cpuPercent)
		ms.Add("node."+node.ID+".memory", memPercent)
	}

	// Cluster-wide metrics
	clusterCPU := 0.0
	if totalCPUTotal > 0 {
		clusterCPU = (totalCPUUsed / totalCPUTotal) * 100
	}
	clusterMem := 0.0
	if totalMemTotal > 0 {
		clusterMem = (totalMemUsed / totalMemTotal) * 100
	}

	ms.Add("cluster.cpu", clusterCPU)
	ms.Add("cluster.memory", clusterMem)
}
