package autoscaling

import (
	"fmt"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/scheduling"
)

// trafficWindow — how far back we look for gateway-RPS samples when
// deciding "is there sustained traffic on this node?". 60 s smooths
// over per-second jitter while still reacting to traffic shifts within
// a minute.
const trafficWindow = 60 * time.Second

// evaluateScaleUp returns a ScalingDecision when one of two conditions
// holds:
//
//  1. Locality: gateway traffic on a node that doesn't run svc — bring
//     a copy there to keep replies same-node-NATS local.
//  2. Resource pressure: an existing copy is over TargetCPU or
//     TargetMemory — add another copy on a free node.
//
// Returns nil if neither rule fires.
func (as *Autoscaler) evaluateScaleUp(svc *types.ServiceDefinition, live []*types.ServiceAllocation, nodes []*types.NodeInfo) *ScalingDecision {
	if node := as.findNodeWithTrafficWithoutService(nodes, live); node != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      types.ScaleUp,
			Reason:      fmt.Sprintf("gateway traffic on node %s without %s", node.ID, svc.Name),
			TargetNode:  node.ID,
		}
	}
	if hot := as.findOverloadedAlloc(svc, live); hot != nil {
		target := as.pickFreeNode(svc, live, nodes)
		if target == nil {
			return nil
		}
		memPct := 0
		if svc.Resources.Memory > 0 {
			memPct = hot.MemoryUsage * 100 / svc.Resources.Memory
		}
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      types.ScaleUp,
			Reason: fmt.Sprintf(
				"copy on %s exceeded targets (cpu=%d%%, mem=%d%% of %dMB) — adding copy on %s",
				hot.NodeID, hot.CPUUsage, memPct, svc.Resources.Memory, target.ID),
			TargetNode: target.ID,
		}
	}
	return nil
}

// findNodeWithTrafficWithoutService returns the first ready node that
// has gateway traffic but no copy of svc. This is the locality
// trigger — we want every node serving traffic to also have a local
// service backend.
func (as *Autoscaler) findNodeWithTrafficWithoutService(nodes []*types.NodeInfo, live []*types.ServiceAllocation) *types.NodeInfo {
	hasService := scheduling.NodeIDsOf(live)
	for _, node := range nodes {
		if node.Status != types.NodeReady {
			continue
		}
		if hasService[node.ID] {
			continue
		}
		if !as.hasGatewayTraffic(node) {
			continue
		}
		return node
	}
	return nil
}

// hasGatewayTraffic averages valid-RPS samples in the last trafficWindow
// and reports whether the average meets TrafficRPSThreshold. The
// average (rather than max) avoids reacting to single-tick spikes.
func (as *Autoscaler) hasGatewayTraffic(node *types.NodeInfo) bool {
	if as.metricsStore == nil {
		return false
	}
	points := as.metricsStore.GetRPS(node.ID, time.Now().Add(-trafficWindow))
	if len(points) == 0 {
		return false
	}
	var sum float64
	for _, p := range points {
		sum += p.Value
	}
	return sum/float64(len(points)) >= float64(as.cfg.Autoscale.TrafficRPSThreshold)
}

// findOverloadedAlloc returns the first running allocation whose
// CPUUsage or memory utilisation exceeds the configured target. Both
// targets are interpreted as percentages; memory utilisation is
// computed against svc.Resources.Memory (the declared per-copy limit
// in MB). When Resources.Memory is 0 (undeclared limit) the memory
// check is skipped — without a reference capacity there's no
// percentage to compute.
func (as *Autoscaler) findOverloadedAlloc(svc *types.ServiceDefinition, live []*types.ServiceAllocation) *types.ServiceAllocation {
	for _, alloc := range live {
		if alloc.Status != types.AllocRunning || alloc.PID == 0 {
			continue
		}
		if alloc.CPUUsage > as.cfg.Autoscale.TargetCPU {
			return alloc
		}
		if svc.Resources.Memory > 0 {
			memPct := alloc.MemoryUsage * 100 / svc.Resources.Memory
			if memPct > as.cfg.Autoscale.TargetMemory {
				return alloc
			}
		}
	}
	return nil
}

// pickFreeNode delegates to the scheduler's PickCandidates to honour
// the same DC-diversity rules used during initial placement.
func (as *Autoscaler) pickFreeNode(svc *types.ServiceDefinition, live []*types.ServiceAllocation, nodes []*types.NodeInfo) *types.NodeInfo {
	healthy := as.scheduler.FilterHealthyNodes(nodes)
	occupied := scheduling.NodeIDsOf(live)
	dcCounts := scheduling.DatacenterCountsByOccupied(healthy, occupied)
	nodeAllocCounts := as.scheduler.ComputeNodeAllocCounts()
	picks := as.scheduler.PickCandidates(svc, healthy, occupied, dcCounts, nodeAllocCounts, 1)
	if len(picks) == 0 {
		return nil
	}
	return picks[0]
}
