package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/execution/process"

	"github.com/rs/zerolog/log"
)

// StartService is the agent's "make this service running on this node"
// entry point. It is idempotent: if the process is already running, the
// allocation record is just refreshed. Otherwise it downloads the
// artifact (if any), starts the process, registers health and metrics
// probes, and updates the allocation.
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

	log.Info().Str("service", svc.Name).Int("pid", proc.PID()).Msg("service started")

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

// StopService stops a running service. If the process is unknown to the
// agent (e.g. agent restarted between drain and stop), the allocation
// is still marked stopped — the cluster shouldn't keep "running" state
// for a process that does not actually exist on this node.
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

	log.Info().Str("service", serviceName).Msg("service stopped")
	return nil
}

// stopAllProcesses runs StopService on every known process, called on
// agent shutdown to leave cluster state consistent.
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
