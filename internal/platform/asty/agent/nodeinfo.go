package agent

import (
	"time"

	"asty/internal/platform/asty/core/netutil"
	"asty/internal/platform/asty/core/types"
)

// cpuPctToMHzFactor converts a CPU% sample (0..100 per core) into a MHz
// estimate: assume 40 MHz of headroom per 1% reported by the OS. The
// number is calibrated to keep CPUAvailable in roughly the right
// ballpark on commodity x86 hardware; precise accounting would require
// per-platform knowledge that isn't worth the complexity here.
const cpuPctToMHzFactor = 40

// getNodeInfo assembles the NodeInfo record published in heartbeats.
// It folds in process resource use, NodeIP detection, and reads the
// existing KV record to preserve "draining"/"drained" status set by an
// operator-initiated drain.
func (a *Agent) getNodeInfo() *types.NodeInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	processes := make([]string, 0, len(a.processes))
	var cpuUsed int
	var memUsed int64
	for name, proc := range a.processes {
		processes = append(processes, name)
		if m, ok := a.metricsCollector.GetMetrics(proc.PID()); ok {
			cpuUsed += int(m.CPUPercent * cpuPctToMHzFactor)
			memUsed += m.MemoryMB
		}
	}

	nodeIP := a.cfg.NodeIP
	if nodeIP == "" {
		nodeIP = netutil.LocalIPv4(a.cfg.NATS.Host)
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

	status := types.NodeReady
	if existing, err := a.clusterState.GetNode(a.nodeID); err == nil {
		switch existing.Status {
		case types.NodeDraining, types.NodeDrained:
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
