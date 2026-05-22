package agent

import (
	"fmt"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// StopService stops a running service. If the process is unknown to the
// agent (e.g. agent restarted between drain and stop), the allocation
// is still marked stopped — the cluster shouldn't keep "running" state
// for a process that does not actually exist on this node.
//
// The status sequence is:
//   Running → Stopping (right after dispatch) → Stopped (on confirmed exit)
//
// AllocStopping is the explicit "graceful exit in flight" window —
// the slot is still occupied so the scheduler does not race another
// copy into it before the agent confirms.
func (a *Agent) StopService(serviceName string) error {
	a.mu.RLock()
	_, exists := a.processes[serviceName]
	a.mu.RUnlock()

	if !exists {
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

	// Mark Stopping so observers see the intent in flight.
	_ = a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
		if alloc.Status == types.AllocStopped || alloc.Status == types.AllocFailed {
			return false
		}
		alloc.Status = types.AllocStopping
		return true
	})

	a.stopProcess(serviceName)

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

// stopProcess kills the local process for serviceName and unregisters its
// health/metrics, without touching KV. The caller owns KV transitions.
// Returns true iff a process existed and a signal was sent. Used by both
// StopService and RestartService so the two paths share kill mechanics
// but keep distinct KV state machines.
func (a *Agent) stopProcess(serviceName string) bool {
	a.mu.Lock()
	proc, exists := a.processes[serviceName]
	if !exists {
		a.mu.Unlock()
		return false
	}
	delete(a.processes, serviceName)
	a.mu.Unlock()

	a.healthChecker.Unregister(serviceName)
	a.metricsCollector.Unregister(proc.PID())
	if err := proc.Stop(); err != nil {
		log.Error().Err(err).Str("service", serviceName).Msg("process stop failed")
	}
	return true
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
