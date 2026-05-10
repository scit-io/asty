package state

import (
	"errors"
	"fmt"
	"strings"

	"asty/internal/platform/asty/core/netutil"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

const allocationMutateMaxRetries = 8

// ClusterState manages cluster state in NATS JetStream KV
type ClusterState struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	bucket nats.KeyValue
}

// New creates a new cluster state manager
func New(nc *nats.Conn) (*ClusterState, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	bucket, err := netutil.EnsureBucket(js, &nats.KeyValueConfig{
		Bucket:      "asty-cluster",
		Description: "Asty cluster state",
		History:     10,
	})
	if err != nil {
		return nil, err
	}

	log.Info().Msg("cluster state initialized")

	return &ClusterState{
		nc:     nc,
		js:     js,
		bucket: bucket,
	}, nil
}

func isCASConflict(err error) bool {
	var apiErr *nats.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == nats.JSErrCodeStreamWrongLastSequence
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
