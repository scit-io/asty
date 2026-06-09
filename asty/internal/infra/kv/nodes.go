package kv

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// nodeKVTTL is how long a `node.<id>` KV record lives without a write.
// Heartbeats run every 5 s; each write carries a Nats-TTL header that
// re-arms the timer to nodeKVTTL. 60 s = 12× heartbeat tolerates the
// cluster-startup burst where KV publishes can time out for 15-20 s
// under replica-catchup load, without ghosting a live node out from
// under itself (and triggering watchSelfRemoval on the server side).
// An unplugged agent stops writing, NATS drops the record at the TTL,
// and survivors see it as a NodeDeleted event; the dead-peer reaper
// catches genuinely-dead-but-meta-still-listed peers from its
// JSZ-active angle within 15 s, so the doubled grace here only adds
// latency to the rarer KV-driven removal path.
const nodeKVTTL = 60 * time.Second

// kvSubjectPrefix is the JetStream subject prefix the KV API uses for
// bucket "asty-cluster". Hard-coded because nats.go does not expose the
// stream's subject prefix directly and StreamInfo on every write would
// cost an extra round-trip per heartbeat.
const kvSubjectPrefix = "$KV.asty-cluster."

// UpdateNode writes the node record to KV with a per-write Nats-TTL
// header so NATS expires the key if no further heartbeats land.
//
// Why raw PublishMsg instead of KeyValue.Update: live probe on
// nats-server 2.14.2 (see .audit/15:39_08-06-26.md §B1) confirms that
// Update CLEARS the per-key TTL entirely — the nats.go comment
// "Update also resets the TTL associated with the key" means "removes
// it", not "re-arms the timer". So heartbeat-renewal must publish
// directly on $KV.<bucket>.<key> with Nats-TTL set on every write.
//
// CAS via ExpectedLastSubjSeqHeader is set ONLY when the key already
// exists (mirrors Update's revision check, prevents a stale agent's
// last write from clobbering a fresh one). When the key is absent —
// first ever write OR right after a TTL expiry — the header is
// omitted: ExpectedLastSubjSeqHeader=0 would mean "subject must be
// empty", but per-key TTL leaves a delete-marker on the subject (its
// LimitMarkerTTL=1m), so the subject sequence is non-zero and NATS
// rejects with err_code=10071 "wrong last sequence". Single-writer-
// per-node makes the no-CAS first write safe (no concurrent writer
// to race with).
func (cs *ClusterState) UpdateNode(node *types.NodeInfo) error {
	now := time.Now()
	node.LastSeen = now

	key := fmt.Sprintf("node.%s", node.ID)

	ctx, cancel := kvCtx()
	defer cancel()

	var prevSeq uint64
	if existing, err := cs.bucket.Get(ctx, key); err == nil {
		var existingNode types.NodeInfo
		if codec.State.Unmarshal(existing.Value(), &existingNode) == nil && !existingNode.CreatedAt.IsZero() {
			if node.CreatedAt.IsZero() {
				node.CreatedAt = existingNode.CreatedAt
			}
		}
		prevSeq = existing.Revision()
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}

	data, err := codec.State.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}

	hdr := nats.Header{
		jetstream.MsgTTLHeader: []string{nodeKVTTL.String()},
	}
	if prevSeq != 0 {
		hdr[jetstream.ExpectedLastSubjSeqHeader] = []string{strconv.FormatUint(prevSeq, 10)}
	}
	msg := &nats.Msg{
		Subject: kvSubjectPrefix + key,
		Data:    data,
		Header:  hdr,
	}
	if _, err := cs.js.PublishMsg(ctx, msg); err != nil {
		// CAS conflict — someone else wrote first. Under one-writer-
		// per-node this is rare; caller retries on the next heartbeat.
		if isCASConflict(err) {
			return fmt.Errorf("heartbeat raced: %w", err)
		}
		return fmt.Errorf("failed to publish node info: %w", err)
	}
	return nil
}

// GetNode retrieves node information from cluster state
func (cs *ClusterState) GetNode(nodeID string) (*types.NodeInfo, error) {
	key := fmt.Sprintf("node.%s", nodeID)
	ctx, cancel := kvCtx()
	defer cancel()
	entry, err := cs.bucket.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, fmt.Errorf("node %s not found", nodeID)
		}
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	var node types.NodeInfo
	if err := codec.State.Unmarshal(entry.Value(), &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node info: %w", err)
	}

	return &node, nil
}

// ListNodes returns all nodes in the cluster via a single streaming Watch
// snapshot (see snapshotKVByPattern) — no per-key round-trips.
func (cs *ClusterState) ListNodes() ([]*types.NodeInfo, error) {
	raw, err := cs.snapshotKVByPattern("node.*")
	if err != nil {
		return nil, fmt.Errorf("snapshot nodes: %w", err)
	}
	nodes := make([]*types.NodeInfo, 0, len(raw))
	for key, data := range raw {
		var node types.NodeInfo
		if err := codec.State.Unmarshal(data, &node); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("failed to unmarshal node")
			continue
		}
		nodes = append(nodes, &node)
	}
	return nodes, nil
}

// RemoveNode removes a node from cluster state
func (cs *ClusterState) RemoveNode(nodeID string) error {
	key := fmt.Sprintf("node.%s", nodeID)
	ctx, cancel := kvCtx()
	defer cancel()
	if err := cs.bucket.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}

	log.Info().Str("node_id", nodeID).Msg("node removed from cluster state")
	return nil
}
