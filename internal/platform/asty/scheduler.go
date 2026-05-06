package asty

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

// Scheduler maintains the baseline placement (MinCopies, geo-spread) for services.
// It is idempotent: existing live allocations are preserved across reconciliation
// cycles, and only deficits below MinCopies trigger new placements. The autoscaler
// handles growth above MinCopies and shrinkage back to it.
type Scheduler struct {
	clusterState    *ClusterState
	cfg             *Config
	proximityMatrix *ProximityMatrix
}

// Placement is a scheduling decision: place ServiceName on NodeID.
type Placement struct {
	ServiceName string
	NodeID      string
	Resources   Resources
}

// nodeStaleAfter is how long a node can go without a heartbeat before being
// excluded from placement. Matches NodeInfo TTL × 2.
const nodeStaleAfter = 10 * time.Minute

func NewScheduler(clusterState *ClusterState, cfg *Config) *Scheduler {
	pm := NewProximityMatrix()
	if err := pm.LoadFromConfig(cfg.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}
	return &Scheduler{
		clusterState:    clusterState,
		cfg:             cfg,
		proximityMatrix: pm,
	}
}

// ReconcileService brings allocations of svc up to the baseline target.
//
// For ServiceTypeSystem: ensures one live allocation on each healthy node.
// For ServiceTypeService: ensures at least targetCopies live allocations,
// preferring geo-diversity. Allocations above the target are NEVER removed
// here — that is the autoscaler's job. This is what makes reconciliation
// idempotent and stops the rescheduling churn.
func (s *Scheduler) ReconcileService(ctx context.Context, svc *ServiceDefinition) error {
	allocs, err := s.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return fmt.Errorf("failed to list allocations: %w", err)
	}
	nodes, err := s.clusterState.ListNodes()
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}
	healthy := s.filterHealthyNodes(nodes)
	if len(healthy) == 0 {
		return fmt.Errorf("no healthy nodes available")
	}

	live := liveAllocations(allocs)
	occupied := nodeIDsOf(live)

	switch svc.Type {
	case ServiceTypeSystem:
		return s.reconcileSystem(svc, healthy, occupied)
	case ServiceTypeService:
		// Packing pressure: count live allocations on each node across ALL
		// services. pickCandidates uses this to concentrate new placements on
		// nodes that already host other services — at bootstrap, all services
		// land on the same MinCopies nodes (one per DC) instead of spreading
		// across the whole cluster.
		nodeAllocCounts := s.computeNodeAllocCounts()
		return s.reconcileRegular(svc, healthy, live, occupied, nodeAllocCounts)
	default:
		return fmt.Errorf("unknown service type: %s", svc.Type)
	}
}

// computeNodeAllocCounts returns per-node count of live allocations across
// every service. Errors are swallowed because the count is an advisory tiebreak
// — placement still works correctly if the count is zero everywhere.
func (s *Scheduler) computeNodeAllocCounts() map[string]int {
	out := make(map[string]int)
	all, err := s.clusterState.ListAllAllocations()
	if err != nil {
		return out
	}
	for _, a := range all {
		switch a.Status {
		case "pending", "starting", "running":
			out[a.NodeID]++
		}
	}
	return out
}

// reconcileSystem: one allocation per healthy node, idempotent.
func (s *Scheduler) reconcileSystem(svc *ServiceDefinition, healthy []*NodeInfo, occupied map[string]bool) error {
	added := 0
	for _, node := range healthy {
		if occupied[node.ID] {
			continue
		}
		if !s.hasResources(node, svc.Resources) {
			log.Warn().
				Str("service", svc.Name).
				Str("node_id", node.ID).
				Msg("node lacks resources for system service")
			continue
		}
		if err := s.createAllocation(svc, node.ID); err != nil {
			log.Error().Err(err).
				Str("service", svc.Name).
				Str("node_id", node.ID).
				Msg("failed to create system allocation")
			continue
		}
		added++
	}
	if added > 0 {
		log.Info().
			Str("service", svc.Name).
			Str("type", "system").
			Int("added", added).
			Msg("system service reconciled")
	}
	return nil
}

