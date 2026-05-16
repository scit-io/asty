package state

import (
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// CreateAllocation creates a service allocation record
func (cs *ClusterState) CreateAllocation(alloc *types.ServiceAllocation) error {
	if alloc.ID == "" {
		alloc.ID = fmt.Sprintf("%s-%s-%d", alloc.ServiceName, alloc.NodeID, time.Now().UnixNano())
	}

	alloc.CreatedAt = time.Now()
	alloc.UpdatedAt = time.Now()

	data, err := codec.Marshal(alloc)
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
func (cs *ClusterState) GetAllocation(serviceName, nodeID string) (*types.ServiceAllocation, error) {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, fmt.Errorf("allocation not found")
		}
		return nil, fmt.Errorf("failed to get allocation: %w", err)
	}

	var alloc types.ServiceAllocation
	if err := codec.Unmarshal(entry.Value(), &alloc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal allocation: %w", err)
	}

	return &alloc, nil
}

// ListAllocations returns all allocations for a service via a single
// streaming Watch snapshot (see snapshotKVByPattern) — no per-key round-trips.
func (cs *ClusterState) ListAllocations(serviceName string) ([]*types.ServiceAllocation, error) {
	pattern := fmt.Sprintf("alloc.%s.*", serviceName)
	return cs.allocsFromSnapshot(pattern)
}

// ListAllAllocations returns every allocation record in the bucket via a
// single streaming Watch snapshot.
func (cs *ClusterState) ListAllAllocations() ([]*types.ServiceAllocation, error) {
	return cs.allocsFromSnapshot("alloc.>")
}

func (cs *ClusterState) allocsFromSnapshot(pattern string) ([]*types.ServiceAllocation, error) {
	raw, err := cs.snapshotKVByPattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("snapshot allocations: %w", err)
	}
	allocs := make([]*types.ServiceAllocation, 0, len(raw))
	for key, data := range raw {
		var alloc types.ServiceAllocation
		if err := codec.Unmarshal(data, &alloc); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to unmarshal allocation")
			continue
		}
		allocs = append(allocs, &alloc)
	}
	return allocs, nil
}

// UpdateAllocation overwrites the allocation record.
func (cs *ClusterState) UpdateAllocation(alloc *types.ServiceAllocation) error {
	alloc.UpdatedAt = time.Now()

	data, err := codec.Marshal(alloc)
	if err != nil {
		return fmt.Errorf("failed to marshal allocation: %w", err)
	}

	key := fmt.Sprintf("alloc.%s.%s", alloc.ServiceName, alloc.NodeID)
	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("failed to update allocation: %w", err)
	}

	return nil
}

// MutateAllocation atomically applies fn to the allocation using CAS.
func (cs *ClusterState) MutateAllocation(serviceName, nodeID string, fn func(*types.ServiceAllocation) bool) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	for attempt := 0; attempt < allocationMutateMaxRetries; attempt++ {
		entry, err := cs.bucket.Get(key)
		if err != nil {
			if err == nats.ErrKeyNotFound {
				return fmt.Errorf("allocation not found: %s/%s", serviceName, nodeID)
			}
			return fmt.Errorf("failed to get allocation: %w", err)
		}

		var alloc types.ServiceAllocation
		if err := codec.Unmarshal(entry.Value(), &alloc); err != nil {
			return fmt.Errorf("failed to unmarshal allocation: %w", err)
		}
		if !fn(&alloc) {
			return nil
		}
		alloc.UpdatedAt = time.Now()

		data, err := codec.Marshal(&alloc)
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

// DeleteAllocation removes an allocation
func (cs *ClusterState) DeleteAllocation(serviceName, nodeID string) error {
	key := fmt.Sprintf("alloc.%s.%s", serviceName, nodeID)
	if err := cs.bucket.Delete(key); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}

	return nil
}
