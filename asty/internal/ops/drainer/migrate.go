package drainer

import (
	"context"
	"fmt"

	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/scheduler"

	"github.com/rs/zerolog/log"
)

// placeReplacement picks the nearest healthy peer and creates a new
// "pending" allocation for the service there. The fellBack flag tells
// the caller whether we deleted the source allocation already (used
// when no nearest target was available — we fall back to letting the
// controller pick a node).
func (dm *DrainManager) placeReplacement(ctx context.Context, nodeID string, a allocOnNode) (fellBack bool, err error) {
	target := dm.deps.GetScheduler().SelectNearestForReplacement(nodeID, a.svc)
	if target == nil {
		log.Warn().
			Str("service", a.svc.Name).
			Str("node_id", nodeID).
			Msg("drain: no nearest replacement available, falling back to controller placement")

		if err := dm.deps.GetClusterState().DeleteAllocation(a.svc.Name, nodeID); err != nil {
			return true, fmt.Errorf("delete allocation failed: %w", err)
		}
		if err := dm.waitForHealthyReplacement(ctx, nodeID, a.svc); err != nil {
			return true, err
		}
		return true, nil
	}

	if err := dm.deps.GetClusterState().CreateAllocation(&types.ServiceAllocation{
		ServiceName: a.svc.Name,
		NodeID:      target.ID,
		Status:      types.AllocPending,
		Version:     a.alloc.Version,
	}); err != nil {
		return false, fmt.Errorf("create replacement allocation failed: %w", err)
	}

	log.Info().
		Str("service", a.svc.Name).
		Str("from_node", nodeID).
		Str("to_node", target.ID).
		Str("to_dc", scheduler.DatacenterOf(target)).
		Msg("drain: replacement placed on nearest node")

	if err := dm.waitForHealthyOnNode(ctx, target.ID, a.svc); err != nil {
		return false, err
	}
	return false, nil
}

// finalizeMigration is the second half of a regular-allocation drain:
// once placeReplacement has the new copy healthy, we stop the old one
// and tidy up its KV record. The oldAllocAlreadyDeleted flag covers
// the fallback path where placeReplacement already removed the source.
func (dm *DrainManager) finalizeMigration(ctx context.Context, nodeID string, a allocOnNode, oldAllocAlreadyDeleted bool, op *drainOp, total int) {
	if err := dm.deps.StopServiceOnNode(nodeID, a.svc.Name); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop dispatch failed")
	}

	dm.bumpMigrated(op, total)

	if oldAllocAlreadyDeleted {
		return
	}

	if err := dm.waitForStopped(ctx, nodeID, a.svc); err != nil {
		dm.recordError(op, a.svc.Name, err)
		log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop confirmation failed")
	}

	if err := dm.deps.GetClusterState().DeleteAllocation(a.svc.Name, nodeID); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: delete allocation failed")
	}
}
