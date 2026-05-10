package scheduling

import (
	"sort"

	"asty/internal/platform/asty/core/types"
)

// PickCandidates selects up to needed nodes for a placement. The choice
// criteria, in order of priority:
//
//  1. Prefer DCs with fewer copies of this service (geo-diversity).
//  2. Prefer nodes that already host other allocations (cache locality —
//     "pack" services onto warm nodes rather than spreading thin).
//  3. Prefer more free memory (slack for OS pressure).
//  4. Stable tie-break by node ID for deterministic decisions.
//
// dcCounts and nodeAllocCounts are mutated as we pick — that's how the
// "fewer copies in DC X" rule kicks in for subsequent picks within the
// same call.
func (s *Scheduler) PickCandidates(svc *types.ServiceDefinition, healthy []*types.NodeInfo, occupied map[string]bool, dcCounts map[string]int, nodeAllocCounts map[string]int, needed int) []*types.NodeInfo {
	free := s.eligibleNodes(svc, healthy, occupied)
	if len(free) == 0 || needed <= 0 {
		return nil
	}

	// Defensive copies: caller's maps stay untouched.
	working := cloneIntMap(dcCounts)
	packing := cloneIntMap(nodeAllocCounts)

	picked := make([]*types.NodeInfo, 0, needed)
	for len(picked) < needed && len(free) > 0 {
		sort.Slice(free, candidateLess(free, working, packing))
		pick := free[0]
		picked = append(picked, pick)
		working[DatacenterOf(pick)]++
		packing[pick.ID]++
		free = free[1:]
	}
	return picked
}

// eligibleNodes filters out nodes that are already occupied or lack
// resources for svc.
func (s *Scheduler) eligibleNodes(svc *types.ServiceDefinition, healthy []*types.NodeInfo, occupied map[string]bool) []*types.NodeInfo {
	out := make([]*types.NodeInfo, 0, len(healthy))
	for _, n := range healthy {
		if occupied[n.ID] {
			continue
		}
		if !s.hasResources(n, svc.Resources) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// candidateLess returns the comparator used by PickCandidates; pulled
// out for readability.
func candidateLess(nodes []*types.NodeInfo, working, packing map[string]int) func(i, j int) bool {
	return func(i, j int) bool {
		dci, dcj := DatacenterOf(nodes[i]), DatacenterOf(nodes[j])
		if working[dci] != working[dcj] {
			return working[dci] < working[dcj]
		}
		if packing[nodes[i].ID] != packing[nodes[j].ID] {
			return packing[nodes[i].ID] > packing[nodes[j].ID]
		}
		if nodes[i].MemoryAvailable != nodes[j].MemoryAvailable {
			return nodes[i].MemoryAvailable > nodes[j].MemoryAvailable
		}
		return nodes[i].ID < nodes[j].ID
	}
}

func cloneIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// SelectNearestForReplacement picks the healthy node nearest to
// sourceNodeID for replacing an allocation that's leaving sourceNodeID
// (drain, autoscaler scale-up after node removal).
func (s *Scheduler) SelectNearestForReplacement(sourceNodeID string, svc *types.ServiceDefinition) *types.NodeInfo {
	source, err := s.clusterState.GetNode(sourceNodeID)
	if err != nil {
		return nil
	}
	nodes, err := s.clusterState.ListNodes()
	if err != nil {
		return nil
	}
	allocs, err := s.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return nil
	}

	used := NodeIDsOf(LiveAllocations(allocs))
	used[sourceNodeID] = true

	healthy := s.FilterHealthyNodes(nodes)
	return s.SelectNodeForTrafficBasedPlacement(DatacenterOf(source), healthy, svc.Resources, used)
}

// SelectNodeForTrafficBasedPlacement picks a node closest to
// sourceDatacenter, packing onto warmer nodes within the chosen DC.
// "Closest" is defined by the proximity matrix; nodes in unknown DCs
// sort behind everything else (see proximity.unknownDCLatency).
func (s *Scheduler) SelectNodeForTrafficBasedPlacement(sourceDatacenter string, nodes []*types.NodeInfo, required types.Resources, usedNodes map[string]bool) *types.NodeInfo {
	candidates := make([]*types.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if usedNodes[n.ID] {
			continue
		}
		if !s.hasResources(n, required) {
			continue
		}
		candidates = append(candidates, n)
	}
	if len(candidates) == 0 {
		return nil
	}

	dcNodes := GroupByDatacenter(candidates)
	dcs := make([]string, 0, len(dcNodes))
	for dc := range dcNodes {
		dcs = append(dcs, dc)
	}
	sortedDCs := s.proximityMatrix.SortDatacentersByProximity(sourceDatacenter, dcs)

	packing := s.ComputeNodeAllocCounts()
	for _, dc := range sortedDCs {
		group := dcNodes[dc]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if packing[group[i].ID] != packing[group[j].ID] {
				return packing[group[i].ID] > packing[group[j].ID]
			}
			if group[i].MemoryAvailable != group[j].MemoryAvailable {
				return group[i].MemoryAvailable > group[j].MemoryAvailable
			}
			return group[i].ID < group[j].ID
		})
		return group[0]
	}
	return nil
}
