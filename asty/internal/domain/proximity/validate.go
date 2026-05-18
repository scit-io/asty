package proximity

import (
	"context"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// validateInterval — how often the background goroutine measures inter-DC
// latencies to catch config-vs-reality drift. Picked at 1 hour because
// configured latencies don't change in seconds; pinging more often would
// add noise and NATS load with no operational benefit.
const validateInterval = 1 * time.Hour

// PingFn measures the round-trip time between two cluster nodes — the
// request travels through srcID's agent to tgtID's agent — returning
// milliseconds, or 0 on any error (no responders, timeout, peer
// unreachable). Injected from the server so this package stays decoupled
// from NATS and is unit-testable without a live cluster.
type PingFn func(srcID, tgtID string) int

// divergenceThreshold — fraction by which actual ping latency may differ
// from the configured value before we log a warning. ±50% is a wide band
// that flags real misconfigurations without noisy alerts on jitter.
const divergenceThreshold = 0.5

// NodeLister abstracts cluster state for periodic validation.
type NodeLister interface {
	ListNodes() ([]*types.NodeInfo, error)
}

// RunValidation periodically re-measures inter-DC latencies via the
// agent ping-peer mechanism (see agent/ping.go) and logs divergence from
// the configured matrix. Runs until ctx is cancelled.
func RunValidation(ctx context.Context, pm *Matrix, lister NodeLister, ping PingFn) {
	ticker := time.NewTicker(validateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodes, err := lister.ListNodes()
			if err != nil {
				log.Error().Err(err).Msg("failed to list nodes for latency validation")
				continue
			}
			pm.ValidateLatencies(ctx, nodes, ping)
		}
	}
}

// ValidateLatencies probes representative nodes in each DC pair via the
// supplied PingFn and logs when the measured latency drifts from the
// configured value.
func (m *Matrix) ValidateLatencies(_ context.Context, nodes []*types.NodeInfo, ping PingFn) {
	dcNodes := make(map[string][]*types.NodeInfo)
	for _, node := range nodes {
		dc := node.Datacenter
		if dc == "" {
			dc = "default"
		}
		dcNodes[dc] = append(dcNodes[dc], node)
	}

	for dc1, targets := range m.latencies {
		for dc2, configured := range targets {
			if dc1 >= dc2 {
				continue // each pair processed once
			}

			actual := m.measureLatency(dcNodes[dc1], dcNodes[dc2], ping)
			if actual == 0 {
				continue
			}

			divergence := float64(actual-configured) / float64(configured)
			if divergence > divergenceThreshold || divergence < -divergenceThreshold {
				log.Warn().
					Str("dc1", dc1).Str("dc2", dc2).
					Int("configured", configured).Int("actual", actual).
					Float64("divergence_pct", divergence*100).
					Msg("latency divergence detected")
			} else {
				log.Debug().
					Str("dc1", dc1).Str("dc2", dc2).
					Int("configured", configured).Int("actual", actual).
					Msg("latency validated")
			}
		}
	}
}

// measureLatency asks the representative node of dc1 to ping the
// representative of dc2, returning the RTT. Returns 0 when either DC
// has no nodes or either representative has no usable ID — the loop
// caller treats 0 as "skip this pair".
func (m *Matrix) measureLatency(nodes1, nodes2 []*types.NodeInfo, ping PingFn) int {
	if len(nodes1) == 0 || len(nodes2) == 0 {
		return 0
	}
	if nodes1[0].ID == "" || nodes2[0].ID == "" {
		return 0
	}
	return ping(nodes1[0].ID, nodes2[0].ID)
}
