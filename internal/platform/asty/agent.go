package asty

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Agent manages processes on a single node
type Agent struct {
	cfg    *Config
	nc     *nats.Conn
	nodeID string

	// Process management
	processes map[string]*Process // key: service name
	mu        sync.RWMutex

	// Health checks
	healthChecker *HealthChecker

	// Metrics collection
	metricsCollector *MetricsCollector

	// Artifact downloader
	artifactDownloader *ArtifactDownloader

	// Working directory for processes
	workDir string

	// Cluster state
	clusterState *ClusterState
}

// NewAgent creates a new Asty agent
func NewAgent(cfg *Config) (*Agent, error) {
	workDir := "/var/lib/asty"
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	return &Agent{
		cfg:                cfg,
		nodeID:             generateNodeID(),
		processes:          make(map[string]*Process),
		healthChecker:      NewHealthChecker(),
		metricsCollector:   NewMetricsCollector(cfg.EvalInterval),
		artifactDownloader: NewArtifactDownloader(),
		workDir:            workDir,
	}, nil
}

// Start starts the agent
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	// Connect to NATS
	if err := a.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer a.nc.Close()

	// Initialize cluster state
	clusterState, err := NewClusterState(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	// Start health checker
	go a.healthChecker.Start(ctx)

	// Start metrics collector
	go a.metricsCollector.Start(ctx)

	// Subscribe to agent commands from server
	if err := a.subscribeCommands(); err != nil {
		return fmt.Errorf("failed to subscribe to commands: %w", err)
	}

	// Publish node heartbeat
	go a.publishHeartbeat(ctx)

	log.Info().Msg("agent ready")

	<-ctx.Done()

	// Graceful shutdown: stop all processes
	a.stopAllProcesses()

	return nil
}

// StartService starts a service process
func (a *Agent) StartService(svc *ServiceDefinition) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if already running
	if _, exists := a.processes[svc.Name]; exists {
		return fmt.Errorf("service %s already running", svc.Name)
	}

	// Create service directory
	serviceDir := filepath.Join(a.workDir, svc.Name)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	// Download artifact if URL is provided
	if svc.Artifact.URL != "" {
		if err := a.artifactDownloader.Download(svc.Artifact.URL, svc.Artifact.Checksum, serviceDir); err != nil {
			return fmt.Errorf("failed to download artifact: %w", err)
		}
	}

	// Create process
	proc := NewProcess(svc, a.nodeID, serviceDir)

	// Start process
	if err := proc.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Register process
	a.processes[svc.Name] = proc

	// Register health check if configured
	if svc.Health.Type == "http" {
		// TODO: get dynamic port from environment variable like ${ASTY_HEALTH_ADDR}
		// For now, skip health check registration
		// a.healthChecker.Register(svc.Name, addr, svc.Health.Path, svc.Health.GetInterval(), svc.Health.GetTimeout())
	}

	// Register metrics collection
	a.metricsCollector.Register(proc.PID(), svc.Name)

	log.Info().
		Str("service", svc.Name).
		Int("pid", proc.PID()).
		Msg("service started")

	return nil
}

// StopService stops a service process
func (a *Agent) StopService(serviceName string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	proc, exists := a.processes[serviceName]
	if !exists {
		return fmt.Errorf("service %s not running", serviceName)
	}

	// Stop process
	if err := proc.Stop(); err != nil {
		return fmt.Errorf("failed to stop process: %w", err)
	}

	// Unregister health check
	a.healthChecker.Unregister(serviceName)

	// Unregister metrics collection
	a.metricsCollector.Unregister(proc.PID())

	// Remove from processes map
	delete(a.processes, serviceName)

	log.Info().
		Str("service", serviceName).
		Msg("service stopped")

	return nil
}

// stopAllProcesses stops all running processes
func (a *Agent) stopAllProcesses() {
	a.mu.RLock()
	services := make([]string, 0, len(a.processes))
	for name := range a.processes {
		services = append(services, name)
	}
	a.mu.RUnlock()

	for _, name := range services {
		if err := a.StopService(name); err != nil {
			log.Error().Err(err).Str("service", name).Msg("failed to stop service")
		}
	}
}

