package state

import (
	"context"
	"fmt"
	"sync"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// watchKV is the shared driver behind every Watch* method. It opens a KV
// watcher on pattern, then for each update either calls onDeleted (KV
// delete/purge) or onUpsert (decoded entry). When NATS finishes the initial
// history replay it sends a nil entry; if onReady is non-nil, it is invoked
// once at that point — useful for "wait until cache is hot" callers.
func watchKV(
	ctx context.Context,
	bucket nats.KeyValue,
	pattern string,
	onUpsert func(entry nats.KeyValueEntry),
	onDeleted func(key string),
	onReady func(),
) error {
	watcher, err := bucket.Watch(pattern, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("create watcher %s: %w", pattern, err)
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
				if onReady != nil {
					readyOnce.Do(onReady)
				}
				continue
			}
			switch entry.Operation() {
			case nats.KeyValueDelete, nats.KeyValuePurge:
				if onDeleted != nil {
					onDeleted(entry.Key())
				}
			default:
				onUpsert(entry)
			}
		}
	}
}

// nodeWatchHandlers wires watchKV for node.* keys: it decodes JSON into
// NodeInfo and synthesises a deleted-marker for tombstones so callers see
// a uniform stream of *NodeInfo events.
func (cs *ClusterState) nodeWatchHandlers(onChange func(*types.NodeInfo)) (
	func(nats.KeyValueEntry), func(string),
) {
	upsert := func(entry nats.KeyValueEntry) {
		var node types.NodeInfo
		if err := codec.State.Unmarshal(entry.Value(), &node); err != nil {
			log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal node")
			return
		}
		onChange(&node)
	}
	deleted := func(key string) {
		onChange(&types.NodeInfo{ID: keySuffix(key, "node."), Status: types.NodeDeleted})
	}
	return upsert, deleted
}

// allocWatchHandlers is the allocation analogue of nodeWatchHandlers.
func (cs *ClusterState) allocWatchHandlers(onChange func(*types.ServiceAllocation)) (
	func(nats.KeyValueEntry), func(string),
) {
	upsert := func(entry nats.KeyValueEntry) {
		var alloc types.ServiceAllocation
		if err := codec.State.Unmarshal(entry.Value(), &alloc); err != nil {
			log.Warn().Err(err).Str("key", entry.Key()).Msg("failed to unmarshal allocation")
			return
		}
		onChange(&alloc)
	}
	deleted := func(key string) {
		svc, node := splitAllocKey(key)
		onChange(&types.ServiceAllocation{ServiceName: svc, NodeID: node, Status: types.AllocDeleted})
	}
	return upsert, deleted
}

// WatchNodes invokes onChange once per node KV update or delete.
func (cs *ClusterState) WatchNodes(ctx context.Context, onChange func(*types.NodeInfo)) error {
	upsert, deleted := cs.nodeWatchHandlers(onChange)
	return watchKV(ctx, cs.bucket, "node.*", upsert, deleted, nil)
}

// WatchNodesInit is like WatchNodes but also calls onReady once after the
// initial key-history replay completes.
func (cs *ClusterState) WatchNodesInit(ctx context.Context, onChange func(*types.NodeInfo), onReady func()) error {
	upsert, deleted := cs.nodeWatchHandlers(onChange)
	return watchKV(ctx, cs.bucket, "node.*", upsert, deleted, onReady)
}

// WatchAllocations invokes onChange once per allocation KV update or delete.
func (cs *ClusterState) WatchAllocations(ctx context.Context, onChange func(*types.ServiceAllocation)) error {
	upsert, deleted := cs.allocWatchHandlers(onChange)
	return watchKV(ctx, cs.bucket, "alloc.>", upsert, deleted, nil)
}

// WatchAllocationsInit is like WatchAllocations but also calls onReady once.
func (cs *ClusterState) WatchAllocationsInit(ctx context.Context, onChange func(*types.ServiceAllocation), onReady func()) error {
	upsert, deleted := cs.allocWatchHandlers(onChange)
	return watchKV(ctx, cs.bucket, "alloc.>", upsert, deleted, onReady)
}

// WatchAllocation watches a single allocation key. The callback returns
// true to stop watching — used by drain to wait for a specific transition.
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
			if err := codec.State.Unmarshal(entry.Value(), &alloc); err != nil {
				continue
			}
			if fn(&alloc) {
				return nil
			}
		}
	}
}
