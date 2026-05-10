package netutil

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// kvBucketReadyAttempts and kvBucketReadyDelay control how long
// EnsureBucket waits for JetStream to come up at boot. Total wait is
// bounded by attempts × delay = 30 s, which covers cold-start of a
// freshly-launched embedded NATS server.
const (
	kvBucketReadyAttempts = 30
	kvBucketReadyDelay    = 1 * time.Second
)

// EnsureBucket creates the KV bucket (or fetches it if it already exists)
// and waits until it accepts reads, retrying transient errors. Returns the
// usable bucket handle.
//
// At boot the JetStream stream may briefly return errors while propagating;
// callers (cluster state, leader election) want a single helper that hides
// that startup race instead of duplicating the retry loop.
func EnsureBucket(js nats.JetStreamContext, cfg *nats.KeyValueConfig) (nats.KeyValue, error) {
	var (
		bucket nats.KeyValue
		err    error
	)

	for attempt := 0; attempt < kvBucketReadyAttempts; attempt++ {
		if bucket, err = js.CreateKeyValue(cfg); err == nil {
			break
		}
		if bucket, err = js.KeyValue(cfg.Bucket); err == nil {
			break
		}
		time.Sleep(kvBucketReadyDelay)
	}
	if err != nil {
		return nil, fmt.Errorf("ensure bucket %s after %d retries: %w", cfg.Bucket, kvBucketReadyAttempts, err)
	}

	for attempt := 0; attempt < kvBucketReadyAttempts; attempt++ {
		_, err = bucket.Keys()
		if err == nil || err == nats.ErrNoKeysFound {
			return bucket, nil
		}
		time.Sleep(kvBucketReadyDelay)
	}
	return nil, fmt.Errorf("bucket %s not ready after %d retries: %w", cfg.Bucket, kvBucketReadyAttempts, err)
}