// reconcileRegular: top up to targetCopies, never shrink.
func (s *Scheduler) reconcileRegular(svc *ServiceDefinition, healthy []*NodeInfo, live []*ServiceAllocation, occupied map[string]bool, nodeAllocCounts map[string]int) error {
	target := s.targetCopies(len(healthy))
	if len(live) >= target {
		return nil
	}
	needed := target - len(live)

	dcCounts := datacenterCountsByOccupied(healthy, occupied)
	candidates := s.pickCandidates(svc, healthy, occupied, dcCounts, nodeAllocCounts, needed)
	if len(candidates) == 0 {
		log.Warn().
			Str("service", svc.Name).
			Int("live", len(live)).
			Int("target", target).
			Msg("no candidate nodes for placement")
		return nil
	}

	for _, node := range candidates {
		if err := s.createAllocation(svc, node.ID); err != nil {
			log.Error().Err(err).
				Str("service", svc.Name).
				Str("node_id", node.ID).
				Msg("failed to create allocation")
			continue
		}
	}
	log.Info().
		Str("service", svc.Name).
		Str("type", "service").
		Int("added", len(candidates)).
		Int("target", target).
		Int("live", len(live)+len(candidates)).
		Msg("service reconciled")
	return nil
}

// targetCopies bounds MinCopies by the number of healthy nodes — never request
// more copies than nodes that can host them.
func (s *Scheduler) targetCopies(healthyNodes int) int {
	target := s.cfg.MinCopies
	if target < 1 {
		target = 1
	}
	if target > healthyNodes {
		target = healthyNodes
	}
	return target
}

// pickCandidates selects up to `needed` nodes that don't already host svc.
// Sort priority:
//  1. Geo-spread: prefer DCs with fewest live copies of THIS service
//  2. Packing: within DC, prefer nodes that already host other services
//     (concentrates the bootstrap on a minimal set; idle nodes stay free
//     for autoscale-on-demand)
//  3. Free memory (more = better) — capacity-aware tiebreak
//  4. Node ID — stable tiebreak; eliminates churn between reconciliation
//     cycles when nodes are otherwise identical
//
// nodeAllocCounts is the global per-node live-allocation count across all
// services; passing nil disables the packing tiebreak.
func (s *Scheduler) pickCandidates(svc *ServiceDefinition, healthy []*NodeInfo, occupied map[string]bool, dcCounts map[string]int, nodeAllocCounts map[string]int, needed int) []*NodeInfo {
	free := make([]*NodeInfo, 0, len(healthy))
	for _, n := range healthy {
		if occupied[n.ID] {
			continue
		}
		if !s.hasResources(n, svc.Resources) {
			continue
		}
		free = append(free, n)
	}
	if len(free) == 0 || needed <= 0 {
		return nil
	}

	// Working copies — both DC counts and per-node alloc counts are
	// incremented as we pick. Each subsequent selection drifts toward
	// under-represented DCs while still preferring already-used nodes
	// within the chosen DC.
	working := make(map[string]int, len(dcCounts))
	for dc, c := range dcCounts {
		working[dc] = c
	}
	packing := make(map[string]int, len(nodeAllocCounts))
	for n, c := range nodeAllocCounts {
		packing[n] = c
	}

	picked := make([]*NodeInfo, 0, needed)
	for len(picked) < needed && len(free) > 0 {
		sort.Slice(free, func(i, j int) bool {
			dci, dcj := datacenterOf(free[i]), datacenterOf(free[j])
			if working[dci] != working[dcj] {
				return working[dci] < working[dcj]
			}
			if packing[free[i].ID] != packing[free[j].ID] {
				return packing[free[i].ID] > packing[free[j].ID]
			}
			if free[i].MemoryAvailable != free[j].MemoryAvailable {
				return free[i].MemoryAvailable > free[j].MemoryAvailable
			}
			return free[i].ID < free[j].ID
		})
		pick := free[0]
		picked = append(picked, pick)
		working[datacenterOf(pick)]++
		packing[pick.ID]++
		free = free[1:]
	}
	return picked
}

func (s *Scheduler) createAllocation(svc *ServiceDefinition, nodeID string) error {
	return s.clusterState.CreateAllocation(&ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      nodeID,
		Status:      "pending",
		Version:     "latest",
	})
}

// filterHealthyNodes keeps only nodes that are ready and have heartbeated
// recently. Stale or draining nodes are excluded from placement decisions.
func (s *Scheduler) filterHealthyNodes(nodes []*NodeInfo) []*NodeInfo {
	healthy := make([]*NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.Status != "ready" {
			continue
		}
		if time.Since(node.LastSeen) > nodeStaleAfter {
			log.Warn().
				Str("node_id", node.ID).
				Time("last_seen", node.LastSeen).
				Msg("node stale, excluding from scheduling")
			continue
		}
		healthy = append(healthy, node)
	}
	return healthy
}

