package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"asty/asty/internal/features/observability/logs"

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
// Splitting it out keeps startLogBuffering focused on subscription wiring.
func (s *Server) makeLogAppender() func(source string, data []byte) {
	return func(source string, data []byte) {
		var entry map[string]interface{}
		if err := json.Unmarshal(data, &entry); err != nil {
			return
		}
		level, _ := entry["level"].(string)
		msg, _ := entry["message"].(string)

		var ts int64
		if v, ok := entry["timestamp"].(float64); ok {
			ts = int64(v)
		}

		timeStr := ""
		if t, ok := entry["time"].(string); ok {
			timeStr = t
		} else if ts > 0 {
			timeStr = fmt.Sprintf("%d", ts)
		}

		line := fmt.Sprintf("[%s] [%s] %s", timeStr, level, msg)
		s.logBuffer.Append(source, logs.LogLine{Timestamp: ts, Level: level, Line: line})
	}
}
