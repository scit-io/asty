package agent

import (
	"context"
	"path/filepath"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/process"

	"github.com/rs/zerolog/log"
)

// heartbeatInterval — how often the agent re-writes its NodeInfo to KV.
// The cluster considers nodes dead if their last_seen is older than
// nodeHeartbeatStaleAfter (2 min); 5 s gives 24 retries before the node
// is excluded from scheduling.
const heartbeatInterval = 5 * time.Second

// processMetricsInterval — how often per-process CPU/Memory snapshots
// are pushed to the allocation record. Slower than heartbeat because
// metrics are sampled, not state — they only need to be fresh enough
// for the autoscaler's averaging window.
const processMetricsInterval = 10 * time.Second

// publishHeartbeat keeps NodeInfo up-to-date in KV until ctx is cancelled.
func (a *Agent) publishHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info := a.getNodeInfo()
			a.finalizeNodeStatus(info)
			if err := a.clusterState.UpdateNode(info); err != nil {
				log.Error().Err(err).Msg("failed to update node heartbeat")
			} else {
				log.Debug().Str("node_id", a.nodeID).Msg("heartbeat sent")
			}
		}
	}
}

// finalizeNodeStatus resolves info.Status with three signals: the
// value getNodeInfo got back from KV (empty = read failed), the
// in-memory cache of the last observed operator-set status, and
// the capacity probe state (zero capacity → Joining).
//
// Cache flow:
//   - Observed Draining/Drained/Paused → cache it.
//   - Observed Ready/Joining            → clear cache (operator's intent
//                                          was cleared, OR fresh node).
//   - Read failed (empty status)         → restore from cache if any,
//                                          otherwise default by capacity.
//
// Without the cache, a transient KV failure during cluster growth
// would let the next UpdateNode write default Ready over Drained.
func (a *Agent) finalizeNodeStatus(info *types.NodeInfo) {
	switch info.Status {
	case types.NodeDraining, types.NodeDrained, types.NodePaused:
		a.lastOperatorStatus = info.Status
		return
	case types.NodeReady, types.NodeJoining:
		a.lastOperatorStatus = ""
		return
	}
	// Status == "" — KV read failed inside getNodeInfo.
	if a.lastOperatorStatus != "" {
		info.Status = a.lastOperatorStatus
		return
	}
	if info.CPUTotal == 0 || info.MemoryTotal == 0 {
		info.Status = types.NodeJoining
	} else {
		info.Status = types.NodeReady
	}
}

// publishProcessMetrics streams CPU%/Memory MB into each running
// allocation so the leader's autoscaler can react. Health status is
// included when the checker has it.
func (a *Agent) publishProcessMetrics(ctx context.Context) {
	ticker := time.NewTicker(processMetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pushOneMetricsSample()
		}
	}
}

func (a *Agent) pushOneMetricsSample() {
	a.mu.RLock()
	procs := make(map[string]*process.Process, len(a.processes))
	for name, proc := range a.processes {
		procs[name] = proc
	}
	a.mu.RUnlock()

	for name, proc := range procs {
		m, ok := a.metricsCollector.GetMetrics(proc.PID())
		if !ok {
			continue
		}
		cpu := int(m.CPUPercent)
		mem := int(m.MemoryMB)
		disk := dirSizeMB(filepath.Join(a.workDir, name))
		health := a.healthChecker.HealthStatus(name)
		err := a.clusterState.MutateAllocation(name, a.nodeID, func(alloc *types.ServiceAllocation) bool {
			alloc.CPUUsage = cpu
			alloc.MemoryUsage = mem
			alloc.DiskUsage = disk
			if health != types.HealthUnknown {
				alloc.HealthStatus = health
			}
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", name).Msg("failed to update allocation metrics")
		}
	}
}
