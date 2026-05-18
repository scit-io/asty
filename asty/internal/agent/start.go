package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/features/clustering/state"
	"asty/asty/internal/features/execution/health"
	"asty/asty/internal/features/observability/logs"

	"github.com/rs/zerolog/log"
)

// Start brings up the agent: NATS connection, cluster state, log
// forwarding, health/metrics collectors, command subscriptions, and the
// background goroutines for heartbeats, metrics publishing, and process
// monitoring. Blocks until ctx is cancelled, then stops all processes.
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	a.exportConfigEnv()

	if err := a.bootstrapNATS(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap NATS: %w", err)
	}
	go a.superviseNATS(ctx)
	go a.watchNATSPeers(ctx)

	if err := a.connectAndWireNATS(ctx); err != nil {
		return err
	}
	defer a.nc.Close()
	if a.ncSys != nil {
		defer a.ncSys.Close()
	}

	if err := a.subscribeCommands(); err != nil {
		return fmt.Errorf("failed to subscribe to commands: %w", err)
	}
	if err := a.subscribePing(); err != nil {
		return fmt.Errorf("failed to subscribe to ping: %w", err)
	}

	go a.publishHeartbeat(ctx)
	go a.publishProcessMetrics(ctx)
	go a.monitorProcesses(ctx)
	go a.collectNATSStatsLoop(ctx)

	if err := a.runGateway(ctx); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent ready")

	<-ctx.Done()
	a.stopAllProcesses()
	return nil
}

// connectAndWireNATS opens the agent's main (ASTY) connection, the
// optional SYS-account observer connection, and the cluster-state
// adapter that fans out KV operations to the rest of the agent. Also
// re-routes zerolog's output through a NATS writer so every log line
// gets streamed on asty.v1.agent.<nodeID>.logs.agent.
func (a *Agent) connectAndWireNATS(ctx context.Context) error {
	host := a.cfg.NodeIP
	if host == "" {
		host = netutil.LocalIPv4("")
	}
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: host, Port: a.cfg.NATS.Server.Port,
		User: a.cfg.NATS.User, Password: a.cfg.NATS.Password,
	}, "asty-agent-"+a.nodeID)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	a.nc = nc

	// Separate connection in the SYS account, used exclusively by
	// natsstats.go for $SYS.REQ.SERVER.*.STATSZ/JSZ. Optional: if no
	// observer credentials are configured the agent still comes up and
	// the asty_node_nats_* metrics simply stay at zero.
	if a.cfg.NATS.ObserverUser != "" {
		ncSys, err := netutil.ConnectNATS(netutil.NATSCreds{
			Host: host, Port: a.cfg.NATS.Server.Port,
			User: a.cfg.NATS.ObserverUser, Password: a.cfg.NATS.ObserverPassword,
		}, "asty-observer-"+a.nodeID)
		if err != nil {
			return fmt.Errorf("failed to connect to NATS as observer: %w", err)
		}
		a.ncSys = ncSys
	}

	clusterState, err := state.New(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	a.healthChecker = health.NewChecker(a.nc)

	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := logs.NewNATSWriter(a.nc, agentSubject)
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	go a.healthChecker.Start(ctx)
	go a.metricsCollector.Start(ctx)
	a.metricsCollector.Register(os.Getpid(), "asty-agent")
	return nil
}
