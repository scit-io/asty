package asty

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// subscribeCommands subscribes to agent commands from server
func (a *Agent) subscribeCommands() error {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", a.nodeID)

	_, err := a.nc.Subscribe(subject, func(msg *nats.Msg) {
		a.handleCommand(msg)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	log.Info().Str("subject", subject).Msg("subscribed to commands")
	return nil
}

// handleCommand handles incoming commands from server
func (a *Agent) handleCommand(msg *nats.Msg) {
	cmd, err := UnmarshalCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to unmarshal command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("type", cmd.Type).
		Msg("received command")

	switch cmd.Type {
	case "start":
		a.handleStartCommand(msg, cmd.Data)
	case "stop":
		a.handleStopCommand(msg, cmd.Data)
	case "logs":
		a.handleLogsCommand(msg, cmd.Data)
	default:
		log.Error().Str("type", cmd.Type).Msg("unknown command type")
		msg.Respond(MarshalResponse(false, "", fmt.Errorf("unknown command type: %s", cmd.Type)))
	}
}

// handleStartCommand handles start service command
func (a *Agent) handleStartCommand(msg *nats.Msg, data []byte) {
	startCmd, err := ParseStartCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse start command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("starting service")

	if err := a.StartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to start service")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	msg.Respond(MarshalResponse(true, fmt.Sprintf("service %s started", startCmd.Service.Name), nil))
}

// handleStopCommand acknowledges the stop command immediately and runs the
// real shutdown asynchronously.
func (a *Agent) handleStopCommand(msg *nats.Msg, data []byte) {
	stopCmd, err := ParseStopCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse stop command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", stopCmd.ServiceName).
		Msg("stopping service")

	msg.Respond(MarshalResponse(true, fmt.Sprintf("service %s stop initiated", stopCmd.ServiceName), nil))

	go func() {
		if err := a.StopService(stopCmd.ServiceName); err != nil {
			log.Warn().Err(err).Str("service", stopCmd.ServiceName).Msg("background stop reported")
		}
	}()
}

// handleLogsCommand handles get logs command
func (a *Agent) handleLogsCommand(msg *nats.Msg, data []byte) {
	logsCmd, err := ParseGetLogsCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse logs command")
		msg.Respond(MarshalLogsResponse(nil, err))
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
		msg.Respond(MarshalLogsResponse(nil, err))
		return
	}

	logData, err := proc.GetLogs(logsCmd.Lines)
	if err != nil {
		log.Error().Err(err).Str("service", logsCmd.ServiceName).Msg("failed to get logs")
		msg.Respond(MarshalLogsResponse(nil, err))
		return
	}

	logs := splitLines(string(logData), logsCmd.Lines)

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("line_count", len(logs)).
		Msg("logs retrieved")

	msg.Respond(MarshalLogsResponse(logs, nil))
}
