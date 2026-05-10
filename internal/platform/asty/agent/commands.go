package agent

import (
	"fmt"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

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

func (a *Agent) handleCommand(msg *nats.Msg) {
	cmd, err := types.UnmarshalCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to unmarshal command")
		msg.Respond(types.MarshalResponse(false, "", err))
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
		msg.Respond(types.MarshalResponse(false, "", fmt.Errorf("unknown command type: %s", cmd.Type)))
	}
}

func (a *Agent) handleStartCommand(msg *nats.Msg, data []byte) {
	startCmd, err := types.ParseStartCommand(data)
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

func (a *Agent) handleStopCommand(msg *nats.Msg, data []byte) {
	stopCmd, err := types.ParseStopCommand(data)
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

func (a *Agent) handleLogsCommand(msg *nats.Msg, data []byte) {
	logsCmd, err := types.ParseGetLogsCommand(data)
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

	logLines := tailLines(string(logData), logsCmd.Lines)

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("line_count", len(logLines)).
		Msg("logs retrieved")

	msg.Respond(types.MarshalLogsResponse(logLines, nil))
}
