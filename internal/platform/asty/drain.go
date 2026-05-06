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

	for i, a := range allocs {
		if ctx.Err() != nil {
			return // drain was cancelled (resume)
		}

		dm.mu.Lock()
		op.status.CurrentAllocation = a.alloc.ServiceName
		op.status.Remaining = len(allocs) - i
		status := op.status
		dm.mu.Unlock()
		dm.publishDrainEvent(status)

		if err := dm.migrateAllocation(ctx, nodeID, a); err != nil {
			dm.mu.Lock()
			op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
			dm.mu.Unlock()
			log.Error().Err(err).
				Str("service", a.svc.Name).
				Str("node_id", nodeID).
				Msg("drain migration failed")
			continue
		}

		dm.mu.Lock()
		op.status.Migrated++
		op.status.Remaining = len(allocs) - i - 1
		dm.mu.Unlock()

		// Pause between migrations
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}

	// All done — mark node as drained
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

// migrateAllocation: delete old alloc so controller creates a new one on a
// healthy node, wait for the new one to become healthy, then stop the old process.
func (dm *DrainManager) migrateAllocation(ctx context.Context, nodeID string, a allocOnNode) error {
	svc := a.svc

	// For system services, just stop — no replacement on other nodes
	if svc.Type == ServiceTypeSystem {
		if err := dm.server.StopServiceOnNode(nodeID, svc.Name); err != nil {
			return fmt.Errorf("stop failed: %w", err)
		}
		_ = dm.server.clusterState.DeleteAllocation(svc.Name, nodeID)
		return nil
	}

	// Delete the allocation — controller will create a replacement
	if err := dm.server.clusterState.DeleteAllocation(svc.Name, nodeID); err != nil {
		return fmt.Errorf("delete allocation failed: %w", err)
	}

	// Wait for a new healthy allocation to appear (timeout 2 min)
	deadline := time.After(2 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			// Timed out waiting — still stop old, best effort
			_ = dm.server.StopServiceOnNode(nodeID, svc.Name)
			return fmt.Errorf("timeout waiting for healthy replacement")
		case <-ticker.C:
			allocs, err := dm.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == nodeID {
					continue
				}
				if alloc.Status == "running" && alloc.HealthStatus == "healthy" {
					// Replacement is healthy — stop old process
					_ = dm.server.StopServiceOnNode(nodeID, svc.Name)
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
