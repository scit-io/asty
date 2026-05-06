package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DrainStatus tracks the progress of a node drain operation.
type DrainStatus struct {
	NodeID             string   `json:"node_id"`
	Status             string   `json:"status"` // draining, drained, error
	TotalAllocations   int      `json:"total_allocations"`
	Migrated           int      `json:"migrated"`
	Remaining          int      `json:"remaining"`
	CurrentAllocation  string   `json:"current_allocation"`
	Errors             []string `json:"errors"`
}

// DrainManager tracks active drain operations.
type DrainManager struct {
	mu       sync.Mutex
	drains   map[string]*drainOp
	server   *Server
}

type drainOp struct {
	cancel context.CancelFunc
	status DrainStatus
}

func NewDrainManager(server *Server) *DrainManager {
	return &DrainManager{
		drains: make(map[string]*drainOp),
		server: server,
	}
}

// Start initiates a drain on the given node. Returns the initial status.
func (dm *DrainManager) Start(nodeID string) (*DrainStatus, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if op, exists := dm.drains[nodeID]; exists {
		return &op.status, fmt.Errorf("node %s is already draining", nodeID)
	}

	node, err := dm.server.clusterState.GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	if node.Status == "down" {
		return nil, fmt.Errorf("node %s is down", nodeID)
	}
	if node.Status == "drained" {
		return nil, fmt.Errorf("node %s is already drained", nodeID)
	}

	// Count allocations on this node
	var allocs []allocOnNode
	for _, svc := range dm.server.services {
		alloc, err := dm.server.clusterState.GetAllocation(svc.Name, nodeID)
		if err != nil {
			continue
		}
		if alloc.Status == "running" || alloc.Status == "pending" || alloc.Status == "starting" {
			allocs = append(allocs, allocOnNode{svc: svc, alloc: alloc})
		}
	}

	// Mark node as draining
	node.Status = "draining"
	if err := dm.server.clusterState.UpdateNode(node); err != nil {
		return nil, fmt.Errorf("failed to update node status: %w", err)
	}

	status := DrainStatus{
		NodeID:           nodeID,
		Status:           "draining",
		TotalAllocations: len(allocs),
		Migrated:         0,
		Remaining:        len(allocs),
		Errors:           []string{},
	}

	if len(allocs) == 0 {
		// Nothing to drain — mark as drained immediately
		node.Status = "drained"
		_ = dm.server.clusterState.UpdateNode(node)
		status.Status = "drained"
		dm.publishDrainEvent(status)
		return &status, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	op := &drainOp{cancel: cancel, status: status}
	dm.drains[nodeID] = op

	go dm.runDrain(ctx, nodeID, allocs, op)

	log.Info().Str("node_id", nodeID).Int("allocations", len(allocs)).Msg("node drain initiated")
	dm.publishDrainEvent(status)
	return &status, nil
}

// Resume cancels a drain and returns the node to ready.
func (dm *DrainManager) Resume(nodeID string) error {
	dm.mu.Lock()
	op, exists := dm.drains[nodeID]
	if exists {
		op.cancel()
		delete(dm.drains, nodeID)
	}
	dm.mu.Unlock()

	node, err := dm.server.clusterState.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("node not found: %w", err)
	}
	if node.Status != "draining" && node.Status != "drained" {
		return fmt.Errorf("node %s is not draining (status: %s)", nodeID, node.Status)
	}

	node.Status = "ready"
	node.LastSeen = time.Now()
	if err := dm.server.clusterState.UpdateNode(node); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	log.Info().Str("node_id", nodeID).Msg("node drain cancelled, status set to ready")
	return nil
}

// GetStatus returns the drain status for a node.
func (dm *DrainManager) GetStatus(nodeID string) *DrainStatus {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if op, exists := dm.drains[nodeID]; exists {
		s := op.status
		return &s
	}
	return nil
}

type allocOnNode struct {
	svc   *ServiceDefinition
	alloc *ServiceAllocation
}

