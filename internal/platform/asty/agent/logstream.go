package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/features/execution/process"

	"github.com/rs/zerolog/log"
)

// logStreamPresenceCheck — how often we verify the process is still in
// the agent's map. When a service is stopped, the agent removes it, and
// the goroutine should exit promptly to free resources. Phase 6.3 will
// replace this with a per-process context.
const logStreamPresenceCheck = 500 * time.Millisecond

// streamProcessLogs publishes lines from proc.TailLogs to the
// per-allocation NATS subject until the process is removed from the
// agent's map.
func (a *Agent) streamProcessLogs(serviceName string, proc *process.Process) {
	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", a.nodeID, serviceName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logLines := make(chan string, 100)
	go func() {
		if err := proc.TailLogs(ctx, logLines); err != nil && err != context.Canceled {
			log.Error().
				Err(err).
				Str("service", serviceName).
				Msg("failed to tail logs")
		}
		close(logLines)
	}()

	log.Info().Str("service", serviceName).Str("subject", subject).Msg("streaming logs to NATS")

	ticker := time.NewTicker(logStreamPresenceCheck)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-logLines:
			if !ok {
				log.Info().Str("service", serviceName).Msg("log channel closed, ending stream")
				return
			}
			if entry, err := json.Marshal(map[string]interface{}{
				"line":      line,
				"timestamp": time.Now().Unix(),
			}); err == nil {
				if pubErr := a.nc.Publish(subject, entry); pubErr != nil {
					log.Error().Err(pubErr).Str("subject", subject).Msg("failed to publish log line")
				}
			}
		case <-ticker.C:
			a.mu.RLock()
			_, exists := a.processes[serviceName]
			a.mu.RUnlock()
			if !exists {
				log.Info().Str("service", serviceName).Msg("process no longer exists, ending log stream")
				cancel()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
