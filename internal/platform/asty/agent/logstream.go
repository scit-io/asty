package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/features/execution/process"

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

	for {
		select {
		case line, ok := <-logLines:
			if !ok {
				log.Info().Str("service", serviceName).Msg("log channel closed, ending stream")
				return
			}
			entry, err := json.Marshal(map[string]interface{}{
				"line":      line,
				"timestamp": time.Now().Unix(),
			})
			if err != nil {
				continue
			}
			if pubErr := a.nc.Publish(subject, entry); pubErr != nil {
				log.Error().Err(pubErr).Str("subject", subject).Msg("failed to publish log line")
			}
		case <-ctx.Done():
			return
		}
	}
}
