package asty

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Server handles scheduling, autoscaling, and orchestration
type Server struct {
	cfg    *Config
	nc     *nats.Conn
	nodeID string

	clusterState   *ClusterState
	leaderElection *LeaderElection
	nodeDiscovery  *NodeDiscovery
	scheduler      *Scheduler
	autoscaler     *Autoscaler
	proximityMatrix *ProximityMatrix
	deployer       *Deployer
	serviceLoader  *ServiceLoader
	services       []*ServiceDefinition
	api            *API
	metricsStore   *MetricsStore
	logBuffer      *LogBuffer
	eventBuffer    *EventBuffer
	drainManager   *DrainManager
	streamHub      *streamHub

	// Leadership-scoped goroutines (scheduler/autoscaler) run under leaderCtx,
	// which is cancelled on loss of leadership. mu guards swaps when leadership
	// flips.
	mu           sync.Mutex
	leaderCancel context.CancelFunc
}

// NewServer creates a new Asty server
func NewServer(cfg *Config) (*Server, error) {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	return &Server{
		cfg:    cfg,
		nodeID: nodeID,
	}, nil
}

// Start starts the server
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
	clusterState, err := NewClusterState(s.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	s.clusterState = clusterState

	// Initialize leader election
	leaderIP := s.cfg.NodeIP
	if leaderIP == "" {
		leaderIP = getNodeIP(s.cfg.NATSHost)
	}
	leaderElection, err := NewLeaderElection(s.nc, s.nodeID, leaderIP)
	if err != nil {
		return fmt.Errorf("failed to initialize leader election: %w", err)
	}
	s.leaderElection = leaderElection

	// Initialize node discovery
	s.nodeDiscovery = NewNodeDiscovery(s.cfg.Domain)

	// Initialize scheduler
	s.scheduler = NewScheduler(clusterState, s.cfg)

	// Attach NATS writer to zerolog — all server logs stream to UI
	natsWriter := NewNATSWriter(s.nc, "asty.v1.server.logs")
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	// Initialize metrics store (2h in-memory; hub feeds it, no KV-polling loop).
	s.metricsStore = NewMetricsStore(2 * time.Hour)
	s.logBuffer = NewLogBuffer(1000)
	s.eventBuffer = NewEventBuffer(10000)

	// Initialize autoscaler
	s.autoscaler = NewAutoscaler(clusterState, s.scheduler, s.cfg, s.metricsStore)

	// Initialize proximity matrix
	s.proximityMatrix = NewProximityMatrix()
	if err := s.proximityMatrix.LoadFromConfig(s.cfg.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}

	// Start proximity validation
	go RunProximityValidation(ctx, s.proximityMatrix, clusterState)

	// Initialize deployer
	s.deployer = NewDeployer(clusterState, s.nc, s.cfg)

	// Initialize service loader
	s.serviceLoader = NewServiceLoader(s.cfg.ServiceDir)

	// Load service definitions
	services, err := s.serviceLoader.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load service definitions")
	}
	s.services = services

	// Initialize drain manager
	s.drainManager = NewDrainManager(s)

	// Subscribe to Gateway RPS metrics
	s.subscribeGatewayMetrics()

	// Buffer logs from NATS into logBuffer for history endpoint.
	s.startLogBuffering()

	// Single shared snapshot source for all SSE handlers — refreshes every 5s.
	s.streamHub = newStreamHub(s, 5*time.Second)
	go s.streamHub.Run(ctx)

	// Initialize API server
	s.api = NewAPI(s, s.cfg.UIAddr)

	// Start API server
	go func() {
		if err := s.api.Start(ctx); err != nil {
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
			node := &NodeInfo{
				ID:              fmt.Sprintf("mock-node-%d", i),
				Datacenter:      s.cfg.Datacenter,
				IP:              fmt.Sprintf("192.168.1.%d", i),
				Status:          "ready",
				LastSeen:        time.Now(),
				CPUTotal:        4000,  // 4 cores * 1000 MHz
				CPUAvailable:    3500,
				MemoryTotal:     8192,  // 8 GB
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
		// Resync at 6× EvalInterval (so EvalInterval=10s → resync=60s).
		// Watchers handle the reactive path; the safety-net resync only
		// catches drift and drives metric-driven autoscale.
		resync = resync * 6
		if resync > 5*time.Minute {
			resync = 5 * time.Minute
		}
	}
	controller := NewServiceController(
		s.clusterState,
		s.scheduler,
		s.autoscaler,
		s.services,
		serverDispatcher{s},
		workers,
		resync,
	)
	controller.OnEvent = s.addClusterEvent
	go controller.Run(leaderCtx)
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
func (s *Server) addClusterEvent(e ClusterEvent) {
	s.eventBuffer.Add(e)
	if s.streamHub != nil {
		s.streamHub.FanoutEvent(e)
	}
}

