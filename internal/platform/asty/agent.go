package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

	// Agent logger for streaming to UI
}

// NewAgent creates a new Asty agent
func NewAgent(cfg *Config) (*Agent, error) {
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "/var/lib/asty"
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	return &Agent{
		cfg:                cfg,
		nodeID:             nodeID,
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

	// Attach NATS writer to zerolog — all agent logs stream to UI
	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := NewNATSWriter(a.nc, agentSubject)
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

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

	// Monitor and restart failed processes
	go a.monitorProcesses(ctx)

	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent ready")

	<-ctx.Done()

	// Graceful shutdown: stop all processes
	a.stopAllProcesses()

	return nil
}

// StartService starts a service process
func (a *Agent) StartService(svc *ServiceDefinition) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Already running — not an error, desired state achieved
	if _, exists := a.processes[svc.Name]; exists {
		return nil
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

	// Start log streaming to NATS
	go a.streamProcessLogs(svc.Name, proc)

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

	// Update allocation in cluster state with PID
	alloc, err := a.clusterState.GetAllocation(svc.Name, a.nodeID)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("failed to get allocation for PID update")
	} else {
		alloc.PID = proc.PID()
		alloc.Status = "running"
		alloc.StartedAt = time.Now()
		// Reset restart counter on successful start
		alloc.Restarts = 0
		if err := a.clusterState.UpdateAllocation(alloc); err != nil {
			log.Error().Err(err).Str("service", svc.Name).Msg("failed to update allocation with PID")
		} else {
			log.Info().Str("service", svc.Name).Int("pid", proc.PID()).Msg("updated allocation with PID")
		}
	}

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
	case "logs":
		a.handleLogsCommand(msg, cmd.Data)
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

// handleLogsCommand handles get logs command
func (a *Agent) handleLogsCommand(msg *nats.Msg, data []byte) {
	logsCmd, err := ParseGetLogsCommand(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to parse logs command")
		msg.Respond(MarshalLogsResponse(nil, err))
		return
	}

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("lines", logsCmd.Lines).
		Bool("follow", logsCmd.Follow).
		Msg("retrieving logs")

	a.mu.RLock()
	proc, exists := a.processes[logsCmd.ServiceName]
	a.mu.RUnlock()

	if !exists {
		err := fmt.Errorf("service %s not running", logsCmd.ServiceName)
		log.Warn().Err(err).Str("service", logsCmd.ServiceName).Msg("service not found")
		msg.Respond(MarshalLogsResponse(nil, err))
		return
	}

	// Get logs from process
	logData, err := proc.GetLogs(logsCmd.Lines)
	if err != nil {
		log.Error().Err(err).Str("service", logsCmd.ServiceName).Msg("failed to get logs")
		msg.Respond(MarshalLogsResponse(nil, err))
		return
	}

	// Split into lines
	logs := splitLines(string(logData), logsCmd.Lines)

	log.Debug().
		Str("service", logsCmd.ServiceName).
		Int("line_count", len(logs)).
		Msg("logs retrieved")

	msg.Respond(MarshalLogsResponse(logs, nil))
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

	// Use explicit IP from config, or auto-detect
	nodeIP := a.cfg.NodeIP
	if nodeIP == "" {
		nodeIP = getNodeIP(a.cfg.NATSHost)
	}

	// TODO: collect actual resource usage
	return &NodeInfo{
		ID:         a.nodeID,
		Datacenter: a.cfg.Datacenter,
		IP:         nodeIP,
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

// getNodeIP returns the primary IP address of the node
// If natsHost is a loopback address (127.0.0.x), returns the same to match local development setup
func getNodeIP(natsHost string) string {
	// Parse NATS host to check if it's loopback
	natsIP := net.ParseIP(natsHost)
	if natsIP != nil && natsIP.IsLoopback() {
		// For loopback NATS connections, use the NATS host IP
		// This supports local dev with multiple 127.0.0.x aliases
		return natsHost
	}

	// Otherwise, find first non-loopback IPv4 address
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get network interfaces")
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	log.Warn().Msg("no non-loopback IP address found")
	return ""
}

// splitLines splits log data into lines and optionally returns last N lines
func splitLines(data string, lastN int) []string {
	if data == "" {
		return []string{}
	}

	lines := []string{}
	current := ""

	for _, ch := range data {
		if ch == '\n' {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}

	// Add last line if not empty
	if current != "" {
		lines = append(lines, current)
	}

	// Return last N lines if requested
	if lastN > 0 && len(lines) > lastN {
		return lines[len(lines)-lastN:]
	}

	return lines
}

// monitorProcesses periodically checks for failed processes and restarts them
func (a *Agent) monitorProcesses(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAndRestartFailedProcesses()
		}
	}
}

// checkAndRestartFailedProcesses checks all processes and restarts failed ones
func (a *Agent) checkAndRestartFailedProcesses() {
	a.mu.Lock()
	failedProcesses := make(map[string]*Process)
	for name, proc := range a.processes {
		if proc.Status() == ProcessStatusFailed {
			failedProcesses[name] = proc
		}
	}
	a.mu.Unlock()

	for serviceName, proc := range failedProcesses {
		log.Warn().
			Str("service", serviceName).
			Int("pid", proc.PID()).
			Msg("detected failed process, attempting restart")

		// Get service definition for restart policy
		svc := proc.ServiceDefinition()

		// Get allocation from cluster state to check restart policy
		alloc, err := a.clusterState.GetAllocation(serviceName, a.nodeID)
		if err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to get allocation for restart")
			continue
		}

		// Check restart attempts limit from service definition
		maxAttempts := svc.Restart.GetAttempts()
		if alloc.Restarts >= maxAttempts {
			log.Error().
				Str("service", serviceName).
				Int("restarts", alloc.Restarts).
				Int("max_attempts", maxAttempts).
				Msg("restart attempts exhausted, giving up")

			// Update allocation status to failed
			alloc.Status = "failed"
			if err := a.clusterState.UpdateAllocation(alloc); err != nil {
				log.Error().Err(err).Str("service", serviceName).Msg("failed to update allocation status")
			}

			// Remove from processes map
			a.mu.Lock()
			delete(a.processes, serviceName)
			a.mu.Unlock()

			continue
		}

		// Increment restart counter
		alloc.Restarts++
		if err := a.clusterState.UpdateAllocation(alloc); err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to update restart counter")
		}

		// Stop the failed process
		a.mu.Lock()
		delete(a.processes, serviceName)
		a.mu.Unlock()

		// Unregister health check and metrics
		a.healthChecker.Unregister(serviceName)
		a.metricsCollector.Unregister(proc.PID())

		log.Warn().
			Str("service", serviceName).
			Int("restarts", alloc.Restarts).
			Int("old_pid", proc.PID()).
			Msg("restarting failed service")

		// Wait before restart using configured delay
		restartDelay := svc.Restart.GetDelay()
		time.Sleep(restartDelay)

		// Mark allocation as pending so server re-schedules it
		alloc.Status = "pending"
		alloc.PID = 0
		if err := a.clusterState.UpdateAllocation(alloc); err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to mark allocation as pending")
			continue
		}

		log.Info().
			Str("service", serviceName).
			Int("attempt", alloc.Restarts).
			Msg("marked allocation for restart, waiting for server command")
	}
}

