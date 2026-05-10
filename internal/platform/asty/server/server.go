package server

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/controller"
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

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Server handles scheduling, autoscaling, and orchestration.
type Server struct {
	cfg    *config.Config
	nc     *nats.Conn
	nodeID string

	clusterState    *state.ClusterState
	leaderElection  *leader.Election
	nodeDiscovery   *discovery.NodeDiscovery
	scheduler       *scheduling.Scheduler
	autoscaler      *autoscaling.Autoscaler
	proximityMatrix *proximity.Matrix
	deployer        *deployment.Deployer
	serviceLoader   *deployment.ServiceLoader
	services        []*types.ServiceDefinition
	httpAPI         *apiPkg.API
	metricsStore    *autometrics.Store
	logBuffer       *logs.Buffer
	eventBuffer     *events.Buffer
	drainManager    *draining.DrainManager
	streamHub       *streamHub

	// Leadership-scoped goroutines (scheduler/autoscaler) run under leaderCtx,
	// which is cancelled on loss of leadership. mu guards swaps when leadership
	// flips.
	mu           sync.Mutex
	leaderCancel context.CancelFunc
}

// New creates a new Server.
func New(cfg *config.Config) (*Server, error) {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	return &Server{
		cfg:    cfg,
		nodeID: nodeID,
	}, nil
}

// Start starts the server.
func (s *Server) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", s.nodeID).
		Msg("server starting")

	// Connect to NATS
	if err := s.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer s.nc.Close()

	// Initialize cluster state
	clusterState, err := state.New(s.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	s.clusterState = clusterState

	// Initialize leader election
	leaderIP := s.cfg.NodeIP
	if leaderIP == "" {
		leaderIP = getNodeIP(s.cfg.NATSHost)
	}
	leaderElection, err := leader.NewElection(s.nc, s.nodeID, leaderIP)
	if err != nil {
		return fmt.Errorf("failed to initialize leader election: %w", err)
	}
	s.leaderElection = leaderElection

	// Initialize node discovery
	s.nodeDiscovery = discovery.New(s.cfg.Domain)

	// Initialize scheduler
	s.scheduler = scheduling.NewScheduler(clusterState, s.cfg)

	// Attach NATS writer to zerolog — all server logs stream to UI
	natsWriter := logs.NewNATSWriter(s.nc, "asty.v1.server.logs")
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	// Initialize metrics store (2h in-memory; hub feeds it, no KV-polling loop).
	s.metricsStore = autometrics.NewStore(2 * time.Hour)
	s.logBuffer = logs.NewBuffer(1000)
	s.eventBuffer = events.NewBuffer(10000)

	// Initialize autoscaler
	s.autoscaler = autoscaling.NewAutoscaler(clusterState, s.scheduler, s.cfg, s.metricsStore)

	// Initialize proximity matrix
	s.proximityMatrix = proximity.NewMatrix()
	if err := s.proximityMatrix.LoadFromConfig(s.cfg.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}

	// Start proximity validation
	go proximity.RunValidation(ctx, s.proximityMatrix, clusterState)

	// Initialize deployer
	s.deployer = deployment.NewDeployer(clusterState, s.nc, deployment.DeployerConfig{})

	// Initialize service loader
	s.serviceLoader = deployment.NewServiceLoader(s.cfg.ServiceDir)

	// Load service definitions
	services, err := s.serviceLoader.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load service definitions")
	}
	s.services = services

	// Initialize drain manager
	s.drainManager = draining.NewDrainManager(s)

	// Subscribe to Gateway RPS metrics
	s.subscribeGatewayMetrics()

	// Buffer logs from NATS into logBuffer for history endpoint.
	s.startLogBuffering()

	// Single shared snapshot source for all SSE handlers — refreshes every 5s.
	s.streamHub = newStreamHub(s, 5*time.Second)
	go s.streamHub.Run(ctx)

	// Initialize API server
	s.httpAPI = apiPkg.New(s, s.cfg.UIAddr)

	// Start API server
	go func() {
		if err := s.httpAPI.Start(ctx); err != nil {
			log.Error().Err(err).Msg("API server failed")
		}
	}()

	// Start leader election campaign
	go s.leaderElection.CampaignForLeader(ctx)

	// Wait for leader election
	leaderInfo, err := s.leaderElection.WaitForLeader(ctx)
	if err != nil {
		return fmt.Errorf("leader election failed: %w", err)
	}

	log.Info().
		Str("leader", leaderInfo.ID).
		Str("leader_ip", leaderInfo.IP).
		Bool("is_leader", leaderInfo.ID == s.nodeID).
		Msg("leader elected")

	// Discover cluster nodes
	go s.watchClusterNodes(ctx)

	// Create mock nodes in dev mode
	if s.cfg.DevMode && s.cfg.MockNodes > 0 {
		log.Info().Int("count", s.cfg.MockNodes).Msg("creating mock nodes (dev mode)")
		for i := 1; i <= s.cfg.MockNodes; i++ {
			node := &types.NodeInfo{
				ID:              fmt.Sprintf("mock-node-%d", i),
				Datacenter:      s.cfg.Datacenter,
				IP:              fmt.Sprintf("192.168.1.%d", i),
				Status:          "ready",
				LastSeen:        time.Now(),
				CPUTotal:        4000, // 4 cores * 1000 MHz
				CPUAvailable:    3500,
				MemoryTotal:     8192, // 8 GB
				MemoryAvailable: 6144,
				Processes:       []string{},
			}
			if err := s.clusterState.UpdateNode(node); err != nil {
				log.Error().Err(err).Str("node_id", node.ID).Msg("failed to register mock node")
			}
		}
	}

	// Single source of truth for leader-scoped work — start it once, watcher
	// re-arms it on flips.
	if s.leaderElection.IsLeader() {
		s.startLeaderWork(ctx)
	}
	go s.watchLeadership(ctx)

	log.Info().Msg("server ready")

	<-ctx.Done()
	s.stopLeaderWork()
	return nil
}

// startLeaderWork spawns the controller under a sub-context derived from
// the server context. Cancellation of that sub-context (on loss of
// leadership) stops the controller — workers drain, watchers exit, the
// workqueue shuts down. Idempotent: a second call while already running is
// a no-op.
func (s *Server) startLeaderWork(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderCancel != nil {
		return
	}
	leaderCtx, cancel := context.WithCancel(parent)
	s.leaderCancel = cancel

	workers := s.cfg.ControllerWorkers
	resync := s.cfg.EvalInterval
	if resync <= 0 {
		resync = 60 * time.Second
	} else {
		// Resync at 6x EvalInterval (so EvalInterval=10s -> resync=60s).
		// Watchers handle the reactive path; the safety-net resync only
		// catches drift and drives metric-driven autoscale.
		resync = resync * 6
		if resync > 5*time.Minute {
			resync = 5 * time.Minute
		}
	}
	ctrl := controller.NewServiceController(
		s.clusterState,
		s.scheduler,
		s.autoscaler,
		s.services,
		serverDispatcher{s},
		workers,
		resync,
	)
	ctrl.OnEvent = s.addClusterEvent
	go ctrl.Run(leaderCtx)
}

func (s *Server) stopLeaderWork() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderCancel != nil {
		s.leaderCancel()
		s.leaderCancel = nil
	}
}

// addClusterEvent stores e in the event buffer and fans it out to all active
// SSE subscribers via the stream hub.
func (s *Server) addClusterEvent(e types.ClusterEvent) {
	s.eventBuffer.Add(e)
	if s.streamHub != nil {
		s.streamHub.FanoutEvent(e)
	}
}
