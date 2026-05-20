package agent

import (
	"fmt"
	"strings"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

func (a *Agent) subscribeCommands() error {
	subject := types.CommandPattern(a.nodeID)

	_, err := a.nc.Subscribe(subject, func(msg *nats.Msg) {
		a.handleCommand(msg)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	log.Info().Str("subject", subject).Msg("subscribed to commands")
	return nil
}

func (a *Agent) handleCommand(msg *nats.Msg) {
	kind := types.CommandKindFromSubject(msg.Subject)

	log.Info().
		Str("kind", string(kind)).
		Msg("received command")

	switch kind {
	case types.CmdStart:
		a.handleStartCommand(msg)
	case types.CmdRestart:
		a.handleRestartCommand(msg)
	case types.CmdStop:
		a.handleStopCommand(msg)
	case types.CmdLogs:
		a.handleLogsCommand(msg)
	case types.CmdShutdown:
		a.handleShutdownCommand(msg)
	default:
		log.Error().Str("kind", string(kind)).Msg("unknown command kind")
		msg.Respond(types.MarshalResponse(false, "", fmt.Errorf("unknown command kind: %s", kind)))
	}
}

func (a *Agent) handleRestartCommand(msg *nats.Msg) {
	startCmd, err := types.ParseStartCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse restart command")
		msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("restarting service")

	if err := a.RestartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to restart service")
		msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s restarted", startCmd.Service.Name), nil))
}

func (a *Agent) handleStartCommand(msg *nats.Msg) {
	startCmd, err := types.ParseStartCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse start command")
		msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("starting service")

	if err := a.StartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to start service")
		msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s started", startCmd.Service.Name), nil))
}

func (a *Agent) handleStopCommand(msg *nats.Msg) {
	stopCmd, err := types.ParseStopCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse stop command")
		msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", stopCmd.ServiceName).
		Msg("stopping service")

	msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s stop initiated", stopCmd.ServiceName), nil))

	go func() {
		if err := a.StopService(stopCmd.ServiceName); err != nil {
			log.Warn().Err(err).Str("service", stopCmd.ServiceName).Msg("background stop reported")
		}
	}()
}

// handleShutdownCommand acks first, then cancels the agent's ctx so
// the graceful path (decommission NATS, stop services, deregister
// node) runs — same flow as SIGTERM in start.sh remove.
func (a *Agent) handleShutdownCommand(msg *nats.Msg) {
	log.Info().Str("node_id", a.nodeID).Msg("shutdown command received")
	msg.Respond(types.MarshalResponse(true, "shutdown initiated", nil))
	a.shutdownFn()
}

func (a *Agent) handleLogsCommand(msg *nats.Msg) {
	logsCmd, err := types.ParseGetLogsCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse logs command")
		msg.Respond(types.MarshalLogsResponse(nil, err))
		return
	}

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("lines", logsCmd.Lines).
		Bool("follow", logsCmd.Follow).
		Msg("retrieving logs")

	a.mu.RLock()
	proc, exists := a.processes[logsCmd.ServiceName]
	a.mu.RUnlock()

	if !exists {
		err := fmt.Errorf("service %s not running", logsCmd.ServiceName)
		log.Warn().Err(err).Str("service", logsCmd.ServiceName).Msg("service not found")
		msg.Respond(types.MarshalLogsResponse(nil, err))
		return
	}

	logData, err := proc.GetLogs(logsCmd.Lines)
	if err != nil {
		log.Error().Err(err).Str("service", logsCmd.ServiceName).Msg("failed to get logs")
		msg.Respond(types.MarshalLogsResponse(nil, err))
		return
	}

	// GetLogs already bounded the result to logsCmd.Lines lines; here we
	// just split into individual entries and drop empties.
	var logLines []string
	for _, line := range strings.Split(string(logData), "\n") {
		if line != "" {
			logLines = append(logLines, line)
		}
	}

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("line_count", len(logLines)).
		Msg("logs retrieved")

	msg.Respond(types.MarshalLogsResponse(logLines, nil))
}
