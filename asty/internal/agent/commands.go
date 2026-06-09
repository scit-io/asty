package agent

import (
	"bufio"
	"bytes"
	"fmt"

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
	case types.CmdAddPeer:
		a.handleAddPeerCommand(msg)
	default:
		log.Error().Str("kind", string(kind)).Msg("unknown command kind")
		_ = msg.Respond(types.MarshalResponse(false, "", fmt.Errorf("unknown command kind: %s", kind)))
	}
}

func (a *Agent) handleRestartCommand(msg *nats.Msg) {
	startCmd, err := types.ParseStartCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse restart command")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("restarting service")

	if err := a.RestartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to restart service")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	_ = msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s restarted", startCmd.Service.Name), nil))
}

func (a *Agent) handleStartCommand(msg *nats.Msg) {
	startCmd, err := types.ParseStartCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse start command")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("starting service")

	if err := a.StartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to start service")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	_ = msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s started", startCmd.Service.Name), nil))
}

func (a *Agent) handleStopCommand(msg *nats.Msg) {
	stopCmd, err := types.ParseStopCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse stop command")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", stopCmd.ServiceName).
		Msg("stopping service")

	_ = msg.Respond(types.MarshalResponse(true, fmt.Sprintf("service %s stop initiated", stopCmd.ServiceName), nil))

	go func() {
		if err := a.StopService(stopCmd.ServiceName); err != nil {
			log.Warn().Err(err).Str("service", stopCmd.ServiceName).Msg("background stop reported")
		}
	}()
}

// handleAddPeerCommand registers an incoming NATS-route peer so the
// supervisor can rewrite cluster.routes (SIGHUP, or cold restart on
// the standalone→clustered flip). Called by the `asty admin add-peer`
// CLI when a brand-new node SSH's into one cluster member during its
// deploy. After registering locally, we broadcast the peer on
// peerAnnounceSubject so every OTHER node adds the same IP — that
// mirrors the DNS-era behavior where all nodes saw the new A-record
// at once and built a full route mesh. Without the broadcast, only
// the contacted node would know about the joiner; the cluster-leader
// (which may sit on a different node) would have to wait for NATS
// gossip to propagate, missing the gossipChanged signal that drives
// stream-replicas upgrade.
//
// Why so trim: the payload carries just the IP. We don't ask for
// node_id/host — those flow in later via the cluster KV once the new
// node finishes joining and writes its own NodeInfo. The bootstrap
// set is just enough to open :6222 in time for the join to succeed.
func (a *Agent) handleAddPeerCommand(msg *nats.Msg) {
	cmd, err := types.ParseAddPeerCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse add-peer command")
		_ = msg.Respond(types.MarshalResponse(false, "", err))
		return
	}
	if cmd.IP == "" {
		_ = msg.Respond(types.MarshalResponse(false, "", fmt.Errorf("add-peer: empty ip")))
		return
	}
	if a.applyPeerAnnounce(cmd.IP) {
		// Only broadcast the first time we learn about this peer — keeps
		// the announce subject quiet under normal heartbeat traffic.
		if payload, err := types.MarshalAddPeerCommand(cmd.IP); err == nil {
			if err := a.nc.Publish(types.PeerAnnounceSubject, payload); err != nil {
				log.Warn().Err(err).Str("ip", cmd.IP).Msg("add-peer: broadcast publish failed")
			}
		}
	}
	_ = msg.Respond(types.MarshalResponse(true, "peer registered", nil))
}

// applyPeerAnnounce records a peer IP and signals the supervisor.
// Returns whether the IP was new (so the caller can decide whether
// to broadcast). Self-IP gets filtered inside addBootstrap.
func (a *Agent) applyPeerAnnounce(ip string) bool {
	selfIP := a.resolveNodeIP()
	if !a.peers.addBootstrap(ip, selfIP) {
		log.Debug().Str("ip", ip).Msg("add-peer: already known, no-op")
		return false
	}
	// An incoming-peer signal is the only event that legitimately exits
	// solo recovery — KV-watch upserts on their own can be stale
	// post-natssolo and must NOT clear the flag (see watchNATSPeers).
	a.inSolo.Store(false)
	a.signalNATSRestart()
	log.Info().Str("ip", ip).Msg("add-peer: registered, signalled nats-server restart")
	return true
}

// subscribePeerAnnounce subscribes to the cluster-wide peer-announce
// subject. Every agent's CmdAddPeer handler publishes there after a
// local registration; the subscriber on each remote node mirrors the
// addBootstrap+SIGHUP locally, so a single SSH'd add-peer reaches the
// whole cluster.
func (a *Agent) subscribePeerAnnounce() error {
	_, err := a.nc.Subscribe(types.PeerAnnounceSubject, func(msg *nats.Msg) {
		cmd, err := types.ParseAddPeerCommand(msg.Data)
		if err != nil {
			log.Warn().Err(err).Msg("peer-announce: parse failed")
			return
		}
		if cmd.IP == "" {
			return
		}
		a.applyPeerAnnounce(cmd.IP)
	})
	if err != nil {
		return fmt.Errorf("subscribe peer announce: %w", err)
	}
	return nil
}

// handleShutdownCommand acks first, then cancels the agent's ctx so
// the graceful path (decommission NATS, stop services, deregister
// node) runs — same flow as SIGTERM in start.sh remove.
func (a *Agent) handleShutdownCommand(msg *nats.Msg) {
	log.Info().Str("node_id", a.nodeID).Msg("shutdown command received")
	_ = msg.Respond(types.MarshalResponse(true, "shutdown initiated", nil))
	a.shutdownFn()
}

func (a *Agent) handleLogsCommand(msg *nats.Msg) {
	logsCmd, err := types.ParseGetLogsCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse logs command")
		_ = msg.Respond(types.MarshalLogsResponse(nil, err))
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
		_ = msg.Respond(types.MarshalLogsResponse(nil, err))
		return
	}

	logData, err := proc.GetLogs(logsCmd.Lines)
	if err != nil {
		log.Error().Err(err).Str("service", logsCmd.ServiceName).Msg("failed to get logs")
		_ = msg.Respond(types.MarshalLogsResponse(nil, err))
		return
	}

	// GetLogs already bounded the result to logsCmd.Lines lines; here we
	// just split into individual entries and drop empties. bufio.Scanner
	// streams the dump instead of materialising every line up front, which
	// matters for large follow-mode windows.
	var logLines []string
	sc := bufio.NewScanner(bytes.NewReader(logData))
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			logLines = append(logLines, line)
		}
	}

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("line_count", len(logLines)).
		Msg("logs retrieved")

	_ = msg.Respond(types.MarshalLogsResponse(logLines, nil))
}
