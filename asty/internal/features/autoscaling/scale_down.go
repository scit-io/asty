package autoscaling

import (
	"fmt"
	"sort"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/scheduling"
)

// idleFloorDivisor — when average usage drops below TargetCPU/this and
// TargetMemory/this, we consider the service idle enough to remove a
// copy. 2 means "less than half of the scale-up target". Picking a
// gentler floor than the up-target gives hysteresis: a service won't
// flap right back to scale-up after a scale-down.
const idleFloorDivisor = 2

// evaluateScaleDown returns a scale_down decision when MinCopies is
// satisfied AND average resource use across running copies is below
// half of the scale-up target. The chosen victim is the alloc on the
// most-crowded DC (preserves geo-diversity).
func (as *Autoscaler) evaluateScaleDown(svc *types.ServiceDefinition, live []*types.ServiceAllocation) *ScalingDecision {
	floor := as.cfg.Autoscale.MinCopies
	if floor < 1 {
		floor = 1
	}
	if len(live) <= floor {
		return nil
	}

	running := liveRunning(live)
	if len(running) <= floor {
		return nil
	}

	avgCPU, avgMem := averageUsage(running)
	cpuFloor := as.cfg.Autoscale.TargetCPU / idleFloorDivisor
	memFloor := as.cfg.Autoscale.TargetMemory / idleFloorDivisor
	if avgCPU > cpuFloor || avgMem > memFloor {
		return nil
	}

	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil
	}
	victim := as.pickAllocationToRemove(running, nodes)
	if victim == nil {
		return nil
	}
	return &ScalingDecision{
		ServiceName: svc.Name,
		Action:      "scale_down",
		Reason: fmt.Sprintf(
			"avg cpu=%d%% mem=%dMB across %d copies, floor cpu=%d mem=%d",
			avgCPU, avgMem, len(running), cpuFloor, memFloor),
		RemoveNode: victim.NodeID,
	}
}

func liveRunning(live []*types.ServiceAllocation) []*types.ServiceAllocation {
	out := make([]*types.ServiceAllocation, 0, len(live))
	for _, a := range live {
		if a.Status == types.AllocRunning && a.PID > 0 {
			out = append(out, a)
		}
	}
	return out
}

func averageUsage(allocs []*types.ServiceAllocation) (avgCPU, avgMem int) {
	if len(allocs) == 0 {
		return 0, 0
	}
	var cpuSum, memSum int
	for _, a := range allocs {
		cpuSum += a.CPUUsage
		memSum += a.MemoryUsage
	}
	return cpuSum / len(allocs), memSum / len(allocs)
}

// pickAllocationToRemove chooses the victim for scale-down: prefer
// the most-crowded DC (preserves geo-diversity), tie-break by node ID
// for deterministic decisions.
func (as *Autoscaler) pickAllocationToRemove(live []*types.ServiceAllocation, nodes []*types.NodeInfo) *types.ServiceAllocation {
	if len(live) == 0 {
		return nil
	}
	nodeDC := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeDC[n.ID] = scheduling.DatacenterOf(n)
	}
	dcCount := make(map[string]int)
	for _, a := range live {
		dcCount[nodeDC[a.NodeID]]++
	}
	sorted := append([]*types.ServiceAllocation(nil), live...)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := nodeDC[sorted[i].NodeID], nodeDC[sorted[j].NodeID]
		if dcCount[di] != dcCount[dj] {
			return dcCount[di] > dcCount[dj]
		}
		return sorted[i].NodeID < sorted[j].NodeID
	})
	return sorted[0]
}
