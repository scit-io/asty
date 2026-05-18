package autoscaling

import (
	"fmt"
	"sort"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/scheduling"

	"github.com/rs/zerolog/log"
)

// idleFloorDivisor — when average usage drops below TargetCPU/this and
// TargetMemory/this, we consider the service idle enough to remove a
// copy. 2 means "less than half of the scale-up target". Picking a
// gentler floor than the up-target gives hysteresis: a service won't
// flap right back to scale-up after a scale-down.
const idleFloorDivisor = 2

// evaluateScaleDown returns a scale_down decision when:
//
//   - MinCopies is satisfied,
//   - average resource use across running copies is below the floor,
//   - service has stayed under the floor continuously for IdleHold
//     (anti-flap hysteresis on top of cooldown).
//
// The chosen victim is the alloc on the most-crowded DC (preserves
// geo-diversity). IdleSince in the cooldown record is the timestamp
// when the service first dropped below the floor; we clear it whenever
// usage rises above the floor so the next idle window is measured
// fresh.
func (as *Autoscaler) evaluateScaleDown(svc *types.ServiceDefinition, live []*types.ServiceAllocation) *ScalingDecision {
	floor := as.cfg.Autoscale.MinCopies
	if override, ok := as.clusterState.GetServiceScale(svc.Name); ok {
		floor = override
	}
	if floor < 1 {
		floor = 1
	}
	if len(live) <= floor {
		as.clearIdleMarker(svc.Name)
		return nil
	}

	running := liveRunning(live)
	if len(running) <= floor {
		as.clearIdleMarker(svc.Name)
		return nil
	}

	avgCPU, avgMem := averageUsage(running)
	cpuFloor := as.cfg.Autoscale.TargetCPU / idleFloorDivisor
	memFloor := as.cfg.Autoscale.TargetMemory / idleFloorDivisor
	if avgCPU > cpuFloor {
		as.clearIdleMarker(svc.Name)
		return nil
	}
	// Memory check is in percent of svc.Resources.Memory; skip when the
	// service didn't declare a per-copy memory limit (memFloor would have
	// no reference point).
	if svc.Resources.Memory > 0 {
		avgMemPct := avgMem * 100 / svc.Resources.Memory
		if avgMemPct > memFloor {
			as.clearIdleMarker(svc.Name)
			return nil
		}
	}

	// Usage is below the floor — service is idle. If this is the first
	// observation we mark it; if the idle window hasn't elapsed yet we
	// withhold the scale-down decision.
	if !as.idleWindowSatisfied(svc.Name) {
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
	avgMemPctStr := "n/a"
	if svc.Resources.Memory > 0 {
		avgMemPctStr = fmt.Sprintf("%d%%", avgMem*100/svc.Resources.Memory)
	}
	return &ScalingDecision{
		ServiceName: svc.Name,
		Action:      types.ScaleDown,
		Reason: fmt.Sprintf(
			"avg cpu=%d%% mem=%s across %d copies, floor cpu=%d mem=%d (percent of svc.Resources.Memory=%dMB)",
			avgCPU, avgMemPctStr, len(running), cpuFloor, memFloor, svc.Resources.Memory),
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

// idleWindowSatisfied implements the IdleHold hysteresis. The first
// time usage drops below the floor it stamps IdleSince=now and returns
// false (so this tick doesn't yet scale down). Subsequent ticks check
// elapsed time against cfg.Autoscale.IdleHold; only after the full
// window has passed does the function return true and clear the
// marker. Returning early on cooldown-read errors (treat as "not yet"
// — safer to under-scale than to over-shrink during a transient).
func (as *Autoscaler) idleWindowSatisfied(service string) bool {
	hold := as.cfg.Autoscale.IdleHold
	if hold <= 0 {
		return true
	}
	cd, err := as.clusterState.GetServiceCooldown(service)
	if err != nil {
		log.Warn().Err(err).Str("service", service).Msg("idle-window read failed; deferring scale-down")
		return false
	}
	if cd.IdleSince.IsZero() {
		if err := as.clusterState.MarkIdleSince(service, time.Now()); err != nil {
			log.Warn().Err(err).Str("service", service).Msg("failed to mark idle_since")
		}
		return false
	}
	return time.Since(cd.IdleSince) >= hold
}

// clearIdleMarker is the inverse: usage rose back above the floor, so
// the next idle window must restart from scratch.
func (as *Autoscaler) clearIdleMarker(service string) {
	cd, err := as.clusterState.GetServiceCooldown(service)
	if err != nil || cd.IdleSince.IsZero() {
		return
	}
	if err := as.clusterState.MarkIdleSince(service, time.Time{}); err != nil {
		log.Warn().Err(err).Str("service", service).Msg("failed to clear idle_since")
	}
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
