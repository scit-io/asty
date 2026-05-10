package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/netutil"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment/artifacts"
	"asty/internal/platform/asty/features/execution/health"
	"asty/internal/platform/asty/features/execution/process"
	"asty/internal/platform/asty/features/observability/logs"
	"asty/internal/platform/asty/features/observability/metrics"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Agent manages processes on a single node
type Agent struct {
	cfg    *config.Config
	nc     *nats.Conn
	nodeID string

	processes map[string]*process.Process
	mu        sync.RWMutex

	healthChecker      *health.Checker
	metricsCollector   *metrics.Collector
	artifactDownloader *artifacts.Downloader
	clusterState       *state.ClusterState

	workDir string
}

// New creates a new Asty agent
func New(cfg *config.Config) (*Agent, error) {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = netutil.Hostname()
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
		processes:          make(map[string]*process.Process),
		healthChecker:      health.NewChecker(),
		metricsCollector:   metrics.NewCollector(cfg.EvalInterval),
		artifactDownloader: artifacts.NewDownloader(),
		workDir:            workDir,
	}, nil
}

// Start starts the agent
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: a.cfg.NATSHost, Port: a.cfg.NATSPort,
		User: a.cfg.NATSUser, Password: a.cfg.NATSPassword,
	}, "asty-agent-"+a.nodeID)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	a.nc = nc
	defer a.nc.Close()

	clusterState, err := state.New(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := logs.NewNATSWriter(a.nc, agentSubject)
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
func (a *Agent) StartService(svc *types.ServiceDefinition) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if proc, exists := a.processes[svc.Name]; exists {
		_ = a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
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

	proc := process.New(svc, a.nodeID, serviceDir)

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
	if err := a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
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
		_ = a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
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

	if err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
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

func (a *Agent) getNodeInfo() *types.NodeInfo {
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
		nodeIP = netutil.LocalIPv4(a.cfg.NATSHost)
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

	return &types.NodeInfo{
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

// tailLines splits data into lines (dropping empty ones) and returns at
// most lastN trailing lines. Used by the GetLogs RPC to bound the response
// size when the caller asks for "the last N lines" of a log file.
func tailLines(data string, lastN int) []string {
	if data == "" {
		return []string{}
	}
	parts := strings.Split(data, "\n")
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			lines = append(lines, p)
		}
	}
	if lastN > 0 && len(lines) > lastN {
		return lines[len(lines)-lastN:]
	}
	return lines
}
