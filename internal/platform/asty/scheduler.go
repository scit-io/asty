package asty

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

// Scheduler handles service placement decisions
type Scheduler struct {
	clusterState    *ClusterState
	cfg             *Config
	proximityMatrix *ProximityMatrix
}

// NewScheduler creates a new scheduler
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

// ScheduleService determines where to place service instances
func (s *Scheduler) ScheduleService(ctx context.Context, svc *ServiceDefinition) ([]*Placement, error) {
	nodes, err := s.clusterState.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Filter healthy nodes
	healthyNodes := s.filterHealthyNodes(nodes)
	if len(healthyNodes) == 0 {
		return nil, fmt.Errorf("no healthy nodes available")
	}

	switch svc.Type {
	case ServiceTypeSystem:
		return s.scheduleSystemService(svc, healthyNodes)
	case ServiceTypeService:
		return s.scheduleRegularService(svc, healthyNodes)
	default:
		return nil, fmt.Errorf("unknown service type: %s", svc.Type)
	}
}

// scheduleSystemService places one copy on every node
func (s *Scheduler) scheduleSystemService(svc *ServiceDefinition, nodes []*NodeInfo) ([]*Placement, error) {
	placements := make([]*Placement, 0, len(nodes))

	for _, node := range nodes {
		// Check if node has sufficient resources
		if !s.hasResources(node, svc.Resources) {
			log.Warn().
				Str("node_id", node.ID).
				Str("service", svc.Name).
				Msg("node lacks resources for system service")
			continue
		}

		placements = append(placements, &Placement{
			ServiceName: svc.Name,
			NodeID:      node.ID,
			Resources:   svc.Resources,
		})
	}

	log.Info().
		Str("service", svc.Name).
		Str("type", "system").
		Int("placements", len(placements)).
		Msg("scheduled system service")

	return placements, nil
}

// scheduleRegularService places min 3 copies in different datacenters
func (s *Scheduler) scheduleRegularService(svc *ServiceDefinition, nodes []*NodeInfo) ([]*Placement, error) {
	minCopies := s.cfg.MinCopies
	if minCopies < 3 {
		minCopies = 3 // Hard minimum for geo-diversity
	}

	// Group nodes by datacenter
	dcNodes := s.groupNodesByDatacenter(nodes)

	// Get list of datacenters sorted by node count (prefer DCs with more nodes)
	datacenters := s.sortDatacentersByCapacity(dcNodes)

	placements := make([]*Placement, 0, minCopies)
	usedNodes := make(map[string]bool)

	// Place at least one copy in each datacenter (up to 3 DCs)
	for i := 0; i < minCopies && i < len(datacenters); i++ {
		dc := datacenters[i]
		dcNodeList := dcNodes[dc]

		// Find best node in this datacenter
		node := s.selectBestNode(dcNodeList, svc.Resources, usedNodes)
		if node == nil {
			log.Warn().
				Str("datacenter", dc).
				Str("service", svc.Name).
				Msg("no suitable node in datacenter")
			continue
		}

		placements = append(placements, &Placement{
			ServiceName: svc.Name,
			NodeID:      node.ID,
			Resources:   svc.Resources,
		})

		usedNodes[node.ID] = true
	}

	// If we need more copies (fewer than 3 DCs), place additional copies
	for len(placements) < minCopies {
		node := s.selectBestNode(nodes, svc.Resources, usedNodes)
		if node == nil {
			break // No more suitable nodes
		}

		placements = append(placements, &Placement{
			ServiceName: svc.Name,
			NodeID:      node.ID,
			Resources:   svc.Resources,
		})

		usedNodes[node.ID] = true
	}

	if len(placements) < minCopies {
		return nil, fmt.Errorf("insufficient resources: needed %d copies, only placed %d", minCopies, len(placements))
	}

	log.Info().
		Str("service", svc.Name).
		Str("type", "service").
		Int("placements", len(placements)).
		Msg("scheduled service")

	return placements, nil
}

// Placement represents a service placement decision
type Placement struct {
	ServiceName string
	NodeID      string
	Resources   Resources
}

