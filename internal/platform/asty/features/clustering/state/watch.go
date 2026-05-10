package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// WatchNodes invokes onChange once per node KV update or delete.
func (cs *ClusterState) WatchNodes(ctx context.Context, onChange func(*types.NodeInfo)) error {
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
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				onChange(&types.NodeInfo{ID: keySuffix(entry.Key(), "node."), Status: "deleted"})
				continue
			}
			var node types.NodeInfo
			if err := json.Unmarshal(entry.Value(), &node); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal node")
				continue
			}
			onChange(&node)
		}
	}
}

// WatchAllocations invokes onChange once per allocation KV update or delete.
func (cs *ClusterState) WatchAllocations(ctx context.Context, onChange func(*types.ServiceAllocation)) error {
	watcher, err := cs.bucket.Watch("alloc.>", nats.Context(ctx))
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
				onChange(&types.ServiceAllocation{ServiceName: svc, NodeID: node, Status: "deleted"})
				continue
			}
			var alloc types.ServiceAllocation
			if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal allocation")
				continue
			}
			onChange(&alloc)
		}
	}
}

// WatchNodesInit is like WatchNodes but also calls onReady once after the
// initial key-history replay completes.
func (cs *ClusterState) WatchNodesInit(ctx context.Context, onChange func(*types.NodeInfo), onReady func()) error {
	watcher, err := cs.bucket.Watch("node.*", nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to create node watcher: %w", err)
	}
	defer watcher.Stop()

	var readyOnce sync.Once
	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				readyOnce.Do(onReady)
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				onChange(&types.NodeInfo{ID: keySuffix(entry.Key(), "node."), Status: "deleted"})
				continue
			}
			var node types.NodeInfo
			if err := json.Unmarshal(entry.Value(), &node); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal node")
				continue
			}
			onChange(&node)
		}
	}
}

// WatchAllocationsInit is like WatchAllocations but also calls onReady once.
func (cs *ClusterState) WatchAllocationsInit(ctx context.Context, onChange func(*types.ServiceAllocation), onReady func()) error {
	watcher, err := cs.bucket.Watch("alloc.>", nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to create alloc watcher: %w", err)
	}
	defer watcher.Stop()

	var readyOnce sync.Once
	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				readyOnce.Do(onReady)
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				svc, node := splitAllocKey(entry.Key())
				onChange(&types.ServiceAllocation{ServiceName: svc, NodeID: node, Status: "deleted"})
				continue
			}
			var alloc types.ServiceAllocation
			if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
				log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal allocation")
				continue
			}
			onChange(&alloc)
		}
	}
}

// WatchAllocation watches a single allocation key.
func (cs *ClusterState) WatchAllocation(ctx context.Context, serviceName, nodeID string, fn func(*types.ServiceAllocation) bool) error {
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
				continue
			}
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				if fn(nil) {
					return nil
				}
				continue
			}
			var alloc types.ServiceAllocation
			if err := json.Unmarshal(entry.Value(), &alloc); err != nil {
				continue
			}
			if fn(&alloc) {
				return nil
			}
		}
	}
}
