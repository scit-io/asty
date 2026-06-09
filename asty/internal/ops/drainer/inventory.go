package drainer

import (
	"asty/asty/internal/core/types"
)

// allocOnNode pairs a live allocation with its service definition.
// Built up-front by collectAllocs and consumed across the drain
// pipeline (migrate, system, run) so each step has both the alloc
// and the rules it was placed under.
type allocOnNode struct {
	svc   *types.ServiceDefinition
	alloc *types.ServiceAllocation
}

// hasOtherReadyNode reports whether any node OTHER than nodeID is
// Ready. Used as the drain guard so we don't promise migration when
// there's nothing to migrate to. Stale records get TTL'd out by NATS
// before they ever land here, so reading Status directly is sound.
func (dm *DrainManager) hasOtherReadyNode(nodeID string) bool {
	nodes, err := dm.deps.ClusterState().ListNodes()
	if err != nil {
		return false
	}
	for _, n := range nodes {
		if n.ID == nodeID {
			continue
		}
		if n.Status == types.NodeReady {
			return true
		}
	}
	return false
}

// collectAllocs gathers every running/pending/starting allocation
// currently bound to nodeID. Stopped/failed ones are ignored — they're
// not contributing traffic and don't need migration. One Watch-based
// snapshot beats N round-trips when the service list is long.
func (dm *DrainManager) collectAllocs(nodeID string) []allocOnNode {
	svcByName := make(map[string]*types.ServiceDefinition, len(dm.deps.Services()))
	for _, svc := range dm.deps.Services() {
		svcByName[svc.Name] = svc
	}
	all, err := dm.deps.ClusterState().ListAllAllocations()
	if err != nil {
		return nil
	}
	var allocs []allocOnNode
	for _, a := range all {
		if a.NodeID != nodeID || !a.Status.IsLive() {
			continue
		}
		svc, ok := svcByName[a.ServiceName]
		if !ok {
			continue
		}
		allocs = append(allocs, allocOnNode{svc: svc, alloc: a})
	}
	return allocs
}
