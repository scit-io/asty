package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/execution/process"

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
			if alloc.PID == proc.PID() && alloc.Status == types.AllocRunning {
				return false
			}
			alloc.PID = proc.PID()
			alloc.Status = types.AllocRunning
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
	// Register before Start so we never miss an exit. The callback
	// runs on the process monitor goroutine; it must not block, hence
	// the non-blocking send pattern.
	name := svc.Name
	proc.OnExit(func(err error) {
		select {
		case a.failed <- name:
		default:
		}
	})
	if err := proc.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}
	a.processes[svc.Name] = proc

	go a.streamProcessLogs(svc.Name, proc)

	switch svc.Health.Type {
	case "http":
		if svc.Health.Addr != "" {
			a.healthChecker.Register(svc.Name, svc.Health.Addr, svc.Health.Path,
				svc.Health.GetInterval(), svc.Health.GetTimeout())
		}
	case "nats":
		if svc.Health.Subject != "" {
			a.healthChecker.RegisterNATS(svc.Name, svc.Health.Subject,
				svc.Health.GetInterval(), svc.Health.GetTimeout())
		}
	}
	a.metricsCollector.Register(proc.PID(), svc.Name)

	log.Info().Str("service", svc.Name).Int("pid", proc.PID()).Msg("service started")

	pid := proc.PID()
	if err := a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
		alloc.PID = pid
		alloc.Status = types.AllocRunning
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
			if alloc.Status == types.AllocStopped {
				return false
			}
			alloc.Status = types.AllocStopped
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
		alloc.Status = types.AllocStopped
		alloc.PID = 0
		return true
	}); err != nil {
		log.Warn().Err(err).Str("service", serviceName).Msg("failed to mark allocation stopped")
	}

	log.Info().Str("service", serviceName).Msg("service stopped")
	return nil
}

// RestartService stops the currently-running copy (if any) and starts a
// fresh one from svc. Used by the deployer's rolling-update path to apply
// a new version: StartService alone is idempotent (it refreshes the alloc
// PID and returns when the process exists), so the explicit stop here
// bypasses the short-circuit and guarantees the new svc def — including
// any version-substituted Artifact URL — actually takes effect.
func (a *Agent) RestartService(svc *types.ServiceDefinition) error {
	// Best-effort stop: ignore "service not running" so a restart targeted
	// at a node that doesn't currently host the process still results in
	// a fresh start.
	if err := a.StopService(svc.Name); err != nil {
		log.Debug().Err(err).Str("service", svc.Name).Msg("restart: stop returned (may have been already stopped)")
	}
	return a.StartService(svc)
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
