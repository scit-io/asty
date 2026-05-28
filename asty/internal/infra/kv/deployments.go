package kv

import (
	"fmt"

	"asty/asty/internal/core/codec"

	"github.com/nats-io/nats.go"
)

// deploymentKey is the KV key carrying the most recent deployment
// record for a service. Lives alongside `nodes/`, `allocs/`,
// `cooldowns/`, etc. in the asty-cluster bucket.
//
// Schema convention: subjects use dots, not slashes, because
// `nats.KeyValue` keys map onto JetStream subjects under the hood and
// dotted-segments are what the wildcard semantics expect. The other
// keys in the package follow the same pattern (`service.<name>.X`).
const deploymentKey = "service.%s.deployment"

// PutDeployment writes the canonical DeploymentRecord for `service`
// into KV, overwriting whatever was there before. The deployer calls
// this on every status transition (begin/complete/fail/revert) so
// any observer can pick up the freshest state with a single Get.
//
// The payload is opaque CBOR-encoded bytes to keep this package free
// of the higher-layer ops/deployer types (would create an import
// cycle): callers marshal the record themselves with codec.State.
func (cs *ClusterState) PutDeployment(service string, payload []byte) error {
	if service == "" {
		return fmt.Errorf("PutDeployment: service is empty")
	}
	key := fmt.Sprintf(deploymentKey, service)
	if _, err := cs.bucket.Put(key, payload); err != nil {
		return fmt.Errorf("put deployment: %w", err)
	}
	return nil
}

// GetDeployment returns the most recently-written DeploymentRecord
// payload for the service. Missing keys come back as (nil, nil) — the
// service has no deployment history in KV, not an error.
func (cs *ClusterState) GetDeployment(service string) ([]byte, error) {
	key := fmt.Sprintf(deploymentKey, service)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	return entry.Value(), nil
}

// MarshalDeploymentRecord and UnmarshalDeploymentRecord are thin
// wrappers around codec.State so callers don't need to import codec
// directly. The shape of the payload is whatever ops/deployer
// (DeploymentRecord) encodes — kv stays type-agnostic.
func MarshalDeploymentRecord(v any) ([]byte, error)   { return codec.State.Marshal(v) }
func UnmarshalDeploymentRecord(b []byte, v any) error { return codec.State.Unmarshal(b, v) }
