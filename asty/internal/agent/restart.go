package agent

import (
	"context"
	"syscall"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/execution/process"

	"github.com/rs/zerolog/log"
)

// monitorProcesses drains the per-process OnExit channel and reacts on
// each failure. Pure event-driven: no timers, no polling. The Process
// monitor goroutine pushes the service name when a process exits with
// StatusFailed; we decide whether to restart or give up.
func (a *Agent) monitorProcesses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case name := <-a.failed:
			if proc := a.lookupFailed(name); proc != nil {
				a.attemptRestart(ctx, name, proc)
			}
		}
	}
}

// lookupFailed re-fetches the Process by name and confirms it is still
// in StatusFailed. The OnExit callback fires for every exit, including
// clean Stops; we restart only when the agent has not removed the
// process and the exit is genuinely a failure.
func (a *Agent) lookupFailed(name string) *process.Process {
	a.mu.RLock()
	defer a.mu.RUnlock()
	proc, ok := a.processes[name]
	if !ok || proc.Status() != process.StatusFailed {
		return nil
	}
	return proc
}

func (a *Agent) attemptRestart(ctx context.Context, name string, proc *process.Process) {
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
			alloc.Status = types.AllocFailed
			giveUp = true
		} else {
			// Distinguish "scheduled restart within budget" from
			// "running normally": observers (UI, metrics) see Restarting
			// during the delay window and the moment the agent re-spawns
			// the process. The status flips to Pending below so the
			// controller dispatches a fresh start RPC.
			alloc.Status = types.AllocRestarting
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

	select {
	case <-ctx.Done():
		return
	case <-time.After(svc.Restart.GetDelay()):
	}

	err = a.clusterState.MutateAllocation(name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
		alloc.Status = types.AllocPending
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
