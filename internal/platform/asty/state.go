package asty

import (
	"context"
	"encoding/json"
	"errors"
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

	// Retry KV bucket creation — JetStream meta-group may still be electing leader
	var bucket nats.KeyValue
	for attempt := 0; attempt < 30; attempt++ {
		bucket, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      "asty-cluster",
			Description: "Asty cluster state",
			History:     10,
		})
		if err == nil {
			break
		}
		bucket, err = js.KeyValue("asty-cluster")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create/get KV bucket after retries: %w", err)
	}

	// Wait until bucket is operational (stream raft-group has leader)
	for attempt := 0; attempt < 30; attempt++ {
		if _, err := bucket.Keys(); err == nats.ErrNoKeysFound {
			break
		} else if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
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

// ListAllAllocations returns every allocation record in the bucket regardless
// of service. Used by the scheduler to compute per-node "packing" pressure —
// concentrating new placements on nodes that already host other services
// instead of spreading every service across the whole cluster.
func (cs *ClusterState) ListAllAllocations() ([]*ServiceAllocation, error) {
	keys, err := cs.bucket.Keys()
	if err != nil {
		if err == nats.ErrNoKeysFound {
			return []*ServiceAllocation{}, nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	const prefix = "alloc."
	allocs := make([]*ServiceAllocation, 0, len(keys))
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

// UpdateAllocation overwrites the allocation record. Last write wins — use
// MutateAllocation when concurrent writers (leader and agent) might be
// touching the same record, which is essentially everywhere.
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

// allocationMutateMaxRetries caps optimistic-concurrency retries before
// giving up on a mutation. Eight attempts gives ~10ms of competing writers
// time to settle in practice.
const allocationMutateMaxRetries = 8

// MutateAllocation atomically applies fn to the allocation for (serviceName,
// nodeID) and commits the result. Uses NATS KV's revision-based Update so
// concurrent writers cannot overwrite each other's changes — on conflict the
// newest stored value is re-read and fn re-applied. fn must be idempotent.
//
// fn returns the revised alloc (or modifies in place; the same pointer is
// committed) and a boolean indicating whether the change is still relevant.
// Returning false skips the write and reports nil — useful for predicate-
// guarded mutations like "transition only if status == pending".
func (cs *ClusterState) MutateAllocation(serviceName, nodeID string, fn func(*ServiceAllocation) bool) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	for attempt := 0; attempt < allocationMutateMaxRetries; attempt++ {
		entry, err := cs.bucket.Get(key)
		if err != nil {
			if err == nats.ErrKeyNotFound {
				return fmt.Errorf("allocation not found: %s/%s", serviceName, nodeID)
			}
			return fmt.Errorf("failed to get allocation: %w", err)
		}

		var alloc ServiceAllocation
		if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
			return fmt.Errorf("failed to unmarshal allocation: %w", err)
		}
		if !fn(&alloc) {
			return nil
		}
		alloc.UpdatedAt = time.Now()

		data, err := json.Marshal(&alloc)
		if err != nil {
			return fmt.Errorf("failed to marshal allocation: %w", err)
		}
		if _, err := cs.bucket.Update(key, data, entry.Revision()); err != nil {
			if isCASConflict(err) {
				continue
			}
			return fmt.Errorf("failed to update allocation: %w", err)
		}
		return nil
	}
	return fmt.Errorf("update conflict after %d retries: %s/%s", allocationMutateMaxRetries, serviceName, nodeID)
}

// isCASConflict reports whether err came from a revision mismatch on a CAS
// Update. NATS encodes this as JSErrCodeStreamWrongLastSequence — the same
// code used for "key already exists" on Create.
func isCASConflict(err error) bool {
	var apiErr *nats.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == nats.JSErrCodeStreamWrongLastSequence
	}
	return false
}

// DeleteAllocation removes an allocation
func (cs *ClusterState) DeleteAllocation(serviceName, nodeID string) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	if err := cs.bucket.Delete(key); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}

	return nil
}

// WatchNodes invokes onChange once per node KV update or delete (delete is
// signalled with a non-nil NodeInfo whose Status is "deleted"). Replays the
// existing key-set on subscribe — callers should treat that as a baseline
// reconciliation cue, not as live updates. Blocks until ctx is cancelled.
func (cs *ClusterState) WatchNodes(ctx context.Context, onChange func(*NodeInfo)) error {
	watcher, err := cs.bucket.Watch("node.*", nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				// End-of-history marker from JetStream; not an error.
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				// Surface deletes too — caller decides whether to reconcile.
				onChange(&NodeInfo{ID: keySuffix(entry.Key(), "node."), Status: "deleted"})
				continue
			}
			var node NodeInfo
			if err := json.Unmarshal(entry.Value(), &node); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal node")
				continue
			}
			onChange(&node)
		}
	}
}

