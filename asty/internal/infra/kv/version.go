package kv

import (
	"errors"
	"fmt"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go/jetstream"
)

const serviceVersionKey = "service.%s.version"

// GetServiceVersion returns the pinned version pair for a service.
// Missing keys come back as a zero-value ServiceVersion (Current="" —
// caller falls back to "latest") rather than an error.
func (cs *ClusterState) GetServiceVersion(service string) (types.ServiceVersion, error) {
	key := fmt.Sprintf(serviceVersionKey, service)
	ctx, cancel := kvCtx()
	defer cancel()
	entry, err := cs.bucket.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return types.ServiceVersion{}, nil
		}
		return types.ServiceVersion{}, fmt.Errorf("get version: %w", err)
	}
	var v types.ServiceVersion
	if err := codec.State.Unmarshal(entry.Value(), &v); err != nil {
		return types.ServiceVersion{}, fmt.Errorf("unmarshal version: %w", err)
	}
	return v, nil
}

// SetServiceVersion overwrites the pinned version pair for a service.
// The deployer is the only writer; it calls this on deploy-begin,
// deploy-success, and deploy-revert to keep new-alloc placement aligned
// with the operator's intent.
func (cs *ClusterState) SetServiceVersion(service string, v types.ServiceVersion) error {
	data, err := codec.State.Marshal(&v)
	if err != nil {
		return fmt.Errorf("marshal version: %w", err)
	}
	key := fmt.Sprintf(serviceVersionKey, service)
	ctx, cancel := kvCtx()
	defer cancel()
	if _, err := cs.bucket.Put(ctx, key, data); err != nil {
		return fmt.Errorf("put version: %w", err)
	}
	return nil
}
