package proximity

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// unknownDCLatency is the placeholder used for DC pairs not in the config.
// Picked larger than any plausible real-world inter-DC latency so unknown
// pairs always sort last.
const unknownDCLatency = 1000

// Matrix manages datacenter latency information.
type Matrix struct {
	latencies map[string]map[string]int // dc1 -> dc2 -> latency (ms)
}

// NewMatrix creates a new proximity matrix.
func NewMatrix() *Matrix {
	return &Matrix{
		latencies: make(map[string]map[string]int),
	}
}

// LoadFromConfig loads latency matrix from config string.
// Format: "dc1:dc2:100,dc1:dc3:250,dc2:dc3:200".
func (m *Matrix) LoadFromConfig(config string) error {
	if config == "" {
		return nil
	}

	pairs := strings.Split(config, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 3 {
			return fmt.Errorf("invalid latency format: %s (expected dc1:dc2:latency)", pair)
		}

		dc1, dc2 := parts[0], parts[1]
		var latency int
		if _, err := fmt.Sscanf(parts[2], "%d", &latency); err != nil {
			return fmt.Errorf("invalid latency value: %s", parts[2])
		}

		m.SetLatency(dc1, dc2, latency)
	}

	log.Info().Int("entries", len(pairs)).Msg("loaded latency matrix from config")
	return nil
}

// SetLatency records latency between two datacenters (bidirectional).
func (m *Matrix) SetLatency(dc1, dc2 string, latencyMs int) {
	if m.latencies[dc1] == nil {
		m.latencies[dc1] = make(map[string]int)
	}
	if m.latencies[dc2] == nil {
		m.latencies[dc2] = make(map[string]int)
	}
	m.latencies[dc1][dc2] = latencyMs
	m.latencies[dc2][dc1] = latencyMs
}

// GetLatency returns the latency between two datacenters and a presence
// flag. Same-DC lookups always succeed with 0.
func (m *Matrix) GetLatency(dc1, dc2 string) (int, bool) {
	if dc1 == dc2 {
		return 0, true
	}
	if row := m.latencies[dc1]; row != nil {
		if latency, ok := row[dc2]; ok {
			return latency, true
		}
	}
	return 0, false
}

// latencyOr returns the latency from→to or fallback when the pair is
// missing. Used to give SortDatacentersByProximity a consistent "unknown"
// weight so missing entries sort last.
func (m *Matrix) latencyOr(from, to string, fallback int) int {
	if v, ok := m.GetLatency(from, to); ok {
		return v
	}
	return fallback
}
