package drainer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/scheduler"

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

// DrainDeps provides access to server resources without importing the
// server package (which would create a cycle: server → draining → server).
type DrainDeps interface {
	GetClusterState() *kv.ClusterState
	GetScheduler() *scheduler.Scheduler
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

func (op *drainOp) statusCopy() DrainStatus {
	return op.status
}

type allocOnNode struct {
	svc   *types.ServiceDefinition
	alloc *types.ServiceAllocation
}

// NewDrainManager creates a drain manager. deps is typically the Server
// itself; the interface lets tests inject fakes.
func NewDrainManager(deps DrainDeps) *DrainManager {
	return &DrainManager{
		drains: make(map[string]*drainOp),
		deps:   deps,
	}
}

// Start initiates a drain on the given node. Returns the initial
// status, or an error if the node is unknown / already drained / down.
// Once running, the drain progresses in a background goroutine; clients
// can poll GetStatus or watch the asty.v1.drain.progress NATS subject.
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
	if node.Status == types.NodeDown {
		return nil, fmt.Errorf("node %s is down", nodeID)
	}
	if node.Status == types.NodeDrained {
		return nil, fmt.Errorf("node %s is already drained", nodeID)
	}

	allocs := dm.collectAllocs(nodeID)

	node.Status = types.NodeDraining
	if err := dm.deps.GetClusterState().UpdateNode(node); err != nil {
		return nil, fmt.Errorf("failed to update node status: %w", err)
	}

	status := DrainStatus{
		NodeID:           nodeID,
		Status:           string(types.NodeDraining),
		TotalAllocations: len(allocs),
		Migrated:         0,
		Remaining:        len(allocs),
		Errors:           []string{},
	}

	if len(allocs) == 0 {
		node.Status = types.NodeDrained
		_ = dm.deps.GetClusterState().UpdateNode(node)
		status.Status = string(types.NodeDrained)
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
	if node.Status != types.NodeDraining && node.Status != types.NodeDrained {
		return fmt.Errorf("node %s is not draining (status: %s)", nodeID, node.Status)
	}

	node.Status = types.NodeReady
	node.LastSeen = time.Now()
	if err := dm.deps.GetClusterState().UpdateNode(node); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}
	log.Info().Str("node_id", nodeID).Msg("node drain cancelled, status set to ready")
	return nil
}

// GetStatus returns the drain status for a node, or nil if not draining.
func (dm *DrainManager) GetStatus(nodeID string) *DrainStatus {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if op, exists := dm.drains[nodeID]; exists {
		s := op.status
		return &s
	}
	return nil
}

// collectAllocs gathers every running/pending/starting allocation
// currently bound to nodeID. Stopped/failed ones are ignored — they're
// not contributing traffic and don't need migration. One Watch-based
// snapshot beats N round-trips when the service list is long.
func (dm *DrainManager) collectAllocs(nodeID string) []allocOnNode {
	svcByName := make(map[string]*types.ServiceDefinition, len(dm.deps.GetServices()))
	for _, svc := range dm.deps.GetServices() {
		svcByName[svc.Name] = svc
	}
	all, err := dm.deps.GetClusterState().ListAllAllocations()
	if err != nil {
		return nil
	}
	var allocs []allocOnNode
	for _, a := range all {
		if a.NodeID != nodeID || !a.Status.IsLive() {
			continue
		}
		svc, ok := svcByName[a.ServiceName]
		if !ok {
			continue
		}
		allocs = append(allocs, allocOnNode{svc: svc, alloc: a})
	}
	return allocs
}
