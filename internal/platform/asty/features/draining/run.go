package draining

import (
	"context"
	"sync"

	"asty/internal/platform/asty/core/types"

	"github.com/rs/zerolog/log"
)

// runDrain is the orchestrator: split allocs into system (one-per-node,
// have to dismantle in place) and regular (migrate to a peer first),
// drive each in parallel, then mark the node drained when both finish.
func (dm *DrainManager) runDrain(ctx context.Context, nodeID string, allocs []allocOnNode, op *drainOp) {
	defer func() {
		dm.mu.Lock()
		delete(dm.drains, nodeID)
		dm.mu.Unlock()
	}()

	systemAllocs, regularAllocs := splitByType(allocs)
	total := len(allocs)

	var wg sync.WaitGroup

	// System services run one-per-node — there is no peer to migrate
	// to. Just stop and forget.
	for _, a := range systemAllocs {
		wg.Add(1)
		go func(a allocOnNode) {
			defer wg.Done()
			dm.dismantleAndConfirm(ctx, nodeID, a, op, total)
		}(a)
	}

	// Regular services need a healthy replacement before we can stop
	// the local copy.
	for _, a := range regularAllocs {
		if ctx.Err() != nil {
			break
		}
		dm.markCurrent(op, a.alloc.ServiceName)

		fellBack, err := dm.placeReplacement(ctx, nodeID, a)
		if err != nil {
			dm.recordError(op, a.svc.Name, err)
			log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain replacement failed")
			continue
		}

		wg.Add(1)
		go func(a allocOnNode, oldDeleted bool) {
			defer wg.Done()
			dm.finalizeMigration(ctx, nodeID, a, oldDeleted, op, total)
		}(a, fellBack)
	}

	wg.Wait()

	if ctx.Err() != nil {
		return
	}

	dm.completeNodeDrain(nodeID, op)
}

func splitByType(allocs []allocOnNode) (system, regular []allocOnNode) {
	for _, a := range allocs {
		if a.svc.Type == types.ServiceTypeSystem {
			system = append(system, a)
		} else {
			regular = append(regular, a)
		}
	}
	return system, regular
}

// markCurrent records the alloc currently being processed so the API
// can show "now migrating xhttp …".
func (dm *DrainManager) markCurrent(op *drainOp, serviceName string) {
	dm.mu.Lock()
	op.status.CurrentAllocation = serviceName
	dm.mu.Unlock()
	dm.publishDrainEvent(op.statusCopy())
}

// recordError appends a per-allocation error message to the drain
// status (operators see the full set in the API response).
func (dm *DrainManager) recordError(op *drainOp, name string, err error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	op.status.Errors = append(op.status.Errors, name+": "+err.Error())
}

// completeNodeDrain runs once every allocation has been handled (either
// migrated or dismantled). It flips the node status to "drained" and
// publishes the final progress event.
func (dm *DrainManager) completeNodeDrain(nodeID string, op *drainOp) {
	if node, err := dm.deps.GetClusterState().GetNode(nodeID); err == nil && node.Status == "draining" {
		node.Status = "drained"
		_ = dm.deps.GetClusterState().UpdateNode(node)
	}

	dm.mu.Lock()
	op.status.Status = "drained"
	op.status.CurrentAllocation = ""
	op.status.Remaining = 0
	finalStatus := op.status
	dm.mu.Unlock()

	dm.publishDrainEvent(finalStatus)
	log.Info().Str("node_id", nodeID).Int("migrated", finalStatus.Migrated).Msg("node drain complete")
}

// bumpMigrated increments the migrated counter and publishes a
// progress event. Used by both the system and regular paths.
func (dm *DrainManager) bumpMigrated(op *drainOp, total int) {
	dm.mu.Lock()
	op.status.Migrated++
	op.status.Remaining = total - op.status.Migrated
	snapshot := op.status
	dm.mu.Unlock()
	dm.publishDrainEvent(snapshot)
}
