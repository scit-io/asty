package drainer

import (
	"context"
	"sync"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// maxConcurrentMigrations bounds the number of regular-allocation
// migrations executed in parallel during a drain. Without a cap a
// busy node could fan out dozens of placeReplacement+finalize
// goroutines simultaneously, swamping NATS RPC and the scheduler.
// 4 is conservative; tune via field on DrainManager if dev loads grow.
const maxConcurrentMigrations = 4

// runDrain is the orchestrator: split allocs into system (one-per-node,
// have to dismantle in place) and regular (migrate to a peer first),
// drive both groups in parallel, then mark the node drained when
// everything finishes.
//
// Regular migrations run with a semaphore of size maxConcurrentMigrations
// — within that budget, multiple replacements can be placed and
// finalised concurrently. Previously this loop processed regular
// allocations sequentially, which scaled drain time linearly with
// allocation count.
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
	// to. Just stop and forget. Parallel by construction (one goroutine
	// per alloc); no NATS RPC fan-out concern because there is no
	// scheduler involvement.
	for _, a := range systemAllocs {
		wg.Add(1)
		go func(a allocOnNode) {
			defer wg.Done()
			dm.dismantleAndConfirm(ctx, nodeID, a, op, total)
		}(a)
	}

	// Regular services need a healthy replacement before we can stop
	// the local copy. Semaphore caps concurrency so we don't drown
	// NATS or the scheduler.
	sem := make(chan struct{}, maxConcurrentMigrations)
	for _, a := range regularAllocs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(a allocOnNode) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			dm.markCurrent(op, a.alloc.ServiceName)

			fellBack, err := dm.placeReplacement(ctx, nodeID, a)
			if err != nil {
				dm.recordError(op, a.svc.Name, err)
				log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain replacement failed")
				return
			}
			dm.finalizeMigration(ctx, nodeID, a, fellBack, op, total)
		}(a)
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
// can show "now migrating <service> …".
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
	if node, err := dm.deps.GetClusterState().GetNode(nodeID); err == nil && node.Status == types.NodeDraining {
		node.Status = types.NodeDrained
		_ = dm.deps.GetClusterState().UpdateNode(node)
	}

	dm.mu.Lock()
	op.status.Status = string(types.NodeDrained)
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
