package server

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// gatewayMetricsSubject is the wildcard pattern Gateway instances
// publish to. The autoscaler uses these RPS samples to detect traffic
// hitting nodes that don't have the service yet (locality-aware
// scale-up).
const gatewayMetricsSubject = "asty.v1.metrics.gateway.*"

// subscribeGatewayMetrics ingests per-gateway valid-RPS reports.
func (s *Server) subscribeGatewayMetrics() {
	_, err := s.nc.Subscribe(gatewayMetricsSubject, func(msg *nats.Msg) {
		var report struct {
			NodeID   string  `json:"node_id"`
			ValidRPS float64 `json:"valid_rps"`
		}
		if err := json.Unmarshal(msg.Data, &report); err != nil {
			return
		}
		s.metricsStore.AddRPS(report.NodeID, report.ValidRPS)
	})
	if err != nil {
		log.Error().Err(err).Str("subject", gatewayMetricsSubject).Msg("failed to subscribe to gateway metrics")
	}
}
