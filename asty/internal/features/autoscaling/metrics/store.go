package metrics

import (
	"sync"
	"time"

	"asty/asty/internal/core/types"
)

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

// ScalingEvent represents an autoscaling decision event
type ScalingEvent struct {
	Timestamp int64               `json:"timestamp"`
	Service   string              `json:"service"`
	Action    types.ScalingAction `json:"action"`
	Reason    string              `json:"reason"`
	FromCount int                 `json:"from_count"`
	ToCount   int                 `json:"to_count"`
	NodeID    string              `json:"node_id,omitempty"`
}

// Store holds data needed for autoscaler decisions.
type Store struct {
	mu     sync.RWMutex
	rps    map[string][]MetricPoint
	maxAge time.Duration

	eventsMu sync.RWMutex
	events   []ScalingEvent
}

func NewStore(maxAge time.Duration) *Store {
	return &Store{
		rps:    make(map[string][]MetricPoint),
		maxAge: maxAge,
		events: make([]ScalingEvent, 0, 1000),
	}
}

// AddRPS records a gateway RPS sample for a node.
func (s *Store) AddRPS(nodeID string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := "node." + nodeID + ".rps"
	point := MetricPoint{Timestamp: time.Now().Unix(), Value: value}
	s.rps[key] = append(s.rps[key], point)
	s.cleanOld(key)
}

// GetRPS returns RPS points for a node with timestamp >= since.
func (s *Store) GetRPS(nodeID string, since time.Time) []MetricPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := "node." + nodeID + ".rps"
	points := s.rps[key]
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

// GetLatestRPS returns the most recent RPS value for a node.
func (s *Store) GetLatestRPS(nodeID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := "node." + nodeID + ".rps"
	points := s.rps[key]
	cutoff := time.Now().Add(-30 * time.Second).Unix()
	for i := len(points) - 1; i >= 0; i-- {
		if points[i].Timestamp >= cutoff {
			return points[i].Value
		}
	}
	return 0
}

func (s *Store) cleanOld(key string) {
	cutoff := time.Now().Add(-s.maxAge).Unix()
	points := s.rps[key]
	keepFrom := 0
	for i, p := range points {
		if p.Timestamp >= cutoff {
			keepFrom = i
			break
		}
	}
	s.rps[key] = points[keepFrom:]
}

// AddEvent records a scaling event.
func (s *Store) AddEvent(event ScalingEvent) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}
	if len(s.events) >= 1000 {
		s.events = s.events[1:]
	}
	s.events = append(s.events, event)
}

// GetEvents returns scaling events, newest first.
func (s *Store) GetEvents(service string, limit int) []ScalingEvent {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	var filtered []ScalingEvent
	for i := len(s.events) - 1; i >= 0 && len(filtered) < limit; i-- {
		if service == "" || s.events[i].Service == service {
			filtered = append(filtered, s.events[i])
		}
	}
	return filtered
}
