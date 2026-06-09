package agent

import (
	"context"
	"fmt"
	"os"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/infra/logs"
	"asty/asty/internal/infra/probe"

	"github.com/rs/zerolog/log"
)

// Start brings up the agent: NATS connection, cluster state, log
// forwarding, health/metrics collectors, command subscriptions, and the
// background goroutines for heartbeats, metrics publishing, and process
// monitoring. Blocks until ctx is cancelled, then stops all processes.
//
// Privilege ordering, when the OS started us as root:
//
//  1. resolveDropTarget — looks up `nobody`. If we didn't start as
//     root, or there is no `nobody` user, drop is disabled.
//  2. bootstrapNATS — exec'd with SysProcAttr.Credential = nobody so
//     the nats-server child is non-root from the start. Required
//     because post-drop the agent (now nobody) couldn't signal a
//     root child.
//  3. connectAndWireNATS — opens our two NATS connections.
//  4. preBindGateway — listen(2) on the privileged port WHILE we're
//     still root (the listener FD survives setuid).
//  5. dropPrivileges — chown work_dir + store_dir to nobody, setgid,
//     setuid. Everything below runs as nobody.
//  6. subscribe + background goroutines + gateway Serve on the pre-
//     bound listener.
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	// Derive a child context so CmdShutdown can trigger the same
	// graceful path as the parent's SIGTERM (start.sh remove). Cancel
	// is also deferred so any early-error return below tears down
	// goroutines we've already spawned.
	ctx, cancel := context.WithCancel(ctx)
	a.shutdownFn = cancel
	defer cancel()

	a.exportConfigEnv()

	drop, err := a.resolveDropTarget()
	if err != nil {
		return fmt.Errorf("resolve run-as target: %w", err)
	}
	a.drop = drop

	if err := a.bootstrapNATS(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap NATS: %w", err)
	}
	// Belt-and-suspenders: any early-error return below leaves the
	// supervisor goroutine alive otherwise. The orderly-shutdown path
	// at the bottom of Start calls stopNATSSupervisor again — sync.Once
	// makes the second close a no-op.
	defer a.stopNATSSupervisor()
	go a.superviseNATS(ctx)

	if err := a.connectAndWireNATS(ctx); err != nil {
		return err
	}
	// Peer watcher must start AFTER connectAndWireNATS — it subscribes
	// to cluster KV (a.clusterState) for ongoing membership updates.
	// Bootstrap routes were rendered upstream from a.peers (Seed +
	// CmdAddPeer); the watcher now keeps a.peers in sync with the
	// authoritative node.<id> records in KV.
	go a.watchNATSPeers(ctx)
	// Collapse to a single standalone node if a 2-node cluster loses its
	// peer (event-driven on the JetStream quorum-lost advisory; see
	// natssolo.go).
	go a.watchQuorumLost(ctx)
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
	if err := a.subscribePeerAnnounce(); err != nil {
		return fmt.Errorf("failed to subscribe to peer-announce: %w", err)
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
	// A departing node behaves like a power-off — "unplugged from the
	// wall". It does NOT touch the NATS meta group or its streams. A
	// dying node decommissioning itself (the old $JS.API.SERVER.REMOVE
	// path) stalled the whole cluster's KV for ~30s; instead the
	// surviving leader reaps the dead peer (server/deadpeers.go) and
	// lowers stream replicas to fit the smaller cluster. RAFT carries a
	// 3+ node cluster through the loss in seconds; the final 2→1 step is
	// handled by the survivor itself (agent/natssolo.go: force streams to
	// R=1 on disk, restart standalone on the same store). See REPLICAS_KV.md.
	//
	// We still deregister from cluster KV: it is the event that tells the
	// survivor a peer left (watchNATSPeers), and it drops us from the
	// leader's snapshot without waiting out heartbeat staleness. The
	// supervisor keeps nats-server up (it waits on natsStopCh, not
	// ctx.Done()), so this KV.Delete has a live local broker; only after
	// it returns do we tear NATS down.
	if a.clusterState != nil {
		if err := a.clusterState.RemoveNode(a.nodeID); err != nil {
			log.Warn().Err(err).Msg("shutdown: failed to deregister node from cluster state")
		}
	}
	// Explicitly tell the local server process to stop. This is an event
	// the server treats as the ONLY legitimate self-shutdown signal —
	// it must not exit on KV-delete events, because the per-key TTL
	// reaping the node.<id> record under a degraded KV would otherwise
	// cascade every surviving node into shutdown when it most needs to
	// keep running. See server/shutdownsignal.go.
	if a.nc != nil {
		subj := fmt.Sprintf("asty.v1.server.%s.shutdown", a.nodeID)
		if err := a.nc.Publish(subj, nil); err != nil {
			log.Warn().Err(err).Str("subj", subj).Msg("shutdown: publish server-shutdown signal failed")
		}
		_ = a.nc.Flush()
	}
	// Stop spawned services first (while the local broker is still up so
	// they can clean up), then tear NATS down — stopNATSSupervisor blocks
	// until the nats-server child has actually exited, so the process never
	// returns out from under a still-running broker.
	a.stopAllProcesses()
	a.stopNATSSupervisor()
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

	logs.AttachNATS(a.nc, fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID))

	go a.healthChecker.Start(ctx)
	go a.metricsCollector.Start(ctx)
	a.metricsCollector.Register(os.Getpid(), "asty-agent")
	return nil
}
