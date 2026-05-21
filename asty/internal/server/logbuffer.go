package server

import (
	"strings"

	"asty/asty/internal/infra/logs"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// startLogBuffering subscribes to NATS log subjects and feeds parsed
// Events into logBuffer. History endpoints replay these straight to the
// dashboard SSE channel — no extra formatting on the way out.
func (s *Server) startLogBuffering() {
	if _, err := s.nc.Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		s.appendBuffered("cluster", msg.Data)
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe cluster log buffer")
	}

	// Subject pattern: asty.v1.agent.{nodeID}.logs.{service|"agent"}.
	if _, err := s.nc.Subscribe("asty.v1.agent.*.logs.*", func(msg *nats.Msg) {
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 6 {
			return
		}
		nodeID := parts[3]
		svc := parts[5]
		if svc == "agent" {
			s.appendBuffered("node."+nodeID, msg.Data)
		} else {
			s.appendBuffered("node."+nodeID+".svc."+svc, msg.Data)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe agent log buffer")
	}
}

// appendBuffered decodes one NATS message into a logs.Event and stores
// it under source. Decode failures are dropped: a buffer entry the
// dashboard cannot render is worse than a missing one.
func (s *Server) appendBuffered(source string, data []byte) {
	e, err := logs.ParseEvent(data)
	if err != nil {
		return
	}
	s.logBuffer.Append(source, *e)
}
