package proximity

import (
	"context"
	"net"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// validateInterval — how often the background goroutine pings inter-DC
// latencies to catch config-vs-reality drift. Picked at 1 hour because
// configured latencies don't change in seconds; pinging more often would
// add noise and TCP load with no operational benefit.
const validateInterval = 1 * time.Hour

// pingProbeTimeout caps how long a single TCP ping can hang. Long enough
// to not flap on transient hiccups, short enough that one bad node can't
// stall the whole validation pass.
const pingProbeTimeout = 2 * time.Second

// pingProbePort is a well-known asty port we use for TCP-connect timing.
// We don't speak any protocol on it — the connect handshake itself gives
// us a serviceable round-trip estimate without needing ICMP privileges.
const pingProbePort = ":4646"

// divergenceThreshold — fraction by which actual ping latency may differ
// from the configured value before we log a warning. ±50% is a wide band
// that flags real misconfigurations without noisy alerts on jitter.
const divergenceThreshold = 0.5

// NodeLister abstracts cluster state for periodic validation.
type NodeLister interface {
	ListNodes() ([]*types.NodeInfo, error)
}

// RunValidation periodically re-measures inter-DC latencies and logs
// divergence from the configured matrix. It runs until ctx is cancelled.
func RunValidation(ctx context.Context, pm *Matrix, lister NodeLister) {
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
			pm.ValidateLatencies(ctx, nodes)
		}
	}
}

// ValidateLatencies probes representative nodes in each DC pair and logs
// when the measured latency drifts from the configured value.
func (m *Matrix) ValidateLatencies(ctx context.Context, nodes []*types.NodeInfo) {
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

			actual := m.measureLatency(dcNodes[dc1], dcNodes[dc2])
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

func (m *Matrix) measureLatency(nodes1, nodes2 []*types.NodeInfo) int {
	if len(nodes1) == 0 || len(nodes2) == 0 {
		return 0
	}
	if nodes1[0].IP == "" || nodes2[0].IP == "" {
		return 0
	}
	return m.pingNode(nodes1[0].IP)
}

func (m *Matrix) pingNode(ip string) int {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", ip+pingProbePort, pingProbeTimeout)
	if err != nil {
		return 0
	}
	defer conn.Close()
	return int(time.Since(start).Milliseconds())
}
