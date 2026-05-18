package leader

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

// WaitForLeader returns the current leader info, blocking on the KV
// watcher until one appears. Cheaper than spinning a poll loop because
// JetStream signals as soon as the slot is filled.
func (e *Election) WaitForLeader(ctx context.Context) (Info, error) {
	if leader, err := e.GetLeader(); err == nil {
		return leader, nil
	}

	watcher, err := e.bucket.Watch(leaderKey, nats.Context(ctx))
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
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				continue
			}
			return e.GetLeader()
		}
	}
}

// WatchLeadership invokes onBecomeLeader / onLoseLeadership when the
// current node's leadership status changes. It keeps running until ctx
// is cancelled.
func (e *Election) WatchLeadership(ctx context.Context, onBecomeLeader, onLoseLeadership func()) error {
	watcher, err := e.bucket.Watch(leaderKey, nats.Context(ctx))
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
				continue
			}

			var isLeader bool
			switch entry.Operation() {
			case nats.KeyValueDelete, nats.KeyValuePurge:
				isLeader = false
			default:
				isLeader = parseLeaderID(entry.Value()) == e.nodeID
			}

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
