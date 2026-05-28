package server

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/autoscaler"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/leader"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/drainer"
	"asty/asty/internal/infra/events"
	"asty/asty/internal/infra/logs"
	"asty/asty/internal/ops/scheduler"
	"asty/asty/internal/domain/proximity"

	apiPkg "asty/asty/internal/api/dashboard"

	"github.com/rs/zerolog/log"
)

// Tunables (metricsRetention, logBufferLines, …) live in tunables.go.

// Start brings up the server. Returns when ctx is cancelled (parent
// signal, or watchSelfRemoval firing on our own KV-entry delete).
func (s *Server) Start(parent context.Context) error {
	log.Info().Str("node_id", s.nodeID).Msg("server starting")

	if err := s.initInfra(); err != nil {
		return err
	}
	defer s.nc.Close()

	// Child ctx so watchSelfRemoval can end Start on its own — see
	// selfremoval.go.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s.lifeCtx = ctx

	s.initFeatures(ctx)

	if err := s.startAPI(ctx); err != nil {
		return err
	}

	if err := s.runLeaderElection(ctx); err != nil {
		return err
	}

	go s.watchSelfRemoval(ctx, cancel)
	s.seedDevMockNodes()

	// Start leader-scoped work once if we're already the leader; the
	// watcher re-arms it on later flips.
	if s.leaderElection.IsLeader() {
		s.startLeaderWork(ctx)
	}
	go s.watchLeadership(ctx)

	log.Info().Msg("server ready")

	<-ctx.Done()
	s.stopLeaderWork()
	return nil
}

// initInfra brings up NATS, cluster state, and leader election —
// everything the rest of the boot sequence depends on.
func (s *Server) initInfra() error {
	if err := s.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	clusterState, err := kv.New(s.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	s.clusterState = clusterState

	leaderIP := s.cfg.NodeIP
	if leaderIP == "" {
		leaderIP = netutil.LocalIPv4("")
	}
	leaderElection, err := leader.NewElection(s.nc, s.nodeID, leaderIP)
	if err != nil {
		return fmt.Errorf("failed to initialize leader election: %w", err)
	}
	s.leaderElection = leaderElection
	return nil
}

// initFeatures wires the higher-level dependencies (scheduler, autoscaler,
// deployer, …) that need infra in place. Also kicks off background
// pieces that don't depend on leadership: log buffering, gateway RPS
// ingest, proximity validation, the snapshot hub.
func (s *Server) initFeatures(ctx context.Context) {
	logs.AttachNATS(s.nc, "asty.v1.server.logs")

	s.scheduler = scheduler.NewScheduler(s.clusterState, s.cfg)
	s.metricsStore = autometrics.NewStore(metricsRetention)
	s.metricsStore.AttachNATS(s.nc)
	s.subscribeScalingEvents(ctx)
	s.logBuffer = logs.NewBuffer(logBufferLines)
	s.eventBuffer = events.NewBuffer(eventBufferEntries)
	s.autoscaler = autoscaler.NewAutoscaler(s.clusterState, s.scheduler, s.cfg, s.metricsStore)

	s.proximityMatrix = proximity.NewMatrix()
	if err := s.proximityMatrix.LoadFromConfig(s.cfg.Autoscale.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}
	go proximity.RunValidation(ctx, s.proximityMatrix, s.clusterState, s.pingPair)

	s.deployer = deployer.NewDeployer(s.clusterState, s.nc, s.sendRestartCommand)
	s.subscribeDeployHistory(ctx)
	s.serviceLoader = deployer.NewServiceLoader(s.cfg.Agent.ServiceDir)
	services, err := s.serviceLoader.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load service definitions")
	}
	s.services = services

	s.drainManager = drainer.NewDrainManager(s)

	s.subscribeGatewayMetrics()
	s.startLogBuffering()

	s.streamHub = newStreamHub(s, streamHubInterval)
	go s.streamHub.Run(ctx)
}

// startAPI launches the dashboard HTTP API in a goroutine and (when
// the dashboard and prometheus ports differ) a second listener for
// the Prometheus exposition. Both run until ctx is cancelled and log
// their own shutdown errors.
func (s *Server) startAPI(ctx context.Context) error {
	s.httpAPI = apiPkg.New(s)
	go func() {
		if err := s.httpAPI.Start(ctx); err != nil {
			log.Error().Err(err).Msg("dashboard API server failed")
		}
	}()
	if s.cfg.Dashboard.Port != s.cfg.Prometheus.Port {
		go s.runStandalonePrometheus(ctx)
	}
	return nil
}

// runStandalonePrometheus lives in prometheus_listener.go.

// runLeaderElection starts the campaign goroutine and blocks until any
// node (us or another) has been elected.
func (s *Server) runLeaderElection(ctx context.Context) error {
	go s.leaderElection.CampaignForLeader(ctx)
	leaderInfo, err := s.leaderElection.WaitForLeader(ctx)
	if err != nil {
		return fmt.Errorf("leader election failed: %w", err)
	}
	log.Info().
		Str("leader", leaderInfo.ID).
		Str("leader_ip", leaderInfo.IP).
		Bool("is_leader", leaderInfo.ID == s.nodeID).
		Msg("leader elected")
	return nil
}

// seedDevMockNodes inserts fake "ready" nodes into KV when running in
// dev mode with A_MOCK_NODES set. This lets a single-process developer
// see scheduling decisions without standing up real agents.
func (s *Server) seedDevMockNodes() {
	if !s.cfg.DevMode || s.cfg.MockNodes <= 0 {
		return
	}
	log.Info().Int("count", s.cfg.MockNodes).Msg("creating mock nodes (dev mode)")
	for i := 1; i <= s.cfg.MockNodes; i++ {
		node := &types.NodeInfo{
			ID:              fmt.Sprintf("mock-node-%d", i),
			Datacenter:      s.cfg.Datacenter,
			IP:              fmt.Sprintf("192.168.1.%d", i),
			Status:          types.NodeReady,
			LastSeen:        time.Now(),
			CPUTotal:        devMockNodeCPUTotal,
			CPUAvailable:    devMockNodeCPUAvail,
			MemoryTotal:     devMockNodeMemoryTotal,
			MemoryAvailable: devMockNodeMemoryAvail,
			Processes:       []string{},
		}
		if err := s.clusterState.UpdateNode(node); err != nil {
			log.Error().Err(err).Str("node_id", node.ID).Msg("failed to register mock node")
		}
	}
}
