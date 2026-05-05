package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Server handles scheduling, autoscaling, and orchestration
type Server struct {
	cfg    *Config
	nc     *nats.Conn
	nodeID string

	// Cluster state
	clusterState *ClusterState

	// Leader election
	leaderElection *LeaderElection

	// Node discovery
	nodeDiscovery *NodeDiscovery

	// Scheduler
	scheduler *Scheduler

	// Autoscaler
	autoscaler *Autoscaler

	// Proximity matrix
	proximityMatrix *ProximityMatrix

	// Deployer
	deployer *Deployer

	// Service loader
	serviceLoader *ServiceLoader

	// Loaded services
	services []*ServiceDefinition

	// API server
	api *API

	// Metrics storage
	metricsStore *MetricsStore

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
	leaderElection, err := NewLeaderElection(s.nc, s.nodeID)
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

	// Initialize metrics store (24h retention)
	s.metricsStore = NewMetricsStore(24 * time.Hour)

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

	// Start metrics collection (every 10s)
	s.metricsStore.StartCollection(clusterState, s.services, 10*time.Second)

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
	leader, err := s.leaderElection.WaitForLeader(ctx)
	if err != nil {
		return fmt.Errorf("leader election failed: %w", err)
	}

	log.Info().
		Str("leader", leader).
		Bool("is_leader", leader == s.nodeID).
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

	// If we're the leader, start scheduling and autoscaling
	if s.leaderElection.IsLeader() {
		log.Info().Msg("starting scheduler and autoscaler (leader mode)")
		go s.runScheduler(ctx)
		go s.runAutoscaler(ctx)
	}

	// Watch for leadership changes
	go s.watchLeadership(ctx)

	log.Info().Msg("server ready")

	<-ctx.Done()
	return nil
}

// runAutoscaler runs the autoscaler loop (only on leader)
func (s *Server) runAutoscaler(ctx context.Context) {
	log.Info().Msg("autoscaler running")

	s.autoscaler.Run(ctx, s.services)
}

// runScheduler runs the scheduling loop (only on leader)
func (s *Server) runScheduler(ctx context.Context) {
	log.Info().Msg("scheduler running")

	// Watch for allocation changes and sync to agents
	go s.watchAllocations(ctx)

	// Periodic reconciliation
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Reconcile all services
			for _, svc := range s.services {
				if err := s.scheduler.ReconcileService(ctx, svc); err != nil {
					log.Error().Err(err).Str("service", svc.Name).Msg("failed to reconcile service")
				}
			}
		}
	}
}

// watchAllocations watches for allocation changes and sends commands to agents
func (s *Server) watchAllocations(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get all services and their allocations
			for _, svc := range s.services {
				allocs, err := s.clusterState.ListAllocations(svc.Name)
				if err != nil {
					log.Error().Err(err).Str("service", svc.Name).Msg("failed to list allocations")
					continue
				}

				// Send start commands for pending allocations
				for _, alloc := range allocs {
					if alloc.Status == "pending" {
						log.Info().
							Str("service", svc.Name).
							Str("node_id", alloc.NodeID).
							Msg("sending start command to agent")

						if err := s.sendStartCommand(alloc.NodeID, svc); err != nil {
							log.Error().
								Err(err).
								Str("service", svc.Name).
								Str("node_id", alloc.NodeID).
								Msg("failed to send start command")
						} else {
							alloc.Status = "running"
							alloc.UpdatedAt = time.Now()
							if err := s.clusterState.UpdateAllocation(alloc); err != nil {
								log.Error().Err(err).Str("service", svc.Name).Msg("failed to update allocation status")
							}
							log.Info().
								Str("service", svc.Name).
								Str("node_id", alloc.NodeID).
								Msg("service started on node")
						}
					}
				}

				// Check for failed allocations that exceeded restart limit
				// These need to be rescheduled to different nodes
				for _, alloc := range allocs {
					if alloc.Status == "failed" && alloc.ConsecutiveFailures >= 3 {
						log.Warn().
							Str("service", svc.Name).
							Str("node_id", alloc.NodeID).
							Int("restarts", alloc.Restarts).
							Msg("allocation failed permanently, will reschedule")

						// Remove failed allocation
						if err := s.clusterState.DeleteAllocation(svc.Name, alloc.NodeID); err != nil {
							log.Error().Err(err).Msg("failed to delete failed allocation")
						}

						// Trigger reconciliation to create new allocation on another node
						go func(svc *ServiceDefinition) {
							if err := s.scheduler.ReconcileService(ctx, svc); err != nil {
								log.Error().Err(err).Str("service", svc.Name).Msg("failed to reschedule after failure")
							}
						}(svc)
					}
				}
			}
		}
	}
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

// StopServiceOnNode stops a service on a specific node
func (s *Server) StopServiceOnNode(nodeID, serviceName string) error {
	cmd, err := MarshalStopCommand(serviceName)
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
		Str("service", serviceName).
		Str("node_id", nodeID).
		Msg("service stopped on node")

	return nil
}

// watchLeadership watches for leadership changes
func (s *Server) watchLeadership(ctx context.Context) {
	wasLeader := s.leaderElection.IsLeader()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			isLeader := s.leaderElection.IsLeader()

			// Became leader
			if isLeader && !wasLeader {
				log.Info().Msg("became leader, starting scheduler and autoscaler")
				go s.runScheduler(ctx)
				go s.runAutoscaler(ctx)
			}

			// Lost leadership
			if !isLeader && wasLeader {
				log.Info().Msg("lost leadership, stopping scheduler and autoscaler")
				// Scheduler and autoscaler will stop when ctx is cancelled
			}

			wasLeader = isLeader
		}
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
