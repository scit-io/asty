package scheduler

import (
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// reconcileSystem ensures every healthy node runs one copy of svc.
// System services are co-located with the agent on every node, so the
// "occupied" set covers any allocation already there regardless of
// status — a stopping copy still owns the slot.
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

// reconcileRegular brings the live count of svc up to MinCopies (or the
// number of healthy nodes, whichever is smaller). When more copies are
// needed it asks PickCandidates to choose nodes that maximise DC
// diversity first, then pack onto already-busy nodes (cache locality).
func (s *Scheduler) reconcileRegular(svc *types.ServiceDefinition, healthy []*types.NodeInfo, live []*types.ServiceAllocation, occupied map[string]bool, nodeAllocCounts map[string]int) error {
	target := s.TargetCopies(svc, len(healthy))
	if len(live) >= target {
		return nil
	}
	needed := target - len(live)

	dcCounts := DatacenterCountsByOccupied(healthy, occupied)
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
