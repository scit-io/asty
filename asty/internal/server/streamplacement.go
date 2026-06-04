package server

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// streamPeerRemoveTimeout caps one $JS.API.STREAM.PEER.REMOVE round-trip. A
// no-response means the stream's own RAFT group can't reach quorum (too many
// replicas offline) — the next reconcile retries. A cluster collapsing to a
// single node is natssolo's path, not this one.
const streamPeerRemoveTimeout = 5 * time.Second

// repairStreamPlacement forces JetStream to migrate a stream's replicas OFF any
// offline (dead) peer onto a live node. It is the PLACEMENT half of the
// reconcile, complementing the replica-COUNT half (setStreamReplicas):
//
// setStreamReplicas only fires on a count change (UpdateStream re-evaluates
// placement as a side effect). But when a node dies while the cluster still
// holds >= R nodes, the target count is UNCHANGED, so no UpdateStream runs and
// the dead peer stays in the stream's RAFT group forever — online < R, the
// stream never goes "current", and clusterHealed never reports healed. (This
// bit at a hard 12→1 leader-kill run: killing the JS meta leader removed it
// from the meta but left it as a dead replica in every KV stream; the count
// was still right at 8 nodes, so nothing re-placed it.)
//
// $JS.API.STREAM.PEER.REMOVE drops the dead peer and JetStream re-assigns the
// slot to a live node. Driven by the same heartbeat/gossip reconcile trigger,
// so it self-heals within a beat of a peer dying. A healthy stream (no offline
// replicas) is a no-op, so this adds no churn. The removal needs the stream's
// own quorum to apply; a quorum-lost stream is the N→1 case natssolo handles.
func (s *Server) repairStreamPlacement(info *jetstream.StreamInfo) {
	if info.Cluster == nil {
		return
	}
	for _, r := range info.Cluster.Replicas {
		if r == nil || !r.Offline {
			continue
		}
		s.removeStreamPeer(info.Config.Name, r.Name)
	}
}

// removeStreamPeer issues one $JS.API.STREAM.PEER.REMOVE for a dead replica of
// stream. Info on success (a real placement change), Debug on a
// no-quorum/rejected response that the next reconcile retries.
func (s *Server) removeStreamPeer(stream, peer string) {
	body, err := json.Marshal(map[string]string{"peer": peer})
	if err != nil {
		return
	}
	msg, err := s.nc.Request("$JS.API.STREAM.PEER.REMOVE."+stream, body, streamPeerRemoveTimeout)
	if err != nil {
		log.Debug().Err(err).Str("stream", stream).Str("peer", peer).
			Msg("stream-replicas: peer-remove no response (stream under quorum?), will retry")
		return
	}
	var resp struct {
		Success bool `json:"success"`
		Error   *struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return
	}
	if resp.Error != nil {
		log.Debug().Str("stream", stream).Str("peer", peer).Str("err", resp.Error.Description).
			Msg("stream-replicas: peer-remove rejected, will retry")
		return
	}
	if resp.Success {
		log.Info().Str("stream", stream).Str("peer", peer).
			Msg("stream-replicas: removed dead replica peer; JetStream re-places it on a live node")
	}
}
