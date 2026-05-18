package drainer

import (
	"context"

	"github.com/rs/zerolog/log"
)

// dismantleAndConfirm stops a system-type service in place. There is
// no migration target — system services run one-per-node by definition.
// We dispatch a stop, wait for the agent to confirm it's actually
// stopped (or hit the timeout), then delete the allocation record.
func (dm *DrainManager) dismantleAndConfirm(ctx context.Context, nodeID string, a allocOnNode, op *drainOp, total int) {
	if err := dm.deps.StopServiceOnNode(nodeID, a.svc.Name); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop dispatch failed")
	}

	if err := dm.waitForStopped(ctx, nodeID, a.svc); err != nil {
		dm.recordError(op, a.svc.Name, err)
		log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop confirmation failed")
	}

	if err := dm.deps.GetClusterState().DeleteAllocation(a.svc.Name, nodeID); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: delete allocation failed")
	}

	dm.bumpMigrated(op, total)
	log.Info().Str("service", a.svc.Name).Str("node_id", nodeID).Msg("system service dismantled")
}
