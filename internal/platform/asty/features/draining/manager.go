package draining

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// DrainStatus tracks the progress of a node drain operation.
type DrainStatus struct {
	NodeID            string   `json:"node_id"`
	Status            string   `json:"status"` // draining, drained, error
	TotalAllocations  int      `json:"total_allocations"`
	Migrated          int      `json:"migrated"`
	Remaining         int      `json:"remaining"`
	CurrentAllocation string   `json:"current_allocation"`
	Errors            []string `json:"errors"`
}

// DrainDeps provides access to server resources without importing the server package.
type DrainDeps interface {
	GetClusterState() *state.ClusterState
	GetScheduler() *scheduling.Scheduler
	GetServices() []*types.ServiceDefinition
	GetNATSConn() *nats.Conn
	StopServiceOnNode(nodeID, serviceName string) error
}

// DrainManager tracks active drain operations.
type DrainManager struct {
	mu     sync.Mutex
	drains map[string]*drainOp
	deps   DrainDeps
}

type drainOp struct {
	cancel context.CancelFunc
	status DrainStatus
}

func NewDrainManager(deps DrainDeps) *DrainManager {
	return &DrainManager{
		drains: make(map[string]*drainOp),
		deps:   deps,
	}
}

// Start initiates a drain on the given node. Returns the initial status.
func (dm *DrainManager) Start(nodeID string) (*DrainStatus, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if op, exists := dm.drains[nodeID]; exists {
		return &op.status, fmt.Errorf("node %s is already draining", nodeID)
	}

	node, err := dm.deps.GetClusterState().GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	if node.Status == "down" {
		return nil, fmt.Errorf("node %s is down", nodeID)
	}
	if node.Status == "drained" {
		return nil, fmt.Errorf("node %s is already drained", nodeID)
	}

	var allocs []allocOnNode
	for _, svc := range dm.deps.GetServices() {
		alloc, err := dm.deps.GetClusterState().GetAllocation(svc.Name, nodeID)
		if err != nil {
			continue
		}
		if alloc.Status == "running" || alloc.Status == "pending" || alloc.Status == "starting" {
			allocs = append(allocs, allocOnNode{svc: svc, alloc: alloc})
		}
	}

	node.Status = "draining"
	if err := dm.deps.GetClusterState().UpdateNode(node); err != nil {
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
		node.Status = "drained"
		_ = dm.deps.GetClusterState().UpdateNode(node)
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

	node, err := dm.deps.GetClusterState().GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("node not found: %w", err)
	}
	if node.Status != "draining" && node.Status != "drained" {
		return fmt.Errorf("node %s is not draining (status: %s)", nodeID, node.Status)
	}

	node.Status = "ready"
	node.LastSeen = time.Now()
	if err := dm.deps.GetClusterState().UpdateNode(node); err != nil {
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
	svc   *types.ServiceDefinition
	alloc *types.ServiceAllocation
}

func (dm *DrainManager) runDrain(ctx context.Context, nodeID string, allocs []allocOnNode, op *drainOp) {
	defer func() {
		dm.mu.Lock()
		delete(dm.drains, nodeID)
		dm.mu.Unlock()
	}()

	var systemAllocs, regularAllocs []allocOnNode
	for _, a := range allocs {
		if a.svc.Type == types.ServiceTypeSystem {
			systemAllocs = append(systemAllocs, a)
		} else {
			regularAllocs = append(regularAllocs, a)
		}
	}

	total := len(allocs)
	var stopWG sync.WaitGroup

	for _, a := range systemAllocs {
		stopWG.Add(1)
		go func(a allocOnNode) {
			defer stopWG.Done()
			dm.dismantleAndConfirm(ctx, nodeID, a, op, total)
		}(a)
	}

	for _, a := range regularAllocs {
		if ctx.Err() != nil {
			break
		}

		dm.mu.Lock()
		op.status.CurrentAllocation = a.alloc.ServiceName
		dm.mu.Unlock()
		dm.publishDrainEvent(op.statusCopy())

		fellBack, err := dm.placeReplacement(ctx, nodeID, a)
		if err != nil {
			dm.mu.Lock()
			op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
			dm.mu.Unlock()
			log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain replacement failed")
			continue
		}

		stopWG.Add(1)
		go func(a allocOnNode, oldAllocAlreadyDeleted bool) {
			defer stopWG.Done()
			dm.finalizeMigration(ctx, nodeID, a, oldAllocAlreadyDeleted, op, total)
		}(a, fellBack)
	}

	stopWG.Wait()

	if ctx.Err() != nil {
		return
	}

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

func (op *drainOp) statusCopy() DrainStatus {
	return op.status
}

func (dm *DrainManager) dismantleAndConfirm(ctx context.Context, nodeID string, a allocOnNode, op *drainOp, total int) {
	if err := dm.deps.StopServiceOnNode(nodeID, a.svc.Name); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop dispatch failed")
	}

	if err := dm.waitForStopped(ctx, nodeID, a.svc); err != nil {
		dm.mu.Lock()
		op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
		dm.mu.Unlock()
		log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop confirmation failed")
	}

	if err := dm.deps.GetClusterState().DeleteAllocation(a.svc.Name, nodeID); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: delete allocation failed")
	}

	dm.bumpMigrated(op, total)
	log.Info().Str("service", a.svc.Name).Str("node_id", nodeID).Msg("system service dismantled")
}

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
		Status:      "pending",
		Version:     a.alloc.Version,
	}); err != nil {
		return false, fmt.Errorf("create replacement allocation failed: %w", err)
	}

	log.Info().
		Str("service", a.svc.Name).
		Str("from_node", nodeID).
		Str("to_node", target.ID).
		Str("to_dc", scheduling.DatacenterOf(target)).
		Msg("drain: replacement placed on nearest node")

	if err := dm.waitForHealthyOnNode(ctx, target.ID, a.svc); err != nil {
		return false, err
	}
	return false, nil
}

