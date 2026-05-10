package scheduling

import "asty/internal/platform/asty/core/types"

// OccupiedNodes returns the set of nodeIDs that should not receive a new placement.
func OccupiedNodes(allocs []*types.ServiceAllocation) map[string]bool {
	out := make(map[string]bool, len(allocs))
	for _, a := range allocs {
		switch a.Status {
		case "pending", "starting", "running", "failed":
			out[a.NodeID] = true
		}
	}
	return out
}

// LiveAllocations returns allocations that count toward the desired set.
func LiveAllocations(allocs []*types.ServiceAllocation) []*types.ServiceAllocation {
	live := make([]*types.ServiceAllocation, 0, len(allocs))
	for _, a := range allocs {
		switch a.Status {
		case "pending", "starting", "running":
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