func (dm *DrainManager) runDrain(ctx context.Context, nodeID string, allocs []allocOnNode, op *drainOp) {
	defer func() {
		dm.mu.Lock()
		delete(dm.drains, nodeID)
		dm.mu.Unlock()
	}()

	// Phase 1: dismantle every system service in parallel — they have no
	// replacement (one-per-node), so no need to wait between them. Gateway,
	// xauth-edge, etc. all stop concurrently. This shaves seconds when several
	// system services live on the node.
	var systemWG sync.WaitGroup
	systemAllocs := []allocOnNode{}
	regularAllocs := []allocOnNode{}
	for _, a := range allocs {
		if a.svc.Type == ServiceTypeSystem {
			systemAllocs = append(systemAllocs, a)
		} else {
			regularAllocs = append(regularAllocs, a)
		}
	}

	for _, a := range systemAllocs {
		systemWG.Add(1)
		go func(a allocOnNode) {
			defer systemWG.Done()
			if err := dm.dismantleSystem(nodeID, a); err != nil {
				dm.mu.Lock()
				op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
				dm.mu.Unlock()
				log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain dismantle failed")
				return
			}
			dm.mu.Lock()
			op.status.Migrated++
			op.status.Remaining = len(allocs) - op.status.Migrated
			snapshot := op.status
			dm.mu.Unlock()
			dm.publishDrainEvent(snapshot)
		}(a)
	}
	systemWG.Wait()

	if ctx.Err() != nil {
		return
	}

	// Phase 2: regular services migrate sequentially with explicit nearest
	// placement. Sequential because each migration briefly runs N+1 copies and
	// we don't want to multiply that by every regular service at once.
	for _, a := range regularAllocs {
		if ctx.Err() != nil {
			return
		}

		dm.mu.Lock()
		op.status.CurrentAllocation = a.alloc.ServiceName
		dm.mu.Unlock()
		dm.publishDrainEvent(op.statusCopy())

		if err := dm.migrateRegular(ctx, nodeID, a); err != nil {
			dm.mu.Lock()
			op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
			dm.mu.Unlock()
			log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain migration failed")
			continue
		}

		dm.mu.Lock()
		op.status.Migrated++
		op.status.Remaining = len(allocs) - op.status.Migrated
		dm.mu.Unlock()
	}

	// Mark node as drained
	node, err := dm.server.clusterState.GetNode(nodeID)
	if err == nil && node.Status == "draining" {
		node.Status = "drained"
		_ = dm.server.clusterState.UpdateNode(node)
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

// statusCopy returns a snapshot of op.status under the existing lock; intended
// for one-shot publish without holding the mutex across NATS publish.
func (op *drainOp) statusCopy() DrainStatus {
	return op.status
}

// dismantleSystem stops a system service on the drained node and removes its
// allocation. System services are one-per-node (Gateway, etc.) — no
// replacement on other nodes, just clean removal.
func (dm *DrainManager) dismantleSystem(nodeID string, a allocOnNode) error {
	svc := a.svc
	if err := dm.server.StopServiceOnNode(nodeID, svc.Name); err != nil {
		// Best effort: still try to delete alloc so a re-elected leader doesn't
		// believe it's still running. Status update happens via agent normally.
		_ = dm.server.clusterState.DeleteAllocation(svc.Name, nodeID)
		return fmt.Errorf("stop failed: %w", err)
	}
	if err := dm.server.clusterState.DeleteAllocation(svc.Name, nodeID); err != nil {
		return fmt.Errorf("delete alloc failed: %w", err)
	}
	log.Info().Str("service", svc.Name).Str("node_id", nodeID).Msg("system service dismantled")
	return nil
}

// migrateRegular places a replacement on the nearest healthy node (by DC
// proximity), waits for it to become healthy, then stops the old process.
// Order matters: replacement-first means service availability is never zero.
func (dm *DrainManager) migrateRegular(ctx context.Context, nodeID string, a allocOnNode) error {
	svc := a.svc

	target := dm.server.scheduler.SelectNearestForReplacement(nodeID, svc)
	if target == nil {
		// No locality-aware target available — fall back to legacy behavior:
		// delete alloc and let the controller's reconcile pick a node by the
		// global geo-spread heuristic. Better than failing the migration.
		log.Warn().
			Str("service", svc.Name).
			Str("node_id", nodeID).
			Msg("drain: no nearest replacement available, falling back to controller placement")
		if err := dm.server.clusterState.DeleteAllocation(svc.Name, nodeID); err != nil {
			return fmt.Errorf("delete allocation failed: %w", err)
		}
		return dm.waitForHealthyReplacement(ctx, nodeID, svc)
	}

	// Create the replacement on the chosen nearby node. Controller's
	// dispatchPending will see the pending alloc and send the start command.
	if err := dm.server.clusterState.CreateAllocation(&ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      target.ID,
		Status:      "pending",
		Version:     a.alloc.Version,
	}); err != nil {
		return fmt.Errorf("create replacement allocation failed: %w", err)
	}

	log.Info().
		Str("service", svc.Name).
		Str("from_node", nodeID).
		Str("to_node", target.ID).
		Str("from_dc", a.alloc.NodeID).
		Str("to_dc", datacenterOf(target)).
		Msg("drain: replacement placed on nearest node")

	if err := dm.waitForHealthyOnNode(ctx, target.ID, svc); err != nil {
		// Replacement didn't come up — leave it for the controller to retry,
		// but still stop the old process so the drain can complete.
		_ = dm.server.StopServiceOnNode(nodeID, svc.Name)
		_ = dm.server.clusterState.DeleteAllocation(svc.Name, nodeID)
		return err
	}

	if err := dm.server.StopServiceOnNode(nodeID, svc.Name); err != nil {
		log.Warn().Err(err).Str("service", svc.Name).Str("node_id", nodeID).Msg("drain: stop old process failed")
	}
	if err := dm.server.clusterState.DeleteAllocation(svc.Name, nodeID); err != nil {
		return fmt.Errorf("delete old allocation failed: %w", err)
	}
	return nil
}

const (
	drainHealthDeadline = 2 * time.Minute
	drainHealthPoll     = 2 * time.Second
)

// waitForHealthyOnNode blocks until the named service has a running+healthy
// allocation on targetNode, or the deadline expires.
func (dm *DrainManager) waitForHealthyOnNode(ctx context.Context, targetNode string, svc *ServiceDefinition) error {
	deadline := time.After(drainHealthDeadline)
	ticker := time.NewTicker(drainHealthPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for healthy replacement on %s", targetNode)
		case <-ticker.C:
			allocs, err := dm.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID != targetNode {
					continue
				}
				if alloc.Status == "running" && alloc.HealthStatus == "healthy" {
					return nil
				}
			}
		}
	}
}

// waitForHealthyReplacement blocks until ANY allocation other than the one on
// drainedNode is healthy. Used in the fallback path where the placement
// decision was deferred to the controller.
func (dm *DrainManager) waitForHealthyReplacement(ctx context.Context, drainedNode string, svc *ServiceDefinition) error {
	deadline := time.After(drainHealthDeadline)
	ticker := time.NewTicker(drainHealthPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			_ = dm.server.StopServiceOnNode(drainedNode, svc.Name)
			return fmt.Errorf("timeout waiting for healthy replacement")
		case <-ticker.C:
			allocs, err := dm.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == drainedNode {
					continue
				}
				if alloc.Status == "running" && alloc.HealthStatus == "healthy" {
					_ = dm.server.StopServiceOnNode(drainedNode, svc.Name)
					return nil
				}
			}
		}
	}
}

// publishDrainEvent sends a drain_progress event via NATS for SSE consumers.
func (dm *DrainManager) publishDrainEvent(status DrainStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = dm.server.nc.Publish("asty.v1.drain.progress", data)
}
