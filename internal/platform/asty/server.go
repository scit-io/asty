package asty

import (
	"context"
	"encoding/json"
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
	go s.proximityMatrix.RunValidation(ctx, clusterState)

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
	controller.onEvent = s.addClusterEvent
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

// serverDispatcher is the CommandDispatcher adapter that lets the controller
// reach into Server's NATS request/reply path without taking a *Server
// reference (would create a circular dependency between controller logic
// and Server lifecycle).
type serverDispatcher struct{ s *Server }

func (d serverDispatcher) SendStartCommand(nodeID string, svc *ServiceDefinition) error {
	return d.s.sendStartCommand(nodeID, svc)
}

// sendStartCommand sends a start command to an agent
func (s *Server) sendStartCommand(nodeID string, svc *ServiceDefinition) error {
	cmdBytes, err := MarshalStartCommand(svc)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmdBytes, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("agent returned error: %s", resp.Message)
	}

	return nil
}

// SendCommandToAgent sends a command to an agent
func (s *Server) SendCommandToAgent(nodeID string, command []byte, timeout time.Duration) (*CommandResponse, error) {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", nodeID)

	msg, err := s.nc.Request(subject, command, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp CommandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// StartServiceOnNode starts a service on a specific node
func (s *Server) StartServiceOnNode(nodeID string, svc *ServiceDefinition) error {
	cmd, err := MarshalStartCommand(svc)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmd, 30*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("command failed: %s", resp.Error)
	}

	log.Info().
		Str("service", svc.Name).
		Str("node_id", nodeID).
		Msg("service started on node")

	return nil
}

// StopServiceOnNode dispatches a stop command to a node's agent. The agent
// acknowledges immediately and performs the actual shutdown asynchronously, so
// this call is short — only enough for NATS round-trip to the ack. Confirmation
// that the process has stopped comes from KV state (alloc.Status="stopped"),
// which the drain manager polls.
func (s *Server) StopServiceOnNode(nodeID, serviceName string) error {
	cmd, err := MarshalStopCommand(serviceName)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmd, 5*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("command failed: %s", resp.Error)
	}

	log.Info().
		Str("service", serviceName).
		Str("node_id", nodeID).
		Msg("stop command dispatched")

	return nil
}

// watchLeadership re-arms the leader loop on flips. The sub-context returned
// by startLeaderWork is cancelled on loss, so a re-elected leader gets a
// clean run instead of the previous behavior where stale goroutines kept
// running with the parent context still alive.
func (s *Server) watchLeadership(ctx context.Context) {
	err := s.leaderElection.WatchLeadership(ctx,
		func() {
			log.Info().Msg("became leader")
			s.startLeaderWork(ctx)
		},
		func() {
			log.Info().Msg("lost leadership")
			s.stopLeaderWork()
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("leadership watcher failed")
	}
}

// watchClusterNodes watches for cluster node changes via DNS
func (s *Server) watchClusterNodes(ctx context.Context) {
	s.nodeDiscovery.WatchNodes(ctx, func(nodes []string) {
		log.Info().
			Strs("nodes", nodes).
			Int("count", len(nodes)).
			Msg("cluster nodes updated")
	})
}

// DeployService initiates a service deployment
func (s *Server) DeployService(ctx context.Context, serviceName, version string) (*DeploymentStatus, error) {
	// Get service definition
	svc, err := s.serviceLoader.GetService(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to load service definition: %w", err)
	}

	// Get current allocations
	allocs, err := s.clusterState.ListAllocations(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}

	if len(allocs) == 0 {
		return nil, fmt.Errorf("no allocations found for service %s", serviceName)
	}

	// Get current version (from first allocation)
	currentVersion := "unknown"
	if len(allocs) > 0 {
		currentVersion = allocs[0].Version
	}

	// Create deployment plan
	plan := &DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: currentVersion,
		TargetVersion:  version,
		Allocations:    allocs,
		UpdateStrategy: UpdateStrategy{
			MaxParallel:      svc.Update.MaxParallel,
			MinHealthyTime:   parseUpdateDuration(svc.Update.MinHealthyTime, 10*time.Second),
			HealthyDeadline:  parseUpdateDuration(svc.Update.HealthyDeadline, 3*time.Minute),
			ProgressDeadline: parseUpdateDuration(svc.Update.ProgressDeadline, 10*time.Minute),
			AutoRevert:       svc.Update.AutoRevert,
			Canary:           1, // TODO: get from svc.Update.Canary
		},
		HealthyDeadline: parseUpdateDuration(svc.Update.HealthyDeadline, 3*time.Minute),
		MinHealthyTime:  parseUpdateDuration(svc.Update.MinHealthyTime, 10*time.Second),
		MaxParallel:     svc.Update.MaxParallel,
		AutoRevert:      svc.Update.AutoRevert,
		Canary:          1,
	}

	// Execute deployment
	return s.deployer.Deploy(ctx, plan)
}

