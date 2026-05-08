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

// MetricsStore holds only data needed for autoscaler decisions:
//   - node.{id}.rps timeseries (gateway traffic, 60s window)
//   - scaling events log (ring buffer, max 1000)
//
// All UI metrics are computed live from the snapshot and pushed directly.
type MetricsStore struct {
	mu      sync.RWMutex
	rps     map[string][]MetricPoint // "node.{id}.rps"
	maxAge  time.Duration

	eventsMu sync.RWMutex
	events   []ScalingEvent
}

func NewMetricsStore(maxAge time.Duration) *MetricsStore {
	return &MetricsStore{
		rps:    make(map[string][]MetricPoint),
		maxAge: maxAge,
		events: make([]ScalingEvent, 0, 1000),
	}
}

// AddRPS records a gateway RPS sample for a node.
func (ms *MetricsStore) AddRPS(nodeID string, value float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := "node." + nodeID + ".rps"
	point := MetricPoint{Timestamp: time.Now().Unix(), Value: value}
	ms.rps[key] = append(ms.rps[key], point)
	ms.cleanOld(key)
}

// GetRPS returns RPS points for a node with timestamp >= since.
func (ms *MetricsStore) GetRPS(nodeID string, since time.Time) []MetricPoint {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	key := "node." + nodeID + ".rps"
	points := ms.rps[key]
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

// GetLatestRPS returns the most recent RPS value for a node (for snapshot emission).
func (ms *MetricsStore) GetLatestRPS(nodeID string) float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	key := "node." + nodeID + ".rps"
	points := ms.rps[key]
	cutoff := time.Now().Add(-30 * time.Second).Unix()
	for i := len(points) - 1; i >= 0; i-- {
		if points[i].Timestamp >= cutoff {
			return points[i].Value
		}
	}
	return 0
}

func (ms *MetricsStore) cleanOld(key string) {
	cutoff := time.Now().Add(-ms.maxAge).Unix()
	points := ms.rps[key]
	keepFrom := 0
	for i, p := range points {
		if p.Timestamp >= cutoff {
			keepFrom = i
			break
		}
	}
	ms.rps[key] = points[keepFrom:]
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
