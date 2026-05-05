package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// ClusterState manages cluster state in NATS JetStream KV
type ClusterState struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	bucket nats.KeyValue
}

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	ID         string    `json:"id"`
	Datacenter string    `json:"datacenter"`
	IP         string    `json:"ip"`
	Status     string    `json:"status"` // ready, draining, down
	CreatedAt  time.Time `json:"created_at"`
	LastSeen   time.Time `json:"last_seen"`

	// Resources
	CPUTotal      int   `json:"cpu_total"`       // MHz
	CPUAvailable  int   `json:"cpu_available"`
	MemoryTotal   int64 `json:"memory_total"`    // MB
	MemoryAvailable int64 `json:"memory_available"`

	// Processes
	Processes []string `json:"processes"` // list of service names

	// Allocations counters (computed, not persisted in KV)
	AllocationsRunning int `json:"allocations_running"` // Number of running allocations
	AllocationsPlanned int `json:"allocations_planned"` // Total number of planned allocations
}

// ServiceAllocation represents a service instance placement
type ServiceAllocation struct {
	ID          string    `json:"id"`          // Unique allocation ID
	ServiceName string    `json:"service_name"`
	NodeID      string    `json:"node_id"`
	Status      string    `json:"status"` // pending, running, stopped, failed
	Version     string    `json:"version"`
	PID         int       `json:"pid"`           // Process ID (if running)
	StartedAt   time.Time `json:"started_at"`    // When process started
	HealthStatus string   `json:"health_status"` // healthy, unhealthy, unknown
	CPUUsage    int       `json:"cpu_usage"`     // Percentage
	MemoryUsage int       `json:"memory_usage"`  // MB
	Restarts             int       `json:"restarts"`              // Total cumulative restarts
	ConsecutiveFailures  int       `json:"consecutive_failures"`  // Resets on stable run
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewClusterState creates a new cluster state manager
func NewClusterState(nc *nats.Conn) (*ClusterState, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Create or get KV bucket for cluster state
	bucket, err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:      "asty-cluster",
		Description: "Asty cluster state",
		History:     10,
	})
	if err != nil {
		// Try to get existing bucket
		bucket, err = js.KeyValue("asty-cluster")
		if err != nil {
			return nil, fmt.Errorf("failed to create/get KV bucket: %w", err)
		}
	}

	log.Info().Msg("cluster state initialized")

	return &ClusterState{
		nc:     nc,
		js:     js,
		bucket: bucket,
	}, nil
}

// UpdateNode updates node information in cluster state
func (cs *ClusterState) UpdateNode(node *NodeInfo) error {
	now := time.Now()
	node.LastSeen = now

	key := fmt.Sprintf("node.%s", node.ID)

	// Try to get existing node to preserve CreatedAt
	if node.CreatedAt.IsZero() {
		if existing, err := cs.bucket.Get(key); err == nil {
			var existingNode NodeInfo
			if json.Unmarshal(existing.Value(), &existingNode) == nil && !existingNode.CreatedAt.IsZero() {
				node.CreatedAt = existingNode.CreatedAt
			}
		}
		// If still zero (node doesn't exist or error), set to now
		if node.CreatedAt.IsZero() {
			node.CreatedAt = now
		}
	}

	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}

	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("failed to put node info: %w", err)
	}

	return nil
}

// GetNode retrieves node information from cluster state
func (cs *ClusterState) GetNode(nodeID string) (*NodeInfo, error) {
	key := fmt.Sprintf("node.%s", nodeID)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, fmt.Errorf("node %s not found", nodeID)
		}
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	var node NodeInfo
	if err := json.Unmarshal(entry.Value(), &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node info: %w", err)
	}

	return &node, nil
}

