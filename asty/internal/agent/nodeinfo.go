package agent

import (
	"os"
	"time"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
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
		nodeIP = netutil.LocalIPv4("")
	}

	caps := a.cfg.Agent.Capacity
	cpuTotal := detectCPUMHz(caps.CPUTotal)
	memTotal := detectMemoryMB(caps.MemoryTotal)
	diskTotal, diskAvail := detectDiskMB(a.workDir)
	swapTotal, swapAvail := detectSwapMB()
	if caps.DiskTotal > 0 {
		diskTotal = caps.DiskTotal
	}
	if caps.SwapTotal > 0 {
		swapTotal = caps.SwapTotal
	}
	diskType := detectDiskType(caps.DiskType)

	var selfCPU float64
	var selfMem int64
	if m, ok := a.metricsCollector.GetMetrics(os.Getpid()); ok {
		selfCPU = m.CPUPercent
		selfMem = m.MemoryMB
	}
	// Asty's full disk footprint: the bin/asty binary plus everything
	// the agent places under work_dir (deployed service binaries + per-
	// service logs). bin/asty lives outside work_dir, so dirSizeMB
	// alone would miss it.
	selfDisk := astyBinarySizeMB() + dirSizeMB(a.workDir)

	status := types.NodeReady
	if existing, err := a.clusterState.GetNode(a.nodeID); err == nil {
		switch existing.Status {
		case types.NodeDraining, types.NodeDrained, types.NodePaused:
			status = existing.Status
		}
	}

	a.natsStats.mu.RLock()
	natsCPU := a.natsStats.cpuPercent
	natsMem := a.natsStats.memoryMB
	natsConn := a.natsStats.connections
	natsSubs := a.natsStats.subscriptions
	natsSlow := a.natsStats.slowConsumers
	natsIn := a.natsStats.inMsgs
	natsOut := a.natsStats.outMsgs
	natsJSMsg := a.natsStats.jetStreamMessages
	natsJSBytes := a.natsStats.jetStreamBytes
	a.natsStats.mu.RUnlock()

	// In dev with capacity overrides the host's real usage is unrelated
	// to the fake totals — a 16 GB host doesn't fit into a 466 MB
	// pretend-node. Sum the components Asty observes instead (managed
	// processes from the loop above + agent + NATS). In prod we ask the
	// OS for the system-wide usage so OS daemons, page cache, and
	// unmanaged processes all count honestly.
	var cpuAvail int
	if caps.CPUTotal > 0 {
		totalUsed := cpuUsed + int(selfCPU*cpuPctToMHzFactor) + int(natsCPU*cpuPctToMHzFactor)
		cpuAvail = cpuTotal - totalUsed
	} else {
		cpuAvail = cpuTotal - detectCPUUsedMHz(cpuTotal)
	}
	if cpuAvail < 0 {
		cpuAvail = 0
	}

	var memAvail int64
	if caps.MemoryTotal > 0 {
		totalUsed := memUsed + selfMem + natsMem
		memAvail = memTotal - totalUsed
	} else {
		memAvail = detectMemoryAvailableMB()
	}
	if memAvail < 0 {
		memAvail = 0
	}

	// NATS disk footprint = binary baseline (dev only) + actual
	// JetStream on-disk bytes. The baseline gives the NATS card a
	// non-zero starting point in dev where JS streams are empty.
	natsDiskMB := natsJSBytes / (1024 * 1024)
	if caps.DiskTotal > 0 {
		natsDiskMB += natsDiskBaselineMB(caps.NATSDiskBaseline)
	}

	// When DiskTotal fakes the disk size (dev), statfs reports the real
	// host filesystem — unrelated to the fake total. Synthesize dev
	// disk usage as:
	//
	//   OS baseline      ← 20% of fake_total, or caps.DiskOSBaseline in MB
	//   + Asty footprint ← bin/asty + work_dir (services + logs)
	//   + NATS footprint ← NATS binary baseline + JS bytes
	if caps.DiskTotal > 0 {
		used := diskOSBaselineMB(diskTotal, caps.DiskOSBaseline) + selfDisk + natsDiskMB
		diskAvail = diskTotal - used
		if diskAvail < 0 {
			diskAvail = 0
		}
	}

	// Swap: in dev override we don't project anything — most prod
	// servers run at 0% swap usage anyway, so leaving swap "free"
	// matches reality. If real swap activity ever happens in dev it'll
	// be invisible here; revisit when that becomes a use case.
	if caps.SwapTotal > 0 {
		swapAvail = swapTotal
	}

	return &types.NodeInfo{
		ID:                    a.nodeID,
		Datacenter:            a.cfg.Datacenter,
		IP:                    nodeIP,
		Status:                status,
		LastSeen:              time.Now(),
		CPUTotal:              cpuTotal,
		CPUAvailable:          cpuAvail,
		MemoryTotal:           memTotal,
		MemoryAvailable:       memAvail,
		DiskTotal:             diskTotal,
		DiskAvailable:         diskAvail,
		DiskType:              diskType,
		SwapTotal:             swapTotal,
		SwapAvailable:         swapAvail,
		SelfCPUPercent:        selfCPU,
		SelfMemoryMB:          selfMem,
		SelfDiskMB:            selfDisk,
		NATSCPUPercent:        natsCPU,
		NATSMemoryMB:          natsMem,
		NATSConnections:       natsConn,
		NATSSubscriptions:     natsSubs,
		NATSSlowConsumers:     natsSlow,
		NATSInMsgs:            natsIn,
		NATSOutMsgs:           natsOut,
		NATSJetStreamMessages: natsJSMsg,
		NATSJetStreamBytes:    natsJSBytes,
		NATSDiskMB:            natsDiskMB,
		Processes:             processes,
	}
}
