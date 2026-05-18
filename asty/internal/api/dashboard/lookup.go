package dashboard

import (
	"asty/asty/internal/core/types"
)

// allocCounts is the per-node allocation tally used by node listings.
// Defined as a named type so helpers in lookup.go and consumers in
// nodes.go can share it instead of an anonymous struct.
type allocCounts struct {
	Planned int
	Running int
}

// allocByID returns the allocation with the given ID, or nil. Tries the
// streamHub snapshot first (in-memory map lookup, sub-millisecond) and
// falls back to a fresh KV snapshot for the rare case the snapshot is
// still nil during initial replay.
func (api *API) allocByID(allocID string) *types.ServiceAllocation {
	if snap := api.ctx.StreamHub().Snapshot(); snap != nil {
		return snap.AllocByID[allocID]
	}
	allocs, err := api.ctx.ClusterState().ListAllAllocations()
	if err != nil {
		return nil
	}
	for _, a := range allocs {
		if a.ID == allocID {
			return a
		}
	}
	return nil
}

// allocsByNode returns every allocation bound to nodeID. Same snapshot-
// first / KV-fallback strategy as allocByID.
func (api *API) allocsByNode(nodeID string) []*types.ServiceAllocation {
	if snap := api.ctx.StreamHub().Snapshot(); snap != nil {
		return snap.AllocsByNode[nodeID]
	}
	allocs, err := api.ctx.ClusterState().ListAllAllocations()
	if err != nil {
		return nil
	}
	out := make([]*types.ServiceAllocation, 0, len(allocs))
	for _, a := range allocs {
		if a.NodeID == nodeID {
			out = append(out, a)
		}
	}
	return out
}

// nodeAllocCounts returns per-node (Planned, Running) tallies derived
// from the snapshot's AllocsByNode (or a one-shot KV snapshot as
// fallback). Returning a map (instead of computing per-node) keeps
// complexity at O(allocations) regardless of how many nodes are queried
// afterwards.
func (api *API) nodeAllocCounts() map[string]allocCounts {
	out := make(map[string]allocCounts)
	if snap := api.ctx.StreamHub().Snapshot(); snap != nil {
		for nodeID, allocs := range snap.AllocsByNode {
			c := allocCounts{}
			for _, a := range allocs {
				c.Planned++
				if a.Status == types.AllocRunning {
					c.Running++
				}
			}
			out[nodeID] = c
		}
		return out
	}
	allocs, err := api.ctx.ClusterState().ListAllAllocations()
	if err != nil {
		return out
	}
	for _, a := range allocs {
		c := out[a.NodeID]
		c.Planned++
		if a.Status == types.AllocRunning {
			c.Running++
		}
		out[a.NodeID] = c
	}
	return out
}
