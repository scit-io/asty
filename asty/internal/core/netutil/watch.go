package netutil

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// RetryWatchForever runs watch(ctx) in a loop and re-establishes whenever it
// returns — both on a non-nil error and on a clean nil close (KV watchers can
// close their channel mid-life during cluster churn). Sleeps `delay` between
// attempts, bounded by ctx; ctx cancellation is the ONLY exit. The watcher
// owns its own initial-state seeding inside `watch`, so each re-establish
// starts from a fresh KV history replay.
//
// One helper replaces five hand-rolled copies (server/streamhub watchers,
// reconciler alloc/node watchers, leader stream-replicas node watcher) that
// each had the same skeleton with subtly different bugs — one slept without
// honouring ctx, another had no backoff at all.
func RetryWatchForever(ctx context.Context, label string, delay time.Duration, watch func(context.Context) error) {
	for ctx.Err() == nil {
		err := watch(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("watcher", label).Msg("KV watcher errored, re-establishing")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
