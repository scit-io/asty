package server

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// watchShutdownSignal ends Start when the LOCAL agent publishes an
// explicit shutdown event on asty.v1.server.<id>.shutdown.
//
// This replaced watchSelfRemoval which exited the server whenever its
// own node.<id> KV record disappeared. The KV-watch path conflated two
// very different events: an operator-driven shutdown (dashboard kill →
// agent deregisters) AND a per-key TTL eviction of a still-live
// node.<id> record when the KV bucket itself lost quorum. The latter
// turned every degraded-but-recoverable cluster into a cascade of
// server suicides — the exact opposite of what survivor recovery
// needs. Switching to a pub/sub event lets the server exit only on the
// explicit operator path; TTL-driven KV deletes are now ignored.
func (s *Server) watchShutdownSignal(ctx context.Context, cancel context.CancelFunc) {
	if s.nc == nil {
		return
	}
	subj := fmt.Sprintf("asty.v1.server.%s.shutdown", s.nodeID)
	sub, err := s.nc.Subscribe(subj, func(_ *nats.Msg) {
		log.Info().Str("node_id", s.nodeID).Str("subj", subj).Msg("shutdown signal received; stopping server")
		cancel()
	})
	if err != nil {
		log.Warn().Err(err).Str("subj", subj).Msg("subscribe to shutdown signal failed")
		return
	}
	<-ctx.Done()
	_ = sub.Unsubscribe()
}