// connectNATS establishes connection to NATS
func (a *Agent) connectNATS() error {
	natsURL := fmt.Sprintf("nats://%s:%s", a.cfg.NATSHost, a.cfg.NATSPort)

	opts := []nats.Option{
		nats.Name("asty-agent-" + a.nodeID),
	}

	if a.cfg.NATSUser != "" {
		opts = append(opts, nats.UserInfo(a.cfg.NATSUser, a.cfg.NATSPassword))
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return err
	}

	a.nc = nc
	log.Info().Str("url", natsURL).Msg("connected to NATS")
	return nil
}

// subscribeCommands subscribes to agent commands from server
func (a *Agent) subscribeCommands() error {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", a.nodeID)

	_, err := a.nc.Subscribe(subject, func(msg *nats.Msg) {
		a.handleCommand(msg)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	log.Info().Str("subject", subject).Msg("subscribed to commands")
	return nil
}

// handleCommand handles incoming commands from server
func (a *Agent) handleCommand(msg *nats.Msg) {
	cmd, err := UnmarshalCommand(msg.Data)
	if err != nil {
		log.Error().Err(err).Msg("failed to unmarshal command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("type", cmd.Type).
		Msg("received command")

	switch cmd.Type {
	case "start":
		a.handleStartCommand(msg, cmd.Data)
	case "stop":
		a.handleStopCommand(msg, cmd.Data)
	default:
		log.Error().Str("type", cmd.Type).Msg("unknown command type")
		msg.Respond(MarshalResponse(false, "", fmt.Errorf("unknown command type: %s", cmd.Type)))
	}
}

// handleStartCommand handles start service command
func (a *Agent) handleStartCommand(msg *nats.Msg, data []byte) {
	startCmd, err := ParseStartCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse start command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", startCmd.Service.Name).
		Msg("starting service")

	if err := a.StartService(startCmd.Service); err != nil {
		log.Error().Err(err).Str("service", startCmd.Service.Name).Msg("failed to start service")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	msg.Respond(MarshalResponse(true, fmt.Sprintf("service %s started", startCmd.Service.Name), nil))
}

// handleStopCommand handles stop service command
func (a *Agent) handleStopCommand(msg *nats.Msg, data []byte) {
	stopCmd, err := ParseStopCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse stop command")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	log.Info().
		Str("service", stopCmd.ServiceName).
		Msg("stopping service")

	if err := a.StopService(stopCmd.ServiceName); err != nil {
		log.Error().Err(err).Str("service", stopCmd.ServiceName).Msg("failed to stop service")
		msg.Respond(MarshalResponse(false, "", err))
		return
	}

	msg.Respond(MarshalResponse(true, fmt.Sprintf("service %s stopped", stopCmd.ServiceName), nil))
}

// publishHeartbeat publishes periodic heartbeat to cluster state
func (a *Agent) publishHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Collect current node info
			nodeInfo := a.getNodeInfo()

			// Update cluster state
			if err := a.clusterState.UpdateNode(nodeInfo); err != nil {
				log.Error().Err(err).Msg("failed to update node heartbeat")
			} else {
				log.Debug().Str("node_id", a.nodeID).Msg("heartbeat sent")
			}
		}
	}
}

// getNodeInfo collects current node information
func (a *Agent) getNodeInfo() *NodeInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	processes := make([]string, 0, len(a.processes))
	for name := range a.processes {
		processes = append(processes, name)
	}

	// TODO: collect actual resource usage
	return &NodeInfo{
		ID:         a.nodeID,
		Datacenter: a.cfg.Datacenter,
		IP:         "", // TODO: get node IP
		Status:     "ready",
		LastSeen:   time.Now(),
		CPUTotal:      4000, // TODO: detect actual CPU
		CPUAvailable:  3000,
		MemoryTotal:   8192, // TODO: detect actual memory
		MemoryAvailable: 6144,
		Processes:    processes,
	}
}

// generateNodeID generates a stable node ID based on hostname
func generateNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