// parseUpdateDuration parses duration string with fallback
func parseUpdateDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// startLogBuffering subscribes to NATS log subjects and feeds lines into
// logBuffer so that history endpoints can serve recent logs without a live SSE
// connection. One subscription per source type using NATS wildcards.
func (s *Server) startLogBuffering() {
	parseAndAppend := func(source string, data []byte) {
		var entry map[string]interface{}
		if err := json.Unmarshal(data, &entry); err != nil {
			return
		}

		level, _ := entry["level"].(string)
		msg, _ := entry["message"].(string)

		var ts int64
		switch v := entry["timestamp"].(type) {
		case float64:
			ts = int64(v)
		}

		timeStr := ""
		if t, ok := entry["time"].(string); ok {
			timeStr = t
		} else if ts > 0 {
			timeStr = fmt.Sprintf("%d", ts)
		}

		line := fmt.Sprintf("[%s] [%s] %s", timeStr, level, msg)
		s.logBuffer.Append(source, LogLine{Timestamp: ts, Level: level, Line: line})
	}

	// Cluster server logs.
	if _, err := s.nc.Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		parseAndAppend("cluster", msg.Data)
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe cluster log buffer")
	}

	// Agent logs — single wildcard sub handles all nodes and services.
	// Subject pattern: asty.v1.agent.{nodeID}.logs.{service|"agent"}
	if _, err := s.nc.Subscribe("asty.v1.agent.*.logs.*", func(msg *nats.Msg) {
		// Extract nodeID and service from subject tokens.
		// tokens: asty(0) v1(1) agent(2) nodeID(3) logs(4) service(5)
		parts := splitSubject(msg.Subject)
		if len(parts) < 6 {
			return
		}
		nodeID := parts[3]
		svc := parts[5]
		if svc == "agent" {
			parseAndAppend("node."+nodeID, msg.Data)
		} else {
			parseAndAppend("node."+nodeID+".svc."+svc, msg.Data)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe agent log buffer")
	}
}

// subscribeGatewayMetrics ingests per-gateway valid-RPS reports into
// node.<id>.rps. Cluster RPS is aggregated by IngestSnapshot.
func (s *Server) subscribeGatewayMetrics() {
	subject := "asty.v1.metrics.gateway.*"
	_, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var report struct {
			NodeID   string  `json:"node_id"`
			ValidRPS float64 `json:"valid_rps"`
		}
		if err := json.Unmarshal(msg.Data, &report); err != nil {
			return
		}
		s.metricsStore.Add("node."+report.NodeID+".rps", report.ValidRPS)
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to gateway metrics")
	}
}

// connectNATS establishes connection to NATS
func (s *Server) connectNATS() error {
	natsURL := fmt.Sprintf("nats://%s:%s", s.cfg.NATSHost, s.cfg.NATSPort)

	opts := []nats.Option{
		nats.Name("asty-server-" + s.nodeID),
	}

	if s.cfg.NATSUser != "" {
		opts = append(opts, nats.UserInfo(s.cfg.NATSUser, s.cfg.NATSPassword))
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return err
	}

	s.nc = nc
	log.Info().Str("url", natsURL).Msg("connected to NATS")
	return nil
}

// splitSubject splits a NATS subject by '.'.
func splitSubject(subject string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(subject); i++ {
		if subject[i] == '.' {
			parts = append(parts, subject[start:i])
			start = i + 1
		}
	}
	parts = append(parts, subject[start:])
	return parts
}
