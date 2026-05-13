package agent

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// subscribePing answers latency-probe requests with an empty payload so
// callers (proximity.RunValidation on the leader) can measure the NATS
// round-trip as an inter-node latency estimate. The probe carries no data
// in either direction — we only care about the RTT.
func (a *Agent) subscribePing() error {
	subject := fmt.Sprintf("asty.v1.agent.%s.ping", a.nodeID)
	_, err := a.nc.Subscribe(subject, func(msg *nats.Msg) {
		_ = msg.Respond(nil)
	})
	if err != nil {
		return fmt.Errorf("subscribe ping %s: %w", subject, err)
	}
	log.Info().Str("subject", subject).Msg("subscribed to ping")
	return nil
}
