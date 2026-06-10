package leader

import (
	"context"
	"fmt"

	"asty/asty/internal/core/codec"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// WaitForLeader returns the current leader info, blocking on the KV
// watcher until one appears. Used by anything that just needs to know
// who's in charge after a fresh boot — does NOT drive election state.
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
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
				continue
			}
			return e.GetLeader()
		}
	}
}

// RunLeaderInfoCache keeps the in-memory `cached` Info in lockstep with
// KV so GetLeader serves the dashboard hot path without per-call KV.Get.
// Re-attaches the watcher when its channel closes (consumer death after
// a nats-server restart, the natssolo 2→1 collapse) — purely event-
// driven: the loop only advances when the channel closes (an event) or
// the context is cancelled. No back-off timer.
//
// This watcher does NOT write election state. The campaign goroutine is
// the sole source of truth for `state` / `lastSeq` / `IsLeader()`.
func (e *Election) RunLeaderInfoCache(ctx context.Context) {
	for ctx.Err() == nil {
		if err := e.runCacheOnce(ctx); err != nil {
			log.Debug().Err(err).Msg("leader-info cache watcher returned, re-attaching")
		}
	}
}

func (e *Election) runCacheOnce(ctx context.Context) error {
	watcher, err := e.bucket.Watch(ctx, leaderKey)
	if err != nil {
		return fmt.Errorf("watch leader key: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return fmt.Errorf("watcher channel closed")
			}
			if entry == nil {
				e.primeCacheIfEmpty()
				continue
			}
			var info Info
			switch entry.Operation() {
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				info = Info{}
				// Event-driven wake: the slot is now empty (TTL sweep
				// after a SIGKILL'd leader, or a graceful stepDown
				// from another node). Nudge our own campaign goroutine
				// to retry Create immediately instead of waiting for
				// the next 22.5 s tick. Cuts failover from
				// ~(TTL + campaignInterval) to one watch-event RTT.
				e.notifyWake()
			default:
				_ = codec.State.Unmarshal(entry.Value(), &info)
			}
			e.setCachedLeader(info)
		}
	}
}
