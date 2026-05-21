package agent

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/infra/logs"
	"asty/asty/internal/infra/process"

	"github.com/rs/zerolog/log"
)

// logTailBuffer — capacity of the in-memory channel that the tailer
// fills and we drain. 100 lines is enough to absorb a burst without
// dropping; chatty services that exceed this lose old lines first.
const logTailBuffer = 100

// streamProcessLogs publishes lines from proc.TailLogs to the
// per-allocation NATS subject until the process exits. The TailLogs
// goroutine cancels its own context when the process's Done channel
// closes — no timers, no agent-map polling.
func (a *Agent) streamProcessLogs(serviceName string, proc *process.Process) {
	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", a.nodeID, serviceName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Tie the tailer's lifetime to the process: when the process
	// exits, this goroutine cancels ctx, which makes TailLogs return
	// and the main loop exit cleanly.
	go func() {
		<-proc.Done()
		cancel()
	}()

	logLines := make(chan string, logTailBuffer)
	go func() {
		if err := proc.TailLogs(ctx, logLines); err != nil && err != context.Canceled {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to tail logs")
		}
		close(logLines)
	}()

	log.Info().Str("service", serviceName).Str("subject", subject).Msg("streaming logs to NATS")

	tail(a, ctx, logLines, subject, serviceName)
}

// buildServiceEvent turns one stdout line from a managed service into an
// Event the dashboard can render. If the line is already a zerolog JSON
// object (the common case for our Go services), the structured shape is
// preserved verbatim and only the component is stamped over to the
// service name. Anything else — `printf` output, panics, raw text —
// lands in the Line slot and renders as a plain row.
func buildServiceEvent(service, line string) logs.Event {
	now := time.Now().Unix()
	if parsed, err := logs.ParseEvent([]byte(line)); err == nil && !parsed.IsLine() {
		parsed.Component = service
		if parsed.Timestamp == 0 {
			parsed.Timestamp = now
		}
		return *parsed
	}
	return logs.Event{Component: service, Line: line, Timestamp: now}
}

func tail(a *Agent, ctx context.Context, logLines <-chan string, subject, serviceName string) {
	for {
		select {
		case line, ok := <-logLines:
			if !ok {
				log.Info().Str("service", serviceName).Msg("log channel closed, ending stream")
				return
			}
			ev := buildServiceEvent(serviceName, line)
			if pubErr := a.nc.Publish(subject, ev.MarshalWire()); pubErr != nil {
				log.Error().Err(pubErr).Str("subject", subject).Msg("failed to publish log line")
			}
		case <-ctx.Done():
			return
		}
	}
}
