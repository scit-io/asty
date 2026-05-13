package netutil

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// kvInitialBackoff is the first sleep between create-or-fetch attempts.
// We start fast (100 ms) so a cluster that's already up sees ~zero
// latency on cold start; the doubling backoff handles slow JetStream
// boot without wasting wall-clock time.
const kvInitialBackoff = 100 * time.Millisecond

// kvMaxBackoff caps the per-step delay. Reaching the cap means
// JetStream is genuinely slow to come up; we keep retrying at this
// rate up to kvTotalBudget total wall time.
const kvMaxBackoff = 3 * time.Second

// kvTotalBudget is the cumulative wait ceiling. ~30 s matches the
// previous fixed budget of 30×1 s; under that ceiling exponential
// backoff lets the typical case finish far sooner.
const kvTotalBudget = 30 * time.Second

// EnsureBucket creates the KV bucket (or fetches it if it already
// exists) and waits until it accepts reads. Retries use exponential
// backoff capped at kvMaxBackoff, with a kvTotalBudget total ceiling.
//
// At boot the JetStream stream may briefly return errors while
// propagating; callers (cluster state, leader election) want a single
// helper that hides that startup race instead of duplicating the retry
// loop.
func EnsureBucket(js nats.JetStreamContext, cfg *nats.KeyValueConfig) (nats.KeyValue, error) {
	bucket, err := retryWithBackoff(func() (nats.KeyValue, error) {
		if b, e := js.CreateKeyValue(cfg); e == nil {
			return b, nil
		}
		return js.KeyValue(cfg.Bucket)
	})
	if err != nil {
		return nil, fmt.Errorf("ensure bucket %s: %w", cfg.Bucket, err)
	}

	_, err = retryWithBackoff(func() (struct{}, error) {
		_, e := bucket.Keys()
		if e == nil || e == nats.ErrNoKeysFound {
			return struct{}{}, nil
		}
		return struct{}{}, e
	})
	if err != nil {
		return nil, fmt.Errorf("bucket %s not ready: %w", cfg.Bucket, err)
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
