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
	"syscall"
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
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	workDir := filepath.Join(cfg.WorkDir, nodeID)
	if workDir == nodeID {
		workDir = filepath.Join("/var/lib/asty", nodeID)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
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

	// Publish process metrics to cluster state
	go a.publishProcessMetrics(ctx)

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

	// Already running — sync the allocation state and return. Mutation is
	// CAS-guarded so we don't clobber metric updates that may have raced in.
	if proc, exists := a.processes[svc.Name]; exists {
		_ = a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *ServiceAllocation) bool {
			if alloc.PID == proc.PID() && alloc.Status == "running" {
				return false
			}
			alloc.PID = proc.PID()
			alloc.Status = "running"
			alloc.StartedAt = time.Now()
			alloc.ConsecutiveFailures = 0
			return true
		})
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

	// Register health check if configured and address is known.
	if svc.Health.Type == "http" && svc.Health.Addr != "" {
		if err := a.healthChecker.Register(svc.Name, svc.Health.Addr, svc.Health.Path,
			svc.Health.GetInterval(), svc.Health.GetTimeout()); err != nil {
			log.Warn().Err(err).Str("service", svc.Name).Msg("health check registration failed")
		}
	}

	// Register metrics collection
	a.metricsCollector.Register(proc.PID(), svc.Name)

	log.Info().
		Str("service", svc.Name).
		Int("pid", proc.PID()).
		Msg("service started")

	// Mark the allocation running. CAS-guarded — no risk of the leader's
	// dispatch write (status=starting) landing after this and resurrecting
	// the stale view, even if their requests cross.
	pid := proc.PID()
	if err := a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *ServiceAllocation) bool {
		alloc.PID = pid
		alloc.Status = "running"
		alloc.StartedAt = time.Now()
		alloc.ConsecutiveFailures = 0
		return true
	}); err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("failed to update allocation with PID")
	} else {
		log.Info().Str("service", svc.Name).Int("pid", pid).Msg("updated allocation with PID")
	}

	return nil
}

// StopService stops a service process. The expensive part — proc.Stop() with
// kill_timeout — runs OUTSIDE a.mu so concurrent commands and metric
// collection aren't serialized behind a slow shutdown. On completion the
// allocation is marked stopped in KV so the drain manager can confirm.
func (a *Agent) StopService(serviceName string) error {
	a.mu.Lock()
	proc, exists := a.processes[serviceName]
	if !exists {
		a.mu.Unlock()
		// Reconcile KV in case the alloc still says running — drain waits on this.
		_ = a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
			if alloc.Status == "stopped" {
				return false
			}
			alloc.Status = "stopped"
			alloc.PID = 0
			return true
		})
		return fmt.Errorf("service %s not running", serviceName)
	}
	delete(a.processes, serviceName)
	a.mu.Unlock()

	a.healthChecker.Unregister(serviceName)
	a.metricsCollector.Unregister(proc.PID())

	if err := proc.Stop(); err != nil {
		log.Error().Err(err).Str("service", serviceName).Msg("process stop failed")
	}

	if err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
		alloc.Status = "stopped"
		alloc.PID = 0
		return true
	}); err != nil {
		log.Warn().Err(err).Str("service", serviceName).Msg("failed to mark allocation stopped")
	}

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

