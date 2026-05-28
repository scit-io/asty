package server

import (
	"context"
	"encoding/json"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/deployer"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// subscribeGatewayMetrics ingests per-gateway valid-RPS reports.
// Each agent's embedded gateway publishes one report every
// rpsReporterInterval on types.MetricsGatewaySubject(nodeID); the
// autoscaler uses these samples (averaged over trafficWindow) to
// detect traffic hitting nodes that don't have the service yet —
// the locality-aware scale-up trigger.
func (s *Server) subscribeGatewayMetrics() {
	subject := types.MetricsGatewayPattern()
	_, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var report types.GatewayMetricsReport
		if err := codec.Wire.Unmarshal(msg.Data, &report); err != nil {
			return
		}
		s.metricsStore.AddRPS(report.NodeID, report.ValidRPS)
		for svc, rps := range report.Services {
			s.metricsStore.AddServiceRPS(report.NodeID, svc, rps)
		}
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to gateway metrics")
	}
}

// subscribeScalingEvents mirrors autoscaler decisions and manual scale
// actions into the local metrics ring. Publishers (autoscaler.recordEvent,
// dashboard.recordManualScaleEvent) call PublishEvent which broadcasts
// on ScalingEventSubject; this subscriber adds the received event to
// the local ring so any dashboard listener — leader or follower —
// returns the same Scaling-events table regardless of routing.
func (s *Server) subscribeScalingEvents(ctx context.Context) {
	sub, err := s.nc.Subscribe(autometrics.ScalingEventSubject, func(msg *nats.Msg) {
		var evt autometrics.ScalingEvent
		if err := codec.Wire.Unmarshal(msg.Data, &evt); err != nil {
			log.Warn().Err(err).Msg("scaling event: malformed payload, dropping")
			return
		}
		s.metricsStore.AddEvent(evt)
	})
	if err != nil {
		log.Error().Err(err).Str("subject", autometrics.ScalingEventSubject).Msg("failed to subscribe to scaling events")
		return
	}
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}

// subscribeDeployHistory replays deploy-progress messages
// (asty.v1.deploy.progress.<service>) into the local deployer's
// history ring. The publisher (deployer.persistLast on the leader)
// updates its own ring directly; this subscription is what keeps
// every other server's `GET /services/{name}/deploy` consistent.
// Records are upserted by ID so the same record can transition
// running → completed/reverted/failed on follower nodes.
func (s *Server) subscribeDeployHistory(ctx context.Context) {
	sub, err := s.nc.Subscribe("asty.v1.deploy.progress.>", func(msg *nats.Msg) {
		var rec deployer.DeploymentRecord
		if err := json.Unmarshal(msg.Data, &rec); err != nil {
			log.Warn().Err(err).Msg("deploy history: malformed payload, dropping")
			return
		}
		s.deployer.ApplyRemoteRecord(rec)
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to subscribe to deploy progress")
		return
	}
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}
