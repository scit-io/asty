package kv

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// drainKey carries the most recent DrainStatus for a node. Stored in
// the asty-cluster bucket so the operator can read the state of a
// drain even after the originating server has been restarted or
// failed over.
const drainKey = "node.%s.drain"

// PutDrain writes the DrainStatus payload for `nodeID`, overwriting
// the previous one. Drainer calls this on every progress event (start,
// per-alloc migration, complete) so SSE-subscribed dashboards and
// post-mortem investigators see consistent state.
//
// Payload is opaque to kv (the ops/drainer.DrainStatus type lives in
// a higher layer); callers marshal with codec.State or codec.Wire as
// appropriate.
func (cs *ClusterState) PutDrain(nodeID string, payload []byte) error {
	if nodeID == "" {
		return fmt.Errorf("PutDrain: nodeID is empty")
	}
	key := fmt.Sprintf(drainKey, nodeID)
	if _, err := cs.bucket.Put(key, payload); err != nil {
		return fmt.Errorf("put drain: %w", err)
	}
	return nil
}

// GetDrain returns the most recently-written DrainStatus payload for
// the node, or (nil, nil) when the key is missing.
func (cs *ClusterState) GetDrain(nodeID string) ([]byte, error) {
	key := fmt.Sprintf(drainKey, nodeID)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get drain: %w", err)
	}
	return entry.Value(), nil
}

// DeleteDrain removes the per-node drain record entirely. The drainer
// calls this once a Drained node returns to Ready (Resume) so stale
// records don't accumulate.
func (cs *ClusterState) DeleteDrain(nodeID string) error {
	key := fmt.Sprintf(drainKey, nodeID)
	if err := cs.bucket.Delete(key); err != nil && err != nats.ErrKeyNotFound {
		return fmt.Errorf("delete drain: %w", err)
	}
	return nil
}
