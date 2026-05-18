package gateway

import (
	"context"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"
)

// rpsReporterInterval — how often the gateway samples its valid-request
// counter and publishes a GatewayMetricsReport. 5 s matches the agent
// heartbeat cadence; with the autoscaler's 60 s trafficWindow this
// gives ~12 samples per averaging window — enough to smooth single-
// tick spikes without lagging real traffic shifts.
const rpsReporterInterval = 5 * time.Second

// ReportRPSLoop publishes a GatewayMetricsReport every
// rpsReporterInterval until ctx is cancelled. Exits cleanly on ctx.Done
// without flushing a final sample — losing one tick is preferable to
// blocking shutdown on a NATS publish.
func (gw *Gateway) ReportRPSLoop(ctx context.Context) {
	ticker := time.NewTicker(rpsReporterInterval)
	defer ticker.Stop()

	subject := types.MetricsGatewaySubject(gw.nodeID)
	var prev int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := gw.validRequests.Load()
			delta := cur - prev
			prev = cur

			report := types.GatewayMetricsReport{
				NodeID:   gw.nodeID,
				ValidRPS: float64(delta) / rpsReporterInterval.Seconds(),
			}
			data, err := codec.Wire.Marshal(report)
			if err != nil {
				gw.log.Error().Err(err).Msg("rps report marshal failed")
				continue
			}
			if err := gw.nats.Publish(subject, data); err != nil {
				gw.log.Warn().Err(err).Str("subject", subject).Msg("rps report publish failed")
			}
		}
	}
}