// WatchAllocations invokes onChange once per allocation KV update or delete.
// Mirrors WatchNodes: deletes arrive as ServiceAllocation with Status=deleted
// so callers can react without a separate channel. Blocks until ctx is done.
func (cs *ClusterState) WatchAllocations(ctx context.Context, onChange func(*ServiceAllocation)) error {
	watcher, err := cs.bucket.Watch("alloc.*", nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to create alloc watcher: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				svc, node := splitAllocKey(entry.Key())
				onChange(&ServiceAllocation{ServiceName: svc, NodeID: node, Status: "deleted"})
				continue
			}
			var alloc ServiceAllocation
			if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal allocation")
				continue
			}
			onChange(&alloc)
		}
	}
}

// WatchAllocation watches a single allocation key and calls fn on every KV
// update. fn receives nil when the key is deleted or purged. fn returning true
// signals "done" — the watcher stops and WatchAllocation returns nil.
// Returns when ctx is cancelled, the channel closes, or fn returns true.
func (cs *ClusterState) WatchAllocation(ctx context.Context, serviceName, nodeID string, fn func(*ServiceAllocation) bool) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	watcher, err := cs.bucket.Watch(key, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("watch allocation: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				// End of initial-values snapshot — continue to live updates.
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				if fn(nil) {
					return nil
				}
				continue
			}
			var alloc ServiceAllocation
			if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
				continue
			}
			if fn(&alloc) {
				return nil
			}
		}
	}
}

// keySuffix returns the part of key after prefix, or empty if key doesn't
// start with prefix.
func keySuffix(key, prefix string) string {
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return ""
	}
	return key[len(prefix):]
}

// splitAllocKey decomposes "alloc.<service>.<node>" into (service, node).
// Service names may not contain dots; node IDs may.
func splitAllocKey(key string) (string, string) {
	const prefix = "alloc."
	rest := keySuffix(key, prefix)
	for i := 0; i < len(rest); i++ {
		if rest[i] == '.' {
			return rest[:i], rest[i+1:]
		}
	}
	return rest, ""
}

// ServiceCooldown captures the timestamps of the most recent autoscaler
// actions for a service. Stored in KV so leader flips don't reset the
// cooldown window — a freshly elected leader sees the same history.
type ServiceCooldown struct {
	LastScaleUp   time.Time `json:"last_scale_up,omitempty"`
	LastScaleDown time.Time `json:"last_scale_down,omitempty"`
}

const serviceCooldownKey = "service.%s.cooldown"

// GetServiceCooldown returns the cooldown record for a service, or a zero
// value (and no error) when none has been recorded yet.
func (cs *ClusterState) GetServiceCooldown(service string) (ServiceCooldown, error) {
	key := fmt.Sprintf(serviceCooldownKey, service)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return ServiceCooldown{}, nil
		}
		return ServiceCooldown{}, fmt.Errorf("failed to get cooldown: %w", err)
	}
	var c ServiceCooldown
	if err := json.Unmarshal(entry.Value(), &c); err != nil {
		return ServiceCooldown{}, fmt.Errorf("failed to unmarshal cooldown: %w", err)
	}
	return c, nil
}

// MarkScaleUp persists "scale-up just happened" so the cooldown window
// covers leader transitions. Read-modify-write under CAS to coexist with
// MarkScaleDown writes from a different code path.
func (cs *ClusterState) MarkScaleUp(service string, when time.Time) error {
	return cs.mutateCooldown(service, func(c *ServiceCooldown) { c.LastScaleUp = when })
}

// MarkScaleDown is the symmetric write for shrink events.
func (cs *ClusterState) MarkScaleDown(service string, when time.Time) error {
	return cs.mutateCooldown(service, func(c *ServiceCooldown) { c.LastScaleDown = when })
}

func (cs *ClusterState) mutateCooldown(service string, fn func(*ServiceCooldown)) error {
	key := fmt.Sprintf(serviceCooldownKey, service)
	for attempt := 0; attempt < allocationMutateMaxRetries; attempt++ {
		entry, err := cs.bucket.Get(key)
		var (
			rev uint64
			c   ServiceCooldown
		)
		if err == nil {
			rev = entry.Revision()
			if err := json.Unmarshal(entry.Value(), &c); err != nil {
				return fmt.Errorf("unmarshal cooldown: %w", err)
			}
		} else if err != nats.ErrKeyNotFound {
			return fmt.Errorf("get cooldown: %w", err)
		}
		fn(&c)
		data, err := json.Marshal(&c)
		if err != nil {
			return fmt.Errorf("marshal cooldown: %w", err)
		}
		if rev == 0 {
			if _, err := cs.bucket.Create(key, data); err != nil {
				if isCASConflict(err) {
					continue
				}
				return fmt.Errorf("create cooldown: %w", err)
			}
		} else {
			if _, err := cs.bucket.Update(key, data, rev); err != nil {
				if isCASConflict(err) {
					continue
				}
				return fmt.Errorf("update cooldown: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("cooldown update conflict after %d retries", allocationMutateMaxRetries)
}
