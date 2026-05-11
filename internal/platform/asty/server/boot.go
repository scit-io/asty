package server

import (
	"context"
	"fmt"
	"io"
	"time"

	"asty/internal/platform/asty/core/netutil"
	"asty/internal/platform/asty/core/types"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/autoscaling"
	"asty/internal/platform/asty/features/clustering/discovery"
	"asty/internal/platform/asty/features/clustering/leader"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/draining"
	"asty/internal/platform/asty/features/observability/events"
	"asty/internal/platform/asty/features/observability/logs"
	"asty/internal/platform/asty/features/scheduling"
	"asty/internal/platform/asty/features/scheduling/proximity"

	apiPkg "asty/internal/platform/asty/features/api"

	"github.com/rs/zerolog/log"
)

// Tunables (metricsRetention, logBufferLines, …) live in tunables.go.

// Start brings up the server. Returns when ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	log.Info().Str("node_id", s.nodeID).Msg("server starting")

	if err := s.initInfra(); err != nil {
		return err
	}
	defer s.nc.Close()

	s.initFeatures(ctx)

	if err := s.startAPI(ctx); err != nil {
		return err
	}

	if err := s.runLeaderElection(ctx); err != nil {
		return err
	}

	go s.watchClusterNodes(ctx)
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

// initInfra brings up NATS, cluster state, leader election, and the
// node discovery client — everything the rest of the boot sequence
// depends on.
func (s *Server) initInfra() error {
	if err := s.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	clusterState, err := state.New(s.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	s.clusterState = clusterState

	leaderIP := s.cfg.NodeIP
	if leaderIP == "" {
		leaderIP = netutil.LocalIPv4(s.cfg.NATS.Host)
	}
	leaderElection, err := leader.NewElection(s.nc, s.nodeID, leaderIP)
	if err != nil {
		return fmt.Errorf("failed to initialize leader election: %w", err)
	}
	s.leaderElection = leaderElection
	s.nodeDiscovery = discovery.New(s.cfg.Domain)
	return nil
}

// initFeatures wires the higher-level dependencies (scheduler, autoscaler,
// deployer, …) that need infra in place. Also kicks off background
// pieces that don't depend on leadership: log buffering, gateway metrics
// ingest, proximity validation, the snapshot hub.
func (s *Server) initFeatures(ctx context.Context) {
	// Mirror server logs into NATS so the UI can stream them.
	natsWriter := logs.NewNATSWriter(s.nc, "asty.v1.server.logs")
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	s.scheduler = scheduling.NewScheduler(s.clusterState, s.cfg)
	s.metricsStore = autometrics.NewStore(metricsRetention)
	s.logBuffer = logs.NewBuffer(logBufferLines)
	s.eventBuffer = events.NewBuffer(eventBufferEntries)
	s.autoscaler = autoscaling.NewAutoscaler(s.clusterState, s.scheduler, s.cfg, s.metricsStore)

	s.proximityMatrix = proximity.NewMatrix()
	if err := s.proximityMatrix.LoadFromConfig(s.cfg.Autoscale.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}
	go proximity.RunValidation(ctx, s.proximityMatrix, s.clusterState)

	s.deployer = deployment.NewDeployer(s.clusterState, s.nc, deployment.DeployerConfig{})
	s.serviceLoader = deployment.NewServiceLoader(s.cfg.Agent.ServiceDir)
	services, err := s.serviceLoader.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load service definitions")
	}
	s.services = services

	s.drainManager = draining.NewDrainManager(s)

	s.subscribeGatewayMetrics()
	s.startLogBuffering()

	s.streamHub = newStreamHub(s, streamHubInterval)
	go s.streamHub.Run(ctx)
}

// startAPI launches the HTTP API in a goroutine and returns immediately.
// The API runs until ctx is cancelled and logs its own shutdown errors.
func (s *Server) startAPI(ctx context.Context) error {
	s.httpAPI = apiPkg.New(s, s.cfg.UI.Addr)
	go func() {
		if err := s.httpAPI.Start(ctx); err != nil {
			log.Error().Err(err).Msg("API server failed")
		}
	}()
	return nil
}

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
