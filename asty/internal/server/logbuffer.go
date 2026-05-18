package server

import (
	"fmt"
	"strings"

	"asty/asty/internal/infra/logs"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// startLogBuffering subscribes to NATS log subjects and feeds lines into
// logBuffer so that history endpoints can serve recent logs without a
// live SSE connection.
func (s *Server) startLogBuffering() {
	parseAndAppend := s.makeLogAppender()

	if _, err := s.nc.Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		parseAndAppend("cluster", msg.Data)
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
			parseAndAppend("node."+nodeID, msg.Data)
		} else {
			parseAndAppend("node."+nodeID+".svc."+svc, msg.Data)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe agent log buffer")
	}
}

// makeLogAppender returns a closure that decodes a zerolog JSON entry
// and appends it to the in-memory log buffer under the given source key.
// For logstream-wrapped process-stdout lines, the raw text is stored
// verbatim instead of the zerolog "[time] [level] message" template
// (which would render empty body, since logstream frames have no level
// or message).
func (s *Server) makeLogAppender() func(source string, data []byte) {
	return func(source string, data []byte) {
		e, err := logs.DecodeZerologEntry(data)
		if err != nil {
			return
		}
		if lf, ok := e.AsLineFrame(); ok {
			s.logBuffer.Append(source, logs.LogLine{Timestamp: lf.Timestamp, Line: lf.Line})
			return
		}
		timeStr := ""
		if e.Timestamp > 0 {
			timeStr = fmt.Sprintf("%d", e.Timestamp)
		}
		line := fmt.Sprintf("[%s] [%s] %s", timeStr, e.Level, e.Message)
		s.logBuffer.Append(source, logs.LogLine{Timestamp: e.Timestamp, Level: e.Level, Line: line})
	}
}
