package netutil

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// kvInitialBackoff is the first sleep between create-or-fetch attempts.
// We start fast (100 ms) so a cluster that's already up sees ~zero
// latency on cold start; the doubling backoff handles slow JetStream
// boot without wasting wall-clock time.
const kvInitialBackoff = 100 * time.Millisecond

// kvMaxBackoff caps the per-step delay. Reaching the cap means
// JetStream is genuinely slow to come up; we keep retrying at this
// rate up to kvTotalBudget total wall time. It also bounds one
// CreateOrUpdateKeyValue round-trip.
const kvMaxBackoff = 3 * time.Second

// kvTotalBudget is the cumulative wait ceiling. ~30 s matches the
// previous fixed budget of 30×1 s; under that ceiling exponential
// backoff lets the typical case finish far sooner.
const kvTotalBudget = 30 * time.Second

// EnsureBucket opens the KV bucket if it already exists, otherwise
// creates it from cfg. Retries with exponential backoff capped at
// kvMaxBackoff under a kvTotalBudget total ceiling.
//
// At boot the local nats-server may still be finishing JetStream init;
// the underlying calls fail until it is ready, so the retry hides that
// startup race. A success means the bucket exists and is accessible, so
// no separate readiness probe is needed. Callers (cluster state, leader
// election) share this one helper instead of duplicating the retry loop.
//
// Open-then-create (NOT CreateOrUpdateKeyValue): cfg here carries only
// the BOOTSTRAP shape (Replicas defaults to 1 because the bucket may
// be created on a lone first node). On every subsequent startup the
// bucket already exists at whatever replica count the leader's
// watchStreamReplicas raised it to as the cluster grew; using
// CreateOrUpdate would OVERWRITE the actual stream Replicas back to
// cfg.Replicas (= 0/1) on every server restart, which then re-triggers
// the leader's raise — a reset→raise loop that keeps clusterHealed
// false during multi-node startup and prevents convergence. Open
// returns the existing bucket as-is, leaving the stream's current
// Replicas (managed by streamreplicas.go) untouched.
func EnsureBucket(js jetstream.JetStream, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	bucket, err := retryWithBackoff(func() (jetstream.KeyValue, error) {
		ctx, cancel := context.WithTimeout(context.Background(), kvMaxBackoff)
		defer cancel()
		if kv, err := js.KeyValue(ctx, cfg.Bucket); err == nil {
			return kv, nil
		}
		return js.CreateKeyValue(ctx, cfg)
	})
	if err != nil {
		return nil, fmt.Errorf("ensure bucket %s: %w", cfg.Bucket, err)
	}
	return bucket, nil
}

// retryWithBackoff calls op with exponential delay between failures,
// returning the first successful result. Total wait is bounded by
// kvTotalBudget so a permanently broken NATS doesn't hang boot
// indefinitely.
func retryWithBackoff[T any](op func() (T, error)) (T, error) {
	var zero T
	delay := kvInitialBackoff
	totalWait := time.Duration(0)
	var lastErr error

	for totalWait <= kvTotalBudget {
		v, err := op()
		if err == nil {
			return v, nil
		}
		lastErr = err

		time.Sleep(delay)
		totalWait += delay
		delay *= 2
		if delay > kvMaxBackoff {
			delay = kvMaxBackoff
		}
	}
	return zero, lastErr
}
