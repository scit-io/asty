package server

import (
	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// subscribeGatewayMetrics ingests per-gateway valid-RPS reports.
// Each agent's embedded gateway publishes one report every
// rpsReporterInterval on types.MetricsGatewaySubject(nodeID); the
// autoscaler uses these samples (averaged over trafficWindow) to
// detect traffic hitting nodes that don't have the service yet —
// the locality-aware scale-up trigger documented in CLAUDE.md.
func (s *Server) subscribeGatewayMetrics() {
	subject := types.MetricsGatewayPattern()
	_, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var report types.GatewayMetricsReport
		if err := codec.Wire.Unmarshal(msg.Data, &report); err != nil {
			return
		}
		s.metricsStore.AddRPS(report.NodeID, report.ValidRPS)
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to gateway metrics")
	}
}