// handleStopCommand acknowledges the stop command immediately and runs the
// real shutdown asynchronously. The NATS request timeout would otherwise need
// to exceed kill_timeout — making the two equal creates a race where the
// caller times out while the agent is still doing graceful shutdown. Drain
// confirmation now happens via KV state (alloc.Status="stopped").
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

	msg.Respond(MarshalResponse(true, fmt.Sprintf("service %s stop initiated", stopCmd.ServiceName), nil))

	go func() {
		if err := a.StopService(stopCmd.ServiceName); err != nil {
			log.Warn().Err(err).Str("service", stopCmd.ServiceName).Msg("background stop reported")
		}
	}()
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
	var cpuUsed int
	var memUsed int64
	for name, proc := range a.processes {
		processes = append(processes, name)
		if m, ok := a.metricsCollector.GetMetrics(proc.PID()); ok {
			cpuUsed += int(m.CPUPercent * 40) // scale: 100% of 1 core = 4000/100 = 40 MHz per %
			memUsed += m.MemoryMB
		}
	}

	// Use explicit IP from config, or auto-detect
	nodeIP := a.cfg.NodeIP
	if nodeIP == "" {
		nodeIP = getNodeIP(a.cfg.NATSHost)
	}

	cpuTotal := detectCPUMHz()
	memTotal := detectMemoryMB()

	cpuAvail := cpuTotal - cpuUsed
	if cpuAvail < 0 {
		cpuAvail = 0
	}
	memAvail := memTotal - memUsed
	if memAvail < 0 {
		memAvail = 0
	}

	// Preserve drain status set by the server — heartbeat must not overwrite
	// "draining" or "drained" back to "ready", otherwise the scheduler would
	// re-place allocations on a node that is being vacated.
	status := "ready"
	if existing, err := a.clusterState.GetNode(a.nodeID); err == nil {
		switch existing.Status {
		case "draining", "drained":
			status = existing.Status
		}
	}

	return &NodeInfo{
		ID:              a.nodeID,
		Datacenter:      a.cfg.Datacenter,
		IP:              nodeIP,
		Status:          status,
		LastSeen:        time.Now(),
		CPUTotal:        cpuTotal,
		CPUAvailable:    cpuAvail,
		MemoryTotal:     memTotal,
		MemoryAvailable: memAvail,
		Processes:       processes,
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

// publishProcessMetrics periodically updates allocation metrics from collector
func (a *Agent) publishProcessMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.RLock()
			procs := make(map[string]*Process, len(a.processes))
			for name, proc := range a.processes {
				procs[name] = proc
			}
			a.mu.RUnlock()

			for serviceName, proc := range procs {
				metrics, ok := a.metricsCollector.GetMetrics(proc.PID())
				if !ok {
					continue
				}
				cpu := int(metrics.CPUPercent)
				mem := int(metrics.MemoryMB)
				healthStatus := a.healthChecker.HealthStatusStr(serviceName)
				err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
					alloc.CPUUsage = cpu
					alloc.MemoryUsage = mem
					if healthStatus != "" {
						alloc.HealthStatus = healthStatus
					}
					return true
				})
				if err != nil {
					log.Error().Err(err).Str("service", serviceName).Msg("failed to update allocation metrics")
				}
			}
		}
	}
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

		svc := proc.ServiceDefinition()
		maxAttempts := svc.Restart.GetAttempts()

		// Decide and bump counters atomically. Returns the post-mutation
		// snapshot so we can branch on it without re-reading.
		var (
			giveUp        bool
			restarts      int
			consecutive   int
		)
		err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
			if alloc.ConsecutiveFailures >= maxAttempts {
				alloc.Status = "failed"
				giveUp = true
			} else {
				alloc.Restarts++
				alloc.ConsecutiveFailures++
				restarts = alloc.Restarts
				consecutive = alloc.ConsecutiveFailures
			}
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to mutate allocation on failure")
			continue
		}

		if giveUp {
			log.Error().
				Str("service", serviceName).
				Int("max_attempts", maxAttempts).
				Msg("restart attempts exhausted, giving up")
			a.mu.Lock()
			delete(a.processes, serviceName)
			a.mu.Unlock()
			continue
		}

		// Kill the failed process tree to release ports.
		if pid := proc.PID(); pid > 0 {
			syscall.Kill(-pid, syscall.SIGKILL)
			syscall.Kill(pid, syscall.SIGKILL)
		}
		a.mu.Lock()
		delete(a.processes, serviceName)
		a.mu.Unlock()
		a.healthChecker.Unregister(serviceName)
		a.metricsCollector.Unregister(proc.PID())

		log.Warn().
			Str("service", serviceName).
			Int("restarts", restarts).
			Int("consecutive_failures", consecutive).
			Int("old_pid", proc.PID()).
			Msg("restarting failed service")

		time.Sleep(svc.Restart.GetDelay())

		// Flip to pending so the leader's next reconcile cycle dispatches a
		// fresh start command. CAS-guarded — leader's metrics writes during
		// the sleep don't block this transition.
		err = a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
			alloc.Status = "pending"
			alloc.PID = 0
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to mark allocation pending")
			continue
		}

		log.Info().
			Str("service", serviceName).
			Int("attempt", restarts).
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
