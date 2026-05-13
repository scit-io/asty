package autoscaling

import (
	"fmt"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/autoscaling/metrics"
	"asty/asty/internal/features/scheduling"

	"github.com/rs/zerolog/log"
)

func (as *Autoscaler) executeScaleUp(d *ScalingDecision, svc *types.ServiceDefinition) error {
	log.Info().
		Str("service", svc.Name).
		Str("target_node", d.TargetNode).
		Str("reason", d.Reason).
		Msg("scaling up")

	alloc := &types.ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      d.TargetNode,
		Status:      types.AllocPending,
		Version:     "latest",
	}
	if err := as.clusterState.CreateAllocation(alloc); err != nil {
		return fmt.Errorf("failed to create allocation: %w", err)
	}
	if err := as.clusterState.MarkScaleUp(svc.Name, time.Now()); err != nil {
		log.Warn().Err(err).Str("service", svc.Name).Msg("failed to persist scale-up cooldown")
	}
	as.recordEvent(svc.Name, types.ScaleUp, d.Reason, d.TargetNode, +1)
	return nil
}

func (as *Autoscaler) executeScaleDown(d *ScalingDecision, svc *types.ServiceDefinition) error {
	if d.RemoveNode == "" {
		return fmt.Errorf("scale_down decision missing RemoveNode")
	}
	log.Info().
		Str("service", svc.Name).
		Str("remove_node", d.RemoveNode).
		Str("reason", d.Reason).
		Msg("scaling down")

	if err := as.clusterState.DeleteAllocation(svc.Name, d.RemoveNode); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}
	if err := as.clusterState.MarkScaleDown(svc.Name, time.Now()); err != nil {
		log.Warn().Err(err).Str("service", svc.Name).Msg("failed to persist scale-down cooldown")
	}
	as.recordEvent(svc.Name, types.ScaleDown, d.Reason, d.RemoveNode, -1)
	return nil
}

// recordEvent re-reads the allocation list to compute "before/after"
// counts for the event ring buffer. delta is +1 for ScaleUp, -1 for
// ScaleDown so the FromCount/ToCount fields read correctly.
func (as *Autoscaler) recordEvent(service string, action types.ScalingAction, reason, nodeID string, delta int) {
	allocs, _ := as.clusterState.ListAllocations(service)
	count := len(scheduling.LiveAllocations(allocs))
	as.metricsStore.AddEvent(metrics.ScalingEvent{
		Service:   service,
		Action:    action,
		Reason:    reason,
		FromCount: count - delta,
		ToCount:   count,
		NodeID:    nodeID,
	})
}