// filterHealthyNodes returns only healthy nodes
func (s *Scheduler) filterHealthyNodes(nodes []*NodeInfo) []*NodeInfo {
	healthy := make([]*NodeInfo, 0, len(nodes))

	for _, node := range nodes {
		if node.Status != "ready" {
			continue
		}

		// Check if node is recent (within 2x TTL)
		if time.Since(node.LastSeen) > 10*time.Minute {
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

// hasResources checks if node has sufficient resources
func (s *Scheduler) hasResources(node *NodeInfo, required Resources) bool {
	cpuAvailable := node.CPUAvailable - s.cfg.ReservedCPU
	memoryAvailable := node.MemoryAvailable - int64(s.cfg.ReservedMemory)

	return cpuAvailable >= required.CPU && memoryAvailable >= int64(required.Memory)
}

// groupNodesByDatacenter groups nodes by datacenter
func (s *Scheduler) groupNodesByDatacenter(nodes []*NodeInfo) map[string][]*NodeInfo {
	dcNodes := make(map[string][]*NodeInfo)

	for _, node := range nodes {
		dc := node.Datacenter
		if dc == "" {
			dc = "default"
		}
		dcNodes[dc] = append(dcNodes[dc], node)
	}

	return dcNodes
}

// sortDatacentersByCapacity returns datacenters sorted by available capacity
func (s *Scheduler) sortDatacentersByCapacity(dcNodes map[string][]*NodeInfo) []string {
	type dcCapacity struct {
		name     string
		capacity int64
	}

	capacities := make([]dcCapacity, 0, len(dcNodes))

	for dc, nodes := range dcNodes {
		totalCapacity := int64(0)
		for _, node := range nodes {
			totalCapacity += node.MemoryAvailable
		}
		capacities = append(capacities, dcCapacity{name: dc, capacity: totalCapacity})
	}

	// Sort by capacity descending
	sort.Slice(capacities, func(i, j int) bool {
		return capacities[i].capacity > capacities[j].capacity
	})

	result := make([]string, len(capacities))
	for i, dc := range capacities {
		result[i] = dc.name
	}

	return result
}

// SelectNodeForTrafficBasedPlacement selects node for traffic-based placement
// Prefers node with traffic, falls back to proximity-aware selection
func (s *Scheduler) SelectNodeForTrafficBasedPlacement(sourceDatacenter string, nodes []*NodeInfo, required Resources, usedNodes map[string]bool) *NodeInfo {
	// Filter available nodes
	candidates := make([]*NodeInfo, 0)
	for _, node := range nodes {
		if usedNodes[node.ID] {
			continue
		}
		if !s.hasResources(node, required) {
			continue
		}
		candidates = append(candidates, node)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Group by datacenter
	dcNodes := s.groupNodesByDatacenter(candidates)

	// Sort datacenters by proximity to source
	dcList := make([]string, 0, len(dcNodes))
	for dc := range dcNodes {
		dcList = append(dcList, dc)
	}
	sortedDCs := s.proximityMatrix.SortDatacentersByProximity(sourceDatacenter, dcList)

	// Try each DC in order of proximity
	for _, dc := range sortedDCs {
		dcNodeList := dcNodes[dc]
		if len(dcNodeList) == 0 {
			continue
		}

		// Pick node with most available memory in this DC
		sort.Slice(dcNodeList, func(i, j int) bool {
			return dcNodeList[i].MemoryAvailable > dcNodeList[j].MemoryAvailable
		})

		return dcNodeList[0]
	}

	return nil
}

// selectBestNode selects the best node for placement using round-robin
func (s *Scheduler) selectBestNode(nodes []*NodeInfo, required Resources, usedNodes map[string]bool) *NodeInfo {
	// Filter nodes with sufficient resources and not already used
	candidates := make([]*NodeInfo, 0)

	for _, node := range nodes {
		if usedNodes[node.ID] {
			continue
		}

		if !s.hasResources(node, required) {
			continue
		}

		candidates = append(candidates, node)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Simple round-robin: pick node with most available memory
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].MemoryAvailable > candidates[j].MemoryAvailable
	})

	return candidates[0]
}

// ReconcileService ensures service has correct number of instances
func (s *Scheduler) ReconcileService(ctx context.Context, svc *ServiceDefinition) error {
	// Get current allocations
	allocs, err := s.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return fmt.Errorf("failed to list allocations: %w", err)
	}

	// Get desired placements
	placements, err := s.ScheduleService(ctx, svc)
	if err != nil {
		return fmt.Errorf("failed to schedule service: %w", err)
	}

	// Build maps for comparison
	currentNodes := make(map[string]bool)
	for _, alloc := range allocs {
		if alloc.Status == "running" || alloc.Status == "pending" {
			currentNodes[alloc.NodeID] = true
		}
	}

	desiredNodes := make(map[string]bool)
	for _, placement := range placements {
		desiredNodes[placement.NodeID] = true
	}

	// Find nodes to add
	for _, placement := range placements {
		if !currentNodes[placement.NodeID] {
			log.Info().
				Str("service", svc.Name).
				Str("node_id", placement.NodeID).
				Msg("creating allocation")

			alloc := &ServiceAllocation{
				ServiceName: svc.Name,
				NodeID:      placement.NodeID,
				Status:      "pending",
				Version:     "latest", // TODO: version management
			}

			if err := s.clusterState.CreateAllocation(alloc); err != nil {
				log.Error().Err(err).Msg("failed to create allocation")
			}
		}
	}

	// Find nodes to remove
	for _, alloc := range allocs {
		if !desiredNodes[alloc.NodeID] && (alloc.Status == "running" || alloc.Status == "pending") {
			log.Info().
				Str("service", svc.Name).
				Str("node_id", alloc.NodeID).
				Msg("removing allocation")

			if err := s.clusterState.DeleteAllocation(svc.Name, alloc.NodeID); err != nil {
				log.Error().Err(err).Msg("failed to delete allocation")
			}
		}
	}

	return nil
}
