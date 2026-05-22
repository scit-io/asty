package scheduler

import (
	"sort"

	"asty/asty/internal/core/types"
)

// OccupiedNodes returns the set of nodeIDs that should not receive a new placement.
func OccupiedNodes(allocs []*types.ServiceAllocation) map[string]bool {
	out := make(map[string]bool, len(allocs))
	for _, a := range allocs {
		if a.Status.Occupies() {
			out[a.NodeID] = true
		}
	}
	return out
}

// LiveAllocations returns allocations that count toward the desired set.
func LiveAllocations(allocs []*types.ServiceAllocation) []*types.ServiceAllocation {
	live := make([]*types.ServiceAllocation, 0, len(allocs))
	for _, a := range allocs {
		if a.Status.IsLive() {
			live = append(live, a)
		}
	}
	return live
}

// NodeIDsOf returns a set of nodeIDs from allocations.
func NodeIDsOf(allocs []*types.ServiceAllocation) map[string]bool {
	out := make(map[string]bool, len(allocs))
	for _, a := range allocs {
		out[a.NodeID] = true
	}
	return out
}

// DatacenterOf returns the datacenter of a node, defaulting to "default".
func DatacenterOf(n *types.NodeInfo) string {
	if n.Datacenter == "" {
		return "default"
	}
	return n.Datacenter
}

// GroupByDatacenter groups nodes by their datacenter.
func GroupByDatacenter(nodes []*types.NodeInfo) map[string][]*types.NodeInfo {
	out := make(map[string][]*types.NodeInfo)
	for _, n := range nodes {
		dc := DatacenterOf(n)
		out[dc] = append(out[dc], n)
	}
	return out
}

// DatacenterCountsByOccupied returns the number of nodes per DC that are
// already occupied by allocations. DCs with zero occupied nodes are still
// present in the result so that placement can prefer empty DCs.
func DatacenterCountsByOccupied(healthy []*types.NodeInfo, occupied map[string]bool) map[string]int {
	counts := make(map[string]int)
	for _, n := range healthy {
		dc := DatacenterOf(n)
		counts[dc] += 0
		if occupied[n.ID] {
			counts[dc]++
		}
	}
	return counts
}

// PickRemovalVictims returns up to n allocations to remove, preferring
// copies on the most-crowded DCs so geo-diversity is preserved as the
// service shrinks. Tie-break by NodeID ascending for deterministic
// operator-visible ordering. Shared between manual-scale (api/dashboard)
// and autoscaler scale-down so both surfaces drain in the same order.
func PickRemovalVictims(live []*types.ServiceAllocation, nodes []*types.NodeInfo, n int) []*types.ServiceAllocation {
	if n <= 0 || len(live) == 0 {
		return nil
	}
	if n >= len(live) {
		out := append([]*types.ServiceAllocation(nil), live...)
		sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
		return out
	}
	nodeDC := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeDC[node.ID] = DatacenterOf(node)
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
	return sorted[:n]
}