// ListNodes returns all nodes in the cluster
func (cs *ClusterState) ListNodes() ([]*NodeInfo, error) {
	keys, err := cs.bucket.Keys()
	if err != nil {
		// If no keys found, return empty list instead of error
		if err == nats.ErrNoKeysFound {
			return []*NodeInfo{}, nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	nodes := make([]*NodeInfo, 0)
	for _, key := range keys {
		// Only process node keys
		if len(key) < 5 || key[:5] != "node." {
			continue
		}

		entry, err := cs.bucket.Get(key)
		if err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to get node entry")
			continue
		}

		var node NodeInfo
		if err := json.Unmarshal(entry.Value(), &node); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to unmarshal node")
			continue
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// RemoveNode removes a node from cluster state
func (cs *ClusterState) RemoveNode(nodeID string) error {
	key := fmt.Sprintf("node.%s", nodeID)
	if err := cs.bucket.Delete(key); err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}

	log.Info().Str("node_id", nodeID).Msg("node removed from cluster state")
	return nil
}

// CreateAllocation creates a service allocation record
func (cs *ClusterState) CreateAllocation(alloc *ServiceAllocation) error {
	// Generate ID if not set
	if alloc.ID == "" {
		alloc.ID = fmt.Sprintf("%s-%s-%d", alloc.ServiceName, alloc.NodeID, time.Now().UnixNano())
	}

	alloc.CreatedAt = time.Now()
	alloc.UpdatedAt = time.Now()

	data, err := json.Marshal(alloc)
	if err != nil {
		return fmt.Errorf("failed to marshal allocation: %w", err)
	}

	key := fmt.Sprintf("alloc.%s.%s", alloc.ServiceName, alloc.NodeID)
	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("failed to put allocation: %w", err)
	}

	return nil
}

// GetAllocation retrieves a service allocation
func (cs *ClusterState) GetAllocation(serviceName, nodeID string) (*ServiceAllocation, error) {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, fmt.Errorf("allocation not found")
		}
		return nil, fmt.Errorf("failed to get allocation: %w", err)
	}

	var alloc ServiceAllocation
	if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal allocation: %w", err)
	}

	return &alloc, nil
}

// ListAllocations returns all allocations for a service
func (cs *ClusterState) ListAllocations(serviceName string) ([]*ServiceAllocation, error) {
	keys, err := cs.bucket.Keys()
	if err != nil {
		if err == nats.ErrNoKeysFound {
			return []*ServiceAllocation{}, nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	prefix := fmt.Sprintf("alloc.%s.", serviceName)
	allocs := make([]*ServiceAllocation, 0)

	for _, key := range keys {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			continue
		}

		entry, err := cs.bucket.Get(key)
		if err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to get allocation entry")
			continue
		}

		var alloc ServiceAllocation
		if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to unmarshal allocation")
			continue
		}

		allocs = append(allocs, &alloc)
	}

	return allocs, nil
}

// UpdateAllocation updates an allocation status
func (cs *ClusterState) UpdateAllocation(alloc *ServiceAllocation) error {
	alloc.UpdatedAt = time.Now()

	data, err := json.Marshal(alloc)
	if err != nil {
		return fmt.Errorf("failed to marshal allocation: %w", err)
	}

	key := fmt.Sprintf("alloc.%s.%s", alloc.ServiceName, alloc.NodeID)
	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("failed to update allocation: %w", err)
	}

	return nil
}

// DeleteAllocation removes an allocation
func (cs *ClusterState) DeleteAllocation(serviceName, nodeID string) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	if err := cs.bucket.Delete(key); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}

	return nil
}

// WatchNodes watches for node changes
func (cs *ClusterState) WatchNodes(ctx context.Context, onChange func(*NodeInfo)) error {
	watcher, err := cs.bucket.Watch("node.*")
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry := <-watcher.Updates():
			if entry == nil {
				continue
			}

			// Handle delete
			if entry.Operation() == nats.KeyValueDelete {
				log.Info().Str("key", entry.Key()).Msg("node deleted")
				continue
			}

			// Parse node info
			var node NodeInfo
			if err := json.Unmarshal(entry.Value(), &node); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal node")
				continue
			}

			onChange(&node)
		}
	}
}
