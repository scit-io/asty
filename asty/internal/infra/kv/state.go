package kv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"asty/asty/internal/core/netutil"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

const allocationMutateMaxRetries = 8

// kvOpTimeout caps one KV Get/Put/Create/Update/Delete round-trip. The
// jetstream API is context-based; these are short request-reply ops, so a
// bounded ceiling keeps a wedged broker from hanging callers indefinitely.
const kvOpTimeout = 10 * time.Second

// ClusterState manages cluster state in NATS JetStream KV. It uses the
// nats.go `jetstream` package (NOT the deprecated JetStreamContext): its KV
// watchers are backed by ordered consumers that auto-recreate after a
// nats-server restart (the natssolo 2→1 collapse restarts the local broker),
// so the streamHub index does not go stale. See nats.go #1094/#1097.
type ClusterState struct {
	bucket jetstream.KeyValue
	// js is kept so node-heartbeat renews can publish directly to the
	// KV stream subject with a Nats-TTL header. The KV API's Update
	// path strips that header silently (reproduced live on 2.14.2), so
	// renew goes through raw JetStream publish — see UpsertNodeWithTTL.
	js jetstream.JetStream
}

// New creates a new cluster state manager.
func New(nc *nats.Conn) (*ClusterState, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}

	bucket, err := netutil.EnsureBucket(js, jetstream.KeyValueConfig{
		Bucket:      "asty-cluster",
		Description: "Asty cluster state",
		// History=1 (not >1). Per-key TTL — the only signal survivors
		// have that an unplugged node is gone — silently doesn't fire
		// on buckets with History>1 on nats-server 2.14.x (reproduced
		// live: replicas=5+history=1 expires keys, replicas=5+history=10
		// does not). Past KV revisions are not read anywhere in this
		// codebase, so History=1 costs nothing.
		History: 1,
		// Enables per-key TTL on this bucket. UpdateNode publishes each
		// `node.<id>` write with a Nats-TTL header so a missed heartbeat
		// removes the record automatically — survivors see the deletion
		// via the regular WatchNodes delete-marker, no leader-side
		// reaper, no cooperation from the dying node.
		LimitMarkerTTL: time.Minute,
	})
	if err != nil {
		return nil, err
	}

	log.Info().Msg("cluster state initialized")

	return &ClusterState{bucket: bucket, js: js}, nil
}

// kvCtx returns a bounded context for a single KV operation.
func kvCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), kvOpTimeout)
}

// isCASConflict reports whether err is a JetStream wrong-last-sequence
// rejection — the optimistic-concurrency miss returned by Update (stale
// revision) and Create (key already exists), both API code 10071. The
// caller re-reads and retries.
func isCASConflict(err error) bool {
	var jsErr jetstream.JetStreamError
	if errors.As(err, &jsErr) && jsErr.APIError() != nil {
		return jsErr.APIError().ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
	}
	return false
}

// keySuffix returns the part of key after prefix, or "" if key doesn't
// start with prefix or is exactly equal to it.
func keySuffix(key, prefix string) string {
	if !strings.HasPrefix(key, prefix) || len(key) == len(prefix) {
		return ""
	}
	return key[len(prefix):]
}

// splitAllocKey parses an alloc.<service>.<node> KV key into its parts.
func splitAllocKey(key string) (service, node string) {
	rest := keySuffix(key, "alloc.")
	service, node, _ = strings.Cut(rest, ".")
	return service, node
}
