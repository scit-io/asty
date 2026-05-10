package asty

import (
	"context"
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

	if err := a.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer a.nc.Close()

	clusterState, err := NewClusterState(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := NewNATSWriter(a.nc, agentSubject)
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	go a.healthChecker.Start(ctx)

	go a.metricsCollector.Start(ctx)

	if err := a.subscribeCommands(); err != nil {
		return fmt.Errorf("failed to subscribe to commands: %w", err)
	}

	go a.publishHeartbeat(ctx)

	go a.publishProcessMetrics(ctx)

	go a.monitorProcesses(ctx)

	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent ready")

	<-ctx.Done()

	a.stopAllProcesses()

	return nil
}

// StartService starts a service process
func (a *Agent) StartService(svc *ServiceDefinition) error {
	a.mu.Lock()
	defer a.mu.Unlock()

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

	serviceDir := filepath.Join(a.workDir, svc.Name)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	if svc.Artifact.URL != "" {
		if err := a.artifactDownloader.Download(svc.Artifact.URL, svc.Artifact.Checksum, serviceDir); err != nil {
			return fmt.Errorf("failed to download artifact: %w", err)
		}
	}

	proc := NewProcess(svc, a.nodeID, serviceDir)

	if err := proc.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	a.processes[svc.Name] = proc

	go a.streamProcessLogs(svc.Name, proc)

	if svc.Health.Type == "http" && svc.Health.Addr != "" {
		if err := a.healthChecker.Register(svc.Name, svc.Health.Addr, svc.Health.Path,
			svc.Health.GetInterval(), svc.Health.GetTimeout()); err != nil {
			log.Warn().Err(err).Str("service", svc.Name).Msg("health check registration failed")
		}
	}

	a.metricsCollector.Register(proc.PID(), svc.Name)

	log.Info().
		Str("service", svc.Name).
		Int("pid", proc.PID()).
		Msg("service started")

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

// StopService stops a service process.
func (a *Agent) StopService(serviceName string) error {
	a.mu.Lock()
	proc, exists := a.processes[serviceName]
	if !exists {
		a.mu.Unlock()
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
			cpuUsed += int(m.CPUPercent * 40)
			memUsed += m.MemoryMB
		}
	}

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
func getNodeIP(natsHost string) string {
	natsIP := net.ParseIP(natsHost)
	if natsIP != nil && natsIP.IsLoopback() {
		return natsHost
	}

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

	if current != "" {
		lines = append(lines, current)
	}

	if lastN > 0 && len(lines) > lastN {
		return lines[len(lines)-lastN:]
	}

	return lines
}
