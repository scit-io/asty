package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/infra/probe"
	"asty/asty/internal/infra/logs"

	"github.com/rs/zerolog/log"
)

// Start brings up the agent: NATS connection, cluster state, log
// forwarding, health/metrics collectors, command subscriptions, and the
// background goroutines for heartbeats, metrics publishing, and process
// monitoring. Blocks until ctx is cancelled, then stops all processes.
//
// Privilege ordering, when cfg.Agent.RunAsUser is set:
//
//  1. resolveDropTarget — fail fast on a bad user/group name.
//  2. bootstrapNATS — exec'd with SysProcAttr.Credential pointing at
//     the target uid/gid so the nats-server child starts non-root.
//  3. connectAndWireNATS — opens our two NATS connections.
//  4. preBindGateway — listen(2) on the privileged port WHILE we're
//     still root (the listener FD survives setuid).
//  5. dropPrivileges — chown work_dir + store_dir, setgid, setuid.
//     Everything that follows runs as the target user.
//  6. subscribe + background goroutines + gateway Serve on the pre-
//     bound listener.
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	a.exportConfigEnv()

	drop, err := a.resolveDropTarget()
	if err != nil {
		return fmt.Errorf("resolve run-as target: %w", err)
	}
	a.drop = drop

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

	gwListener, err := a.preBindGateway()
	if err != nil {
		return fmt.Errorf("failed to pre-bind gateway: %w", err)
	}

	if err := a.dropPrivileges(); err != nil {
		if gwListener != nil {
			_ = gwListener.Close()
		}
		return fmt.Errorf("drop privileges: %w", err)
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

	if err := a.runGatewayWith(ctx, gwListener); err != nil {
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

	clusterState, err := kv.New(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	a.healthChecker = probe.NewChecker(a.nc)

	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := logs.NewNATSWriter(a.nc, agentSubject)
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	go a.healthChecker.Start(ctx)
	go a.metricsCollector.Start(ctx)
	a.metricsCollector.Register(os.Getpid(), "asty-agent")
	return nil
}
