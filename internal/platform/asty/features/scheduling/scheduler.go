package scheduling

import (
	"context"
	"fmt"
	"sort"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling/proximity"

	"github.com/rs/zerolog/log"
)

// Placement is a scheduling decision: place ServiceName on NodeID.
type Placement struct {
	ServiceName string
	NodeID      string
	Resources   types.Resources
}

const nodeStaleAfter = 10 * time.Minute

// Scheduler maintains the baseline placement for services.
type Scheduler struct {
	clusterState    *state.ClusterState
	cfg             *config.Config
	proximityMatrix *proximity.Matrix
}

func NewScheduler(clusterState *state.ClusterState, cfg *config.Config) *Scheduler {
	pm := proximity.NewMatrix()
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
func (s *Scheduler) ReconcileService(ctx context.Context, svc *types.ServiceDefinition) error {
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

	live := LiveAllocations(allocs)
	occupied := OccupiedNodes(allocs)

	switch svc.Type {
	case types.ServiceTypeSystem:
		return s.reconcileSystem(svc, healthy, occupied)
	case types.ServiceTypeService:
		nodeAllocCounts := s.ComputeNodeAllocCounts()
		return s.reconcileRegular(svc, healthy, live, occupied, nodeAllocCounts)
	default:
		return fmt.Errorf("unknown service type: %s", svc.Type)
	}
}

// ComputeNodeAllocCounts returns per-node count of live allocations across every service.
func (s *Scheduler) ComputeNodeAllocCounts() map[string]int {
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

func (s *Scheduler) reconcileSystem(svc *types.ServiceDefinition, healthy []*types.NodeInfo, occupied map[string]bool) error {
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

func (s *Scheduler) reconcileRegular(svc *types.ServiceDefinition, healthy []*types.NodeInfo, live []*types.ServiceAllocation, occupied map[string]bool, nodeAllocCounts map[string]int) error {
	target := s.targetCopies(len(healthy))
	if len(live) >= target {
		return nil
	}
	needed := target - len(live)

	dcCounts := datacenterCountsByOccupied(healthy, occupied)
	candidates := s.PickCandidates(svc, healthy, occupied, dcCounts, nodeAllocCounts, needed)
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

// PickCandidates selects up to `needed` nodes for placement.
func (s *Scheduler) PickCandidates(svc *types.ServiceDefinition, healthy []*types.NodeInfo, occupied map[string]bool, dcCounts map[string]int, nodeAllocCounts map[string]int, needed int) []*types.NodeInfo {
	free := make([]*types.NodeInfo, 0, len(healthy))
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

	working := make(map[string]int, len(dcCounts))
	for dc, c := range dcCounts {
		working[dc] = c
	}
	packing := make(map[string]int, len(nodeAllocCounts))
	for n, c := range nodeAllocCounts {
		packing[n] = c
	}

	picked := make([]*types.NodeInfo, 0, needed)
	for len(picked) < needed && len(free) > 0 {
		sort.Slice(free, func(i, j int) bool {
			dci, dcj := DatacenterOf(free[i]), DatacenterOf(free[j])
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
		working[DatacenterOf(pick)]++
		packing[pick.ID]++
		free = free[1:]
	}
	return picked
}

func (s *Scheduler) createAllocation(svc *types.ServiceDefinition, nodeID string) error {
	return s.clusterState.CreateAllocation(&types.ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      nodeID,
		Status:      "pending",
		Version:     "latest",
	})
}

// FilterHealthyNodes keeps only ready nodes with recent heartbeats.
func (s *Scheduler) FilterHealthyNodes(nodes []*types.NodeInfo) []*types.NodeInfo {
	return s.filterHealthyNodes(nodes)
}

func (s *Scheduler) filterHealthyNodes(nodes []*types.NodeInfo) []*types.NodeInfo {
	healthy := make([]*types.NodeInfo, 0, len(nodes))
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

func (s *Scheduler) hasResources(node *types.NodeInfo, required types.Resources) bool {
	cpuFree := node.CPUAvailable - s.cfg.ReservedCPU
	memFree := node.MemoryAvailable - int64(s.cfg.ReservedMemory)
	return cpuFree >= required.CPU && memFree >= int64(required.Memory)
}

// SelectNearestForReplacement picks the healthy node nearest to sourceNodeID.
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

	healthy := s.filterHealthyNodes(nodes)
	return s.SelectNodeForTrafficBasedPlacement(DatacenterOf(source), healthy, svc.Resources, used)
}

// SelectNodeForTrafficBasedPlacement picks a node closest to sourceDatacenter.
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
