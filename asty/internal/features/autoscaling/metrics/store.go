package metrics

import (
	"sync"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/core/util/ringbuf"
)

// eventsCapacity caps the in-memory scaling-event history. 1000 covers
// roughly a week at one decision per ~10 minutes; older events fall
// off the back of the ring.
const eventsCapacity = 1000

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
	events   *ringbuf.Ring[ScalingEvent]
}

func NewStore(maxAge time.Duration) *Store {
	return &Store{
		rps:    make(map[string][]MetricPoint),
		maxAge: maxAge,
		events: ringbuf.New[ScalingEvent](eventsCapacity),
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

// AddEvent records a scaling event. Pushing past the ring's capacity
// silently drops the oldest entry.
func (s *Store) AddEvent(event ScalingEvent) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}
	s.events.Push(event)
}

// GetEvents returns scaling events, newest first, optionally filtered
// by service. limit <= 0 falls back to 100 (the typical "tail" count
// requested by the UI).
func (s *Store) GetEvents(service string, limit int) []ScalingEvent {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	snap := s.events.Snapshot()
	filtered := make([]ScalingEvent, 0, limit)
	for i := len(snap) - 1; i >= 0 && len(filtered) < limit; i-- {
		if service == "" || snap[i].Service == service {
			filtered = append(filtered, snap[i])
		}
	}
	return filtered
}