// streamProcessLogs streams process logs to NATS in real-time
func (a *Agent) streamProcessLogs(serviceName string, proc *Process) {
	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", a.nodeID, serviceName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logLines := make(chan string, 100)

	// Start tailing logs
	go func() {
		if err := proc.TailLogs(ctx, logLines); err != nil && err != context.Canceled {
			log.Error().
				Err(err).
				Str("service", serviceName).
				Msg("failed to tail logs")
		}
		close(logLines)
	}()

	log.Info().
		Str("service", serviceName).
		Str("subject", subject).
		Msg("streaming logs to NATS")

	// Publish log lines to NATS
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-logLines:
			if !ok {
				// Channel closed, process stopped
				log.Info().
					Str("service", serviceName).
					Msg("log channel closed, ending stream")
				return
			}

			logEntry, err := json.Marshal(map[string]interface{}{
				"line":      line,
				"timestamp": time.Now().Unix(),
			})
			if err != nil {
				continue
			}

			if err := a.nc.Publish(subject, logEntry); err != nil {
				log.Error().
					Err(err).
					Str("service", serviceName).
					Str("subject", subject).
					Msg("failed to publish log line")
			}

		case <-ticker.C:
			// Periodic check if process still exists
			a.mu.RLock()
			_, exists := a.processes[serviceName]
			a.mu.RUnlock()

			if !exists {
				log.Info().
					Str("service", serviceName).
					Msg("process no longer exists, ending log stream")
				cancel()
				return
			}

		case <-ctx.Done():
			return
		}
	}
}
