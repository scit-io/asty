package server

import (
	"context"
	"errors"
	"strings"

	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// systemStreams enumerates the KV buckets Asty itself provisions. They
// want as many replicas as the cluster can place, capped at
// cluster.system_kv_replicas (wider than app buckets — see ClusterConfig
// and systemReplicas). Spelled out here so targetReplicasFor doesn't
// have to inspect bucket-config metadata.
var systemStreams = map[string]struct{}{
	"KV_asty-cluster": {},
	"KV_asty-leader":  {},
}

// kvStreamPrefix is the prefix the JetStream KV API uses for the
// backing stream of a bucket: bucket "foo" lives in stream "KV_foo".
const kvStreamPrefix = "KV_"

// watchStreamReplicas runs on the leader and keeps every Asty-owned stream's
// replica count matched to the NATS cluster size. Cluster size IS NATS
// membership. Event-driven, no server-side timer or polling — two sources plus
// one initial pass on becoming leader:
//   - nodeChanged: the node KV watch fires on every node event, heartbeats
//     included (~5s). This recurring re-evaluation is what LOWERS replicas as
//     the cluster shrinks: the dead-peer reaper drops a departed node from the
//     JetStream meta, clusterSize falls, and the next heartbeat-triggered pass
//     picks up the smaller size. It also re-runs the reaper itself.
//   - gossipChanged: NATS's discovered-servers callback, the earliest signal
//     of a join so the bucket widens before the joiner writes to KV.
//
// clusterSize (the JetStream RAFT meta group — see kv.go) is the single size
// source for both directions, so the pass is idempotent and cannot flap.
func (s *Server) watchStreamReplicas(ctx context.Context) {
	nodeChanged := make(chan struct{}, 1)
	go s.watchNodesForReplicas(ctx, nodeChanged)
	s.reconcileCluster(ctx) // initial pass
	for {
		select {
		case <-ctx.Done():
			return
		case <-nodeChanged:
			s.reconcileCluster(ctx)
		case <-s.gossipChanged:
			s.reconcileCluster(ctx)
		}
	}
}

// watchNodesForReplicas signals nodeChanged on every node KV event (heartbeats
// included), so watchStreamReplicas re-runs ~every heartbeat. Over-signalling
// is harmless — reconcileStreamReplicas is idempotent. RetryWatchForever
// re-establishes on either a clean churn-close or an error with a bounded
// back-off, so a brief NATS hiccup doesn't busy-loop the reconcile trigger.
func (s *Server) watchNodesForReplicas(ctx context.Context, nodeChanged chan<- struct{}) {
	signal := func() {
		select {
		case nodeChanged <- struct{}{}:
		default:
		}
	}
	netutil.RetryWatchForever(ctx, "stream-replicas-nodes", watcherRetryDelay, func(ctx context.Context) error {
		return s.clusterState.WatchNodes(ctx, func(*types.NodeInfo) { signal() })
	})
}

// reconcileCluster is the leader-only cluster-placement pass: bring stream
// replicas to the NATS cluster size and evict dead meta peers. Idempotent and
// safe to call from any trigger; the IsLeader guard makes a stale call (a
// former leader, or the controller's hook firing through a leadership flip) a
// no-op, so replica edits and reaping stay the leader's job alone.
func (s *Server) reconcileCluster(ctx context.Context) {
	if !s.leaderElection.IsLeader() {
		return
	}
	// One clusterSize read per pass, shared by both halves — each call is a JSZ
	// round-trip, and reading it twice could also let them act on different
	// sizes mid-pass. size <= 1 means standalone (the natssolo survivor): no
	// meta cluster, so skip the reaper — its JSZ meta-leader query would
	// otherwise spin against a meta that has no leader.
	size := s.clusterSize()
	s.reconcileStreamReplicas(ctx, size)
	if size > 1 {
		s.reapDeadPeers()
	}
}

// reconcileStreamReplicas performs one MUTATION pass: bring every Asty-owned
// stream to the replica count the NATS cluster size warrants — raising as
// peers join, lowering as they leave so a shrunk cluster still reaches quorum.
// clusterSize (the JetStream meta group — see kv.go) is the SINGLE source for
// both directions, which makes the pass IDEMPOTENT — it converges to one
// target and never oscillates, however often it is triggered. A change
// JetStream can't apply yet is left for the next node-watch or gossip event to
// re-run; no retry timer. Whether the cluster is fully healed afterwards is a
// separate, read-only question answered on demand by clusterHealed
// (streamhealth.go).
func (s *Server) reconcileStreamReplicas(ctx context.Context, size int) {
	js := s.js

	infos := js.ListStreams(ctx)
	for info := range infos.Info() {
		if info == nil {
			continue
		}
		cur := info.Config.Replicas
		target := s.targetReplicasFor(info.Config.Name, size)
		if target == 0 {
			continue
		}
		if target != cur {
			s.setStreamReplicas(ctx, js, info, target)
			continue
		}
		// Count already matches the cluster size; make sure the PLACEMENT is
		// healthy too — evict any dead replica so JetStream re-places it on a
		// live node (streamplacement.go). Without this, a node death that
		// doesn't change the replica count leaves the stream at online<R
		// forever, so clusterHealed never reports healed.
		s.repairStreamPlacement(info)
	}
	if err := infos.Err(); err != nil {
		log.Warn().Err(err).Msg("stream-replicas: listing streams failed")
	}
}

// targetReplicasFor returns the replica count this stream should run with
// at the given cluster size, capped by what was declared. Zero means
// "leave it alone" — streams Asty does not own (a human-managed migration
// leftover isn't touched here).
func (s *Server) targetReplicasFor(streamName string, clusterSize int) int {
	if _, ok := systemStreams[streamName]; ok {
		// Asty's own control-plane KV — replicated wider than app data.
		return capReplicas(clusterSize, s.cfg.Cluster.SystemKVReplicas)
	}
	bucket := strings.TrimPrefix(streamName, kvStreamPrefix)
	if bucket == streamName {
		return 0
	}
	for _, svc := range s.services {
		for _, kv := range svc.KV {
			if kv.Bucket != bucket {
				continue
			}
			ceiling := s.cfg.Cluster.AppKVReplicas
			if kv.Replicas > 0 && kv.Replicas < ceiling {
				ceiling = kv.Replicas
			}
			return capReplicas(clusterSize, ceiling)
		}
	}
	return 0
}

// setStreamReplicas issues a single UpdateStream to the target replica
// count (up or down) and reports whether it landed. A JetStream error
// (10074 in particular) just means the cluster can't place the count right
// now; the next node/gossip event re-runs the idempotent reconcile. Logs at
// Info on success so the change is visible. Bounded by the caller's ctx
// (server/leadership lifecycle) — no per-call timeout.
func (s *Server) setStreamReplicas(ctx context.Context, js jetstream.JetStream, info *jetstream.StreamInfo, target int) bool {
	cfg := info.Config
	from := cfg.Replicas
	cfg.Replicas = target
	dir := "raised"
	if target < from {
		dir = "lowered"
	}
	if _, err := js.UpdateStream(ctx, cfg); err != nil {
		var jsErr jetstream.JetStreamError
		level := log.Warn()
		if errors.As(err, &jsErr) && jsErr.APIError() != nil {
			level = level.Uint16("code", uint16(jsErr.APIError().ErrorCode))
		}
		level.Err(err).Str("stream", cfg.Name).Int("from", from).Int("target", target).Msg("stream-replicas: " + dir + " failed")
		return false
	}
	log.Info().Str("stream", cfg.Name).Int("from", from).Int("to", target).Msg("stream-replicas: " + dir)
	return true
}
