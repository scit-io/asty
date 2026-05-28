package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/process"

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
	if err := os.MkdirAll(serviceDir, 0o700); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	if svc.Artifact.URL != "" {
		if err := a.artifactDownloader.Download(svc.Artifact.URL, svc.Artifact.Checksum, serviceDir); err != nil {
			return fmt.Errorf("failed to download artifact: %w", err)
		}
		// Stamp +x on the entrypoint binary. The artifact downloader
		// writes 0400 (owner-read-only at rest); we add the exec bit
		// here, just-in-time, so the file is runnable for fork+exec but
		// stays as locked-down as the archive allows when not being
		// executed.
		entrypoint := filepath.Join(serviceDir, svc.Name)
		if err := os.Chmod(entrypoint, 0o500); err != nil {
			return fmt.Errorf("chmod +x %s: %w", entrypoint, err)
		}
	}

	// No explicit Credential needed: this code path runs after
	// dropPrivileges, so fork+exec naturally inherits the agent's
	// current uid (RunAsUser when configured, original uid otherwise).
	// nats-server is the one exception — see bootstrapNATS — because
	// it's exec'd BEFORE the drop while the agent is still root.
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

// RestartService stops the local copy of svc in place and starts a
// fresh one. The KV record stays in AllocRestarting throughout, which
// preserves allocation identity (same ID, same node), holds the slot so
// the scheduler will not race another copy onto a different node, and
// signals the in-flight restart to observers. Used by both the deployer
// (with a version-resolved svc def) and the dashboard's restart-allocation
// action (which routes through CmdRestart for the same reasons).
//
// If the alloc is missing in KV, the first mutate returns an error and
// we bail before spawning anything — that's the guard against orphan
// processes on a deleted slot. If StartService fails after the stop, we
// roll the alloc back to Pending so the reconciler retries with backoff
// rather than leaving it stuck in Restarting.
func (a *Agent) RestartService(svc *types.ServiceDefinition) error {
	if err := a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
		alloc.Status = types.AllocRestarting
		// Count the attempt, not the outcome — matches k8s restartCount
		// semantics and the help string on asty_alloc_restarts_total
		// ("Number of times the agent has restarted the allocation").
		// ConsecutiveFailures stays put: this path is operator- or
		// deployer-initiated, not a crash, so it should not feed
		// pruneFailed's budget. A successful StartService below
		// resets ConsecutiveFailures to 0 anyway.
		alloc.Restarts++
		return true
	}); err != nil {
		return fmt.Errorf("mark restarting: %w", err)
	}

	a.stopProcess(svc.Name)

	if err := a.StartService(svc); err != nil {
		_ = a.clusterState.MutateAllocation(svc.Name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
			alloc.Status = types.AllocPending
			alloc.PID = 0
			return true
		})
		return err
	}
	return nil
}
