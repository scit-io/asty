package state

import (
	"errors"
	"fmt"
	"time"

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

	var bucket nats.KeyValue
	for attempt := 0; attempt < 30; attempt++ {
		bucket, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      "asty-cluster",
			Description: "Asty cluster state",
			History:     10,
		})
		if err == nil {
			break
		}
		bucket, err = js.KeyValue("asty-cluster")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create/get KV bucket after retries: %w", err)
	}

	for attempt := 0; attempt < 30; attempt++ {
		if _, err := bucket.Keys(); err == nats.ErrNoKeysFound {
			break
		} else if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
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

func keySuffix(key, prefix string) string {
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return ""
	}
	return key[len(prefix):]
}

func splitAllocKey(key string) (string, string) {
	const prefix = "alloc."
	rest := keySuffix(key, prefix)
	for i := 0; i < len(rest); i++ {
		if rest[i] == '.' {
			return rest[:i], rest[i+1:]
		}
	}
	return rest, ""
}