func (s *Scheduler) hasResources(node *NodeInfo, required Resources) bool {
	cpuFree := node.CPUAvailable - s.cfg.ReservedCPU
	memFree := node.MemoryAvailable - int64(s.cfg.ReservedMemory)
	return cpuFree >= required.CPU && memFree >= int64(required.Memory)
}

// SelectNearestForReplacement picks the healthy node nearest to sourceNodeID
// that has free resources for svc and isn't already hosting it. The intended
// caller is the drain manager: when a node is being drained, freed allocations
// should land on the closest free node — not get spread across the cluster by
// the global geo-balance heuristic.
//
// Returns nil if no suitable node exists; caller should fall back to whatever
// the scheduler picks on the next reconcile.
func (s *Scheduler) SelectNearestForReplacement(sourceNodeID string, svc *ServiceDefinition) *NodeInfo {
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

	// Exclude source and any node already hosting a live copy.
	used := nodeIDsOf(liveAllocations(allocs))
	used[sourceNodeID] = true

	healthy := s.filterHealthyNodes(nodes)
	return s.SelectNodeForTrafficBasedPlacement(datacenterOf(source), healthy, svc.Resources, used)
}

// SelectNodeForTrafficBasedPlacement picks a node closest to sourceDatacenter
// that has free resources and is not already in usedNodes. Used by the
// autoscaler to place an extra copy near hot traffic, and by the drain manager
// to place a replacement near the drained node. Within the closest DC,
// prefers nodes that already host other services (packing) so new placements
// don't fan out across the cluster.
func (s *Scheduler) SelectNodeForTrafficBasedPlacement(sourceDatacenter string, nodes []*NodeInfo, required Resources, usedNodes map[string]bool) *NodeInfo {
	candidates := make([]*NodeInfo, 0, len(nodes))
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

	dcNodes := groupByDatacenter(candidates)
	dcs := make([]string, 0, len(dcNodes))
	for dc := range dcNodes {
		dcs = append(dcs, dc)
	}
	sortedDCs := s.proximityMatrix.SortDatacentersByProximity(sourceDatacenter, dcs)

	packing := s.computeNodeAllocCounts()

	for _, dc := range sortedDCs {
		group := dcNodes[dc]
		sort.Slice(group, func(i, j int) bool {
			if packing[group[i].ID] != packing[group[j].ID] {
				return packing[group[i].ID] > packing[group[j].ID]
			}
			if group[i].MemoryAvailable != group[j].MemoryAvailable {
				return group[i].MemoryAvailable > group[j].MemoryAvailable
			}
			return group[i].ID < group[j].ID
		})
		if len(group) > 0 {
			return group[0]
		}
	}
	return nil
}

// liveAllocations returns the allocations that count toward the desired set —
// pending (just created), running (started), or starting (start-command sent).
// Stopped/failed allocations are excluded.
func liveAllocations(allocs []*ServiceAllocation) []*ServiceAllocation {
	live := make([]*ServiceAllocation, 0, len(allocs))
	for _, a := range allocs {
		switch a.Status {
		case "pending", "starting", "running":
			live = append(live, a)
		}
	}
	return live
}

func nodeIDsOf(allocs []*ServiceAllocation) map[string]bool {
	out := make(map[string]bool, len(allocs))
	for _, a := range allocs {
		out[a.NodeID] = true
	}
	return out
}

func datacenterOf(n *NodeInfo) string {
	if n.Datacenter == "" {
		return "default"
	}
	return n.Datacenter
}

func groupByDatacenter(nodes []*NodeInfo) map[string][]*NodeInfo {
	out := make(map[string][]*NodeInfo)
	for _, n := range nodes {
		dc := datacenterOf(n)
		out[dc] = append(out[dc], n)
	}
	return out
}

// datacenterCountsByOccupied counts how many of `occupied` nodes live in each
// DC. Used to bias new placements toward under-represented DCs.
func datacenterCountsByOccupied(healthy []*NodeInfo, occupied map[string]bool) map[string]int {
	counts := make(map[string]int)
	for _, n := range healthy {
		counts[datacenterOf(n)] += 0 // ensure all DCs present
		if occupied[n.ID] {
			counts[datacenterOf(n)]++
		}
	}
	return counts
}
