package proximity

import "sort"

// GetNearestDatacenter returns the candidate DC closest to source. If
// source itself is in the list, it wins. If no candidate has a configured
// latency, the first candidate is returned as a deterministic fallback.
func (m *Matrix) GetNearestDatacenter(source string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}

	var nearest string
	minLatency := int(^uint(0) >> 1)
	for _, dc := range candidates {
		if dc == source {
			return dc
		}
		latency, ok := m.GetLatency(source, dc)
		if !ok {
			continue
		}
		if latency < minLatency {
			minLatency = latency
			nearest = dc
		}
	}
	if nearest == "" {
		return candidates[0]
	}
	return nearest
}

// SortDatacentersByProximity returns dcs ordered by latency from source,
// with the source DC (if present) always first.
func (m *Matrix) SortDatacentersByProximity(source string, dcs []string) []string {
	sourcePresent := false
	others := make([]string, 0, len(dcs))
	for _, dc := range dcs {
		if dc == source {
			sourcePresent = true
			continue
		}
		others = append(others, dc)
	}

	sort.Slice(others, func(i, j int) bool {
		return m.latencyOr(source, others[i], unknownDCLatency) <
			m.latencyOr(source, others[j], unknownDCLatency)
	})

	if sourcePresent {
		return append([]string{source}, others...)
	}
	return others
}
