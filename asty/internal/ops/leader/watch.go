package leader

import (
	"context"
	"fmt"

	"asty/asty/internal/core/codec"

	"github.com/nats-io/nats.go/jetstream"
)

// WaitForLeader returns the current leader info, blocking on the KV
// watcher until one appears. Cheaper than spinning a poll loop because
// JetStream signals as soon as the slot is filled.
func (e *Election) WaitForLeader(ctx context.Context) (Info, error) {
	if leader, err := e.GetLeader(); err == nil {
		return leader, nil
	}

	watcher, err := e.bucket.Watch(ctx, leaderKey)
	if err != nil {
		return Info{}, fmt.Errorf("watch leader key: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return Info{}, ctx.Err()
		case entry, ok := <-watcher.Updates():
			if !ok {
				return Info{}, fmt.Errorf("watcher closed before leader appeared")
			}
			if entry == nil {
				// End of initial replay with no leader; keep waiting.
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
				continue
			}
			return e.GetLeader()
		}
	}
}

// WatchLeadership invokes onBecomeLeader / onLoseLeadership when the
// current node's leadership status changes AND keeps the in-memory
// leader-info cache in sync so GetLeader doesn't need a per-call KV
// read. Runs until ctx is cancelled. The watcher is a jetstream
// OrderedConsumer, so it auto-recreates and resumes after a nats-server
// restart (the natssolo 2→1 collapse) instead of going silently deaf —
// leadership flips keep driving startLeaderWork/stopLeaderWork. See
// nats.go #1094.
func (e *Election) WatchLeadership(ctx context.Context, onBecomeLeader, onLoseLeadership func()) error {
	watcher, err := e.bucket.Watch(ctx, leaderKey)
	if err != nil {
		return fmt.Errorf("failed to watch leader key: %w", err)
	}
	defer watcher.Stop()

	wasLeader := e.isLeader
	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				// End-of-history-replay marker. Do not clobber a real entry
				// we already cached during replay; only mark the cache as
				// "known empty" so GetLeader stops falling through to KV.
				e.primeCacheIfEmpty()
				continue
			}

			var (
				info     Info
				isLeader bool
			)
			switch entry.Operation() {
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				isLeader = false
				info = Info{}
			default:
				_ = codec.State.Unmarshal(entry.Value(), &info)
				isLeader = info.ID == e.nodeID
			}
			e.setCachedLeader(info)

			if isLeader && !wasLeader {
				onBecomeLeader()
			}
			if !isLeader && wasLeader {
				onLoseLeadership()
			}
			wasLeader = isLeader
		}
	}
}
