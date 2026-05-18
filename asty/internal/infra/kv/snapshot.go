package kv

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// snapshotReadTimeout caps the wait for a single bucket Watch's initial
// replay. In practice the marker arrives in milliseconds even for thousands
// of keys; the timeout is a defensive ceiling that prevents a frozen
// JetStream from hanging callers indefinitely.
const snapshotReadTimeout = 30 * time.Second

// snapshotKVByPattern reads every current key+value matching pattern in
// one streaming Watch pass — replaces the N+1 round-trip pattern of
// bucket.Keys() + per-key bucket.Get().
//
// Pattern uses NATS subject wildcards: `node.*` matches every node entry,
// `alloc.>` matches every allocation, `alloc.{svc}.*` matches one service.
//
// The watcher emits a nil entry to signal "initial history complete" —
// we stop reading at that marker, which gives us a point-in-time snapshot.
// Tombstones (deleted/purged entries) are filtered via IgnoreDeletes so
// callers don't have to.
func (cs *ClusterState) snapshotKVByPattern(pattern string) (map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotReadTimeout)
	defer cancel()

	watcher, err := cs.bucket.Watch(pattern, nats.IgnoreDeletes(), nats.Context(ctx))
	if err != nil {
		return nil, fmt.Errorf("watch %s: %w", pattern, err)
	}
	defer watcher.Stop()

	out := make(map[string][]byte)
	for entry := range watcher.Updates() {
		if entry == nil {
			return out, nil
		}
		// Defensive copy: NATS may reuse the underlying buffer on next event.
		out[entry.Key()] = append([]byte(nil), entry.Value()...)
	}
	return out, nil
}
