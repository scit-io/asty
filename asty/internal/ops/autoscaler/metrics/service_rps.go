package metrics

import "time"

// serviceRPSFreshness — how long a per-(node, service) sample stays
// valid for reads. Matches the per-node RPS freshness so a stalled
// gateway hides both numbers in lockstep. Reporter cadence is 5 s, so
// 30 s allows ~6 missed ticks before zero — enough for a brief
// reconnect, short enough that operators don't see frozen charts.
const serviceRPSFreshness = 30 * time.Second

// serviceRPSKey is the storage key for the per-(node, service) RPS
// map. Single source of truth so writer and reader cannot drift.
func serviceRPSKey(nodeID, service string) string {
	return "node." + nodeID + ".svc." + service
}

// AddServiceRPS records the most recent RPS sample for (nodeID, service).
// Only the latest value is retained — averaging windows are not needed
// for the per-allocation chart, and the per-node time series in
// AddRPS still covers the autoscaler's locality-aware logic.
func (s *Store) AddServiceRPS(nodeID, service string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serviceRPS[serviceRPSKey(nodeID, service)] = MetricPoint{
		Timestamp: time.Now().Unix(),
		Value:     value,
	}
}

// GetLatestServiceRPS returns the most recent sample for (nodeID,
// service) if it landed within serviceRPSFreshness, else zero. Mirrors
// GetLatestRPS so stale gateways don't paint phantom traffic.
func (s *Store) GetLatestServiceRPS(nodeID, service string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.serviceRPS[serviceRPSKey(nodeID, service)]
	if !ok {
		return 0
	}
	if time.Now().Add(-serviceRPSFreshness).Unix() > p.Timestamp {
		return 0
	}
	return p.Value
}
