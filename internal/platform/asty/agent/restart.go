package agent

import (
	"context"
	"syscall"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/execution/process"

	"github.com/rs/zerolog/log"
)

// restartPollInterval — how often the agent scans its process map for
// exited processes that need restarting. Phase 6.3 will replace this
// with an event-driven OnExit callback from Process.
const restartPollInterval = 5 * time.Second

// monitorProcesses runs the restart loop until ctx is cancelled.
func (a *Agent) monitorProcesses(ctx context.Context) {
	ticker := time.NewTicker(restartPollInterval)
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

// checkAndRestartFailedProcesses finds StatusFailed processes, decides
// whether they've used up their restart budget, and either marks the
// allocation permanently failed or flips it back to "pending" so the
// controller will redispatch it.
func (a *Agent) checkAndRestartFailedProcesses() {
	failed := a.collectFailed()
	for name, proc := range failed {
		a.attemptRestart(name, proc)
	}
}

func (a *Agent) collectFailed() map[string]*process.Process {
	a.mu.Lock()
	defer a.mu.Unlock()
	failed := make(map[string]*process.Process)
	for name, proc := range a.processes {
		if proc.Status() == process.StatusFailed {
			failed[name] = proc
		}
	}
	return failed
}

func (a *Agent) attemptRestart(name string, proc *process.Process) {
	log.Warn().
		Str("service", name).
		Int("pid", proc.PID()).
		Msg("detected failed process, attempting restart")

	svc := proc.ServiceDefinition()
	maxAttempts := svc.Restart.GetAttempts()

	var (
		giveUp      bool
		restarts    int
		consecutive int
	)
	err := a.clusterState.MutateAllocation(name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
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
		log.Error().Err(err).Str("service", name).Msg("failed to mutate allocation on failure")
		return
	}

	if giveUp {
		log.Error().
			Str("service", name).
			Int("max_attempts", maxAttempts).
			Msg("restart attempts exhausted, giving up")
		a.dropProcess(name)
		return
	}

	a.killAndForget(name, proc)

	log.Warn().
		Str("service", name).
		Int("restarts", restarts).
		Int("consecutive_failures", consecutive).
		Int("old_pid", proc.PID()).
		Msg("restarting failed service")

	time.Sleep(svc.Restart.GetDelay())

	err = a.clusterState.MutateAllocation(name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
		alloc.Status = "pending"
		alloc.PID = 0
		return true
	})
	if err != nil {
		log.Error().Err(err).Str("service", name).Msg("failed to mark allocation pending")
		return
	}
	log.Info().
		Str("service", name).
		Int("attempt", restarts).
		Msg("marked allocation for restart, waiting for server command")
}

// dropProcess forgets a failed-and-given-up process so we don't try to
// restart it again. The KV record stays as "failed" until the controller
// prunes it.
func (a *Agent) dropProcess(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.processes, name)
}

// killAndForget makes sure the OS process is dead, deregisters its
// health/metrics, and removes it from the agent's map.
func (a *Agent) killAndForget(name string, proc *process.Process) {
	if pid := proc.PID(); pid > 0 {
		syscall.Kill(-pid, syscall.SIGKILL)
		syscall.Kill(pid, syscall.SIGKILL)
	}
	a.mu.Lock()
	delete(a.processes, name)
	a.mu.Unlock()
	a.healthChecker.Unregister(name)
	a.metricsCollector.Unregister(proc.PID())
}
