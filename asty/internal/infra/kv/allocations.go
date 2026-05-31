package kv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// allocIDBytes — random bytes per allocation ID. Hex-encoded gives a
// 40-char string, matching the Git SHA-1 length the UI renders as a
// short 7-char hash plus a tooltip with the full value.
const allocIDBytes = 20

// newAllocID returns a fresh 40-char hex string. crypto/rand is the
// source — collisions across an entire cluster's lifetime are
// astronomically unlikely at this width. A read failure makes the
// whole CreateAllocation call return an error rather than fall back to
// a predictable suffix.
func newAllocID() (string, error) {
	buf := make([]byte, allocIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate allocation id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// CreateAllocation creates a service allocation record
func (cs *ClusterState) CreateAllocation(alloc *types.ServiceAllocation) error {
	if alloc.ID == "" {
		id, err := newAllocID()
		if err != nil {
			return err
		}
		alloc.ID = id
	}

	alloc.CreatedAt = time.Now()
	alloc.UpdatedAt = time.Now()

	data, err := codec.State.Marshal(alloc)
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
	if err := codec.State.Unmarshal(entry.Value(), &alloc); err != nil {
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
		if err := codec.State.Unmarshal(data, &alloc); err != nil {
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

	data, err := codec.State.Marshal(alloc)
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
		if err := codec.State.Unmarshal(entry.Value(), &alloc); err != nil {
			return fmt.Errorf("failed to unmarshal allocation: %w", err)
		}
		if !fn(&alloc) {
			return nil
		}
		alloc.UpdatedAt = time.Now()

		data, err := codec.State.Marshal(&alloc)
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