func (dm *DrainManager) finalizeMigration(ctx context.Context, nodeID string, a allocOnNode, oldAllocAlreadyDeleted bool, op *drainOp, total int) {
	if err := dm.deps.StopServiceOnNode(nodeID, a.svc.Name); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop dispatch failed")
	}

	dm.bumpMigrated(op, total)

	if oldAllocAlreadyDeleted {
		return
	}

	if err := dm.waitForStopped(ctx, nodeID, a.svc); err != nil {
		dm.mu.Lock()
		op.status.Errors = append(op.status.Errors, fmt.Sprintf("%s: %s", a.svc.Name, err.Error()))
		dm.mu.Unlock()
		log.Error().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: stop confirmation failed")
	}

	if err := dm.deps.GetClusterState().DeleteAllocation(a.svc.Name, nodeID); err != nil {
		log.Warn().Err(err).Str("service", a.svc.Name).Str("node_id", nodeID).Msg("drain: delete allocation failed")
	}
}

func (dm *DrainManager) bumpMigrated(op *drainOp, total int) {
	dm.mu.Lock()
	op.status.Migrated++
	op.status.Remaining = total - op.status.Migrated
	snapshot := op.status
	dm.mu.Unlock()
	dm.publishDrainEvent(snapshot)
}

const (
	drainHealthDeadline = 2 * time.Minute
	drainHealthPoll     = 200 * time.Millisecond
	drainStopMinSlack   = 10 * time.Second
)

func (dm *DrainManager) waitForStopped(ctx context.Context, nodeID string, svc *types.ServiceDefinition) error {
	dctx, cancel := context.WithTimeout(ctx, svc.GetKillTimeout()+drainStopMinSlack)
	defer cancel()

	err := dm.deps.GetClusterState().WatchAllocation(dctx, svc.Name, nodeID, func(alloc *types.ServiceAllocation) bool {
		if alloc == nil {
			return true
		}
		return alloc.Status == "stopped" || alloc.Status == "failed"
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if dctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout waiting for %s to stop on %s", svc.Name, nodeID)
	}
	return nil
}

func (dm *DrainManager) waitForHealthyOnNode(ctx context.Context, targetNode string, svc *types.ServiceDefinition) error {
	dctx, cancel := context.WithTimeout(ctx, drainHealthDeadline)
	defer cancel()

	err := dm.deps.GetClusterState().WatchAllocation(dctx, svc.Name, targetNode, func(alloc *types.ServiceAllocation) bool {
		return alloc != nil && alloc.Status == "running"
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if dctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout waiting for healthy replacement on %s", targetNode)
	}
	return nil
}

func (dm *DrainManager) waitForHealthyReplacement(ctx context.Context, drainedNode string, svc *types.ServiceDefinition) error {
	deadline := time.After(drainHealthDeadline)
	ticker := time.NewTicker(drainHealthPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for healthy replacement")
		case <-ticker.C:
			allocs, err := dm.deps.GetClusterState().ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == drainedNode {
					continue
				}
				if alloc.Status == "running" {
					return nil
				}
			}
		}
	}
}

func (dm *DrainManager) publishDrainEvent(status DrainStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = dm.deps.GetNATSConn().Publish("asty.v1.drain.progress", data)
}
