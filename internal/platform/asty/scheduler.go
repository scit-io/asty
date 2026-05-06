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
		return s.reconcileRegular(svc, healthy, live, occupied)
	default:
		return fmt.Errorf("unknown service type: %s", svc.Type)
	}
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
func (s *Scheduler) reconcileRegular(svc *ServiceDefinition, healthy []*NodeInfo, live []*ServiceAllocation, occupied map[string]bool) error {
	target := s.targetCopies(len(healthy))
	if len(live) >= target {
		return nil
	}
	needed := target - len(live)

	dcCounts := datacenterCountsByOccupied(healthy, occupied)
	candidates := s.pickCandidates(svc, healthy, occupied, dcCounts, needed)
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

// pickCandidates selects up to `needed` nodes that don't already host svc,
// preferring datacenters with the fewest live copies (geo-spread). Ties are
// broken by node ID for stability — eliminates rescheduling churn between
// reconciliation cycles when nodes report identical free memory.
func (s *Scheduler) pickCandidates(svc *ServiceDefinition, healthy []*NodeInfo, occupied map[string]bool, dcCounts map[string]int, needed int) []*NodeInfo {
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

	// Working copy of DC counts — incremented as we pick, so each subsequent
	// selection drifts toward under-represented DCs.
	working := make(map[string]int, len(dcCounts))
	for dc, c := range dcCounts {
		working[dc] = c
	}

	picked := make([]*NodeInfo, 0, needed)
	for len(picked) < needed && len(free) > 0 {
		sort.Slice(free, func(i, j int) bool {
			dci, dcj := datacenterOf(free[i]), datacenterOf(free[j])
			if working[dci] != working[dcj] {
				return working[dci] < working[dcj]
			}
			if free[i].MemoryAvailable != free[j].MemoryAvailable {
				return free[i].MemoryAvailable > free[j].MemoryAvailable
			}
			return free[i].ID < free[j].ID
		})
		pick := free[0]
		picked = append(picked, pick)
		working[datacenterOf(pick)]++
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

// SelectNodeForTrafficBasedPlacement picks a node closest to sourceDatacenter
// that has free resources and is not already in usedNodes. Used by the
// autoscaler to place an extra copy near hot traffic.
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

	for _, dc := range sortedDCs {
		group := dcNodes[dc]
		sort.Slice(group, func(i, j int) bool {
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
