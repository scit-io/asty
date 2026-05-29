package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// streamReplicasRetryDelay — how long to wait before re-attempting an
// upgrade that could not place its replicas yet (cluster still settling
// right after a join, JS err 10074). Upgrades are otherwise driven by
// node-join events; this is the one failure mode with no event to wait
// on, so it gets a bounded retry.
const streamReplicasRetryDelay = 10 * time.Second

// streamReplicasUpdateTimeout — per-UpdateStream context budget. Sized
// so a stale RAFT election doesn't pin the loop indefinitely.
const streamReplicasUpdateTimeout = 10 * time.Second

// systemStreams enumerates the KV buckets Asty itself provisions. They
// always want as many replicas as the cluster can place, capped at the
// maxReplicas ceiling enforced by autoReplicas. Spelled out here so
// targetReplicasFor doesn't have to inspect bucket-config metadata.
var systemStreams = map[string]struct{}{
	"KV_asty-cluster": {},
	"KV_asty-leader":  {},
}

// kvStreamPrefix is the prefix the JetStream KV API uses for the
// backing stream of a bucket: bucket "foo" lives in stream "KV_foo".
const kvStreamPrefix = "KV_"

// watchStreamReplicas runs on the leader and raises stream replica counts
// as the cluster grows. It only ever raises — never lowers — so a node
// briefly unreachable doesn't shrink durable buckets. A node joining is
// the only thing that lets a stream place more replicas, so the work is
// driven by node events rather than a blind periodic scan. The lone
// exception is a just-grown cluster that hasn't settled enough to place
// the replicas yet (JS err 10074): there is no event for "placement now
// possible", so reconcile reports that and we re-arm a bounded retry.
func (s *Server) watchStreamReplicas(ctx context.Context) {
	nodeChanged := make(chan struct{}, 1)
	go s.watchNodesForReplicas(ctx, nodeChanged)

	// A nil channel blocks forever in select, so retryC simply never
	// fires until a reconcile pass asks for a retry.
	var retryC <-chan time.Time
	arm := func(incomplete bool) {
		if incomplete {
			retryC = time.After(streamReplicasRetryDelay)
		} else {
			retryC = nil
		}
	}

	// Initial pass — existing streams may already be under-replicated.
	arm(s.reconcileStreamReplicas(ctx))
	for {
		select {
		case <-ctx.Done():
			return
		case <-nodeChanged:
			arm(s.reconcileStreamReplicas(ctx))
		case <-retryC:
			arm(s.reconcileStreamReplicas(ctx))
		}
	}
}

// watchNodesForReplicas signals nodeChanged on every cluster-membership
// change. Over-signalling is harmless — reconcileStreamReplicas only ever
// raises counts and no-ops when nothing is below target — so this fires on
// any node event rather than decoding ready-transitions. Mirrors the
// reconciler's node watcher: re-establish the watch if it errors.
func (s *Server) watchNodesForReplicas(ctx context.Context, nodeChanged chan<- struct{}) {
	signal := func() {
		select {
		case nodeChanged <- struct{}{}:
		default:
		}
	}
	for ctx.Err() == nil {
		err := s.clusterState.WatchNodes(ctx, func(*types.NodeInfo) { signal() })
		if ctx.Err() != nil || err == nil {
			return
		}
		log.Warn().Err(err).Msg("stream-replicas: node watcher errored, retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(streamReplicasRetryDelay):
		}
	}
}

// reconcileStreamReplicas performs one pass: list streams, compute the
// target replica count for each, and UpdateStream where the current value
// is below it. Returns true ("incomplete") when at least one upgrade could
// not be applied, so the caller can schedule a retry; a single bad stream
// never stalls the rest.
func (s *Server) reconcileStreamReplicas(ctx context.Context) bool {
	js, err := jetstream.New(s.nc)
	if err != nil {
		log.Warn().Err(err).Msg("stream-replicas: jetstream init failed")
		return true
	}

	incomplete := false
	infos := js.ListStreams(ctx)
	for info := range infos.Info() {
		if info == nil {
			continue
		}
		target := s.targetReplicasFor(info.Config.Name)
		if target == 0 || target <= info.Config.Replicas {
			continue
		}
		if !s.upgradeStreamReplicas(js, info, target) {
			incomplete = true
		}
	}
	if err := infos.Err(); err != nil {
		log.Warn().Err(err).Msg("stream-replicas: listing streams failed")
		incomplete = true
	}
	return incomplete
}

// targetReplicasFor returns the replica count this stream should run
// with given the current cluster size and what was declared. Zero
// means "leave it alone" — used for streams Asty does not own (so a
// human-managed migration leftover isn't touched here).
func (s *Server) targetReplicasFor(streamName string) int {
	cluster := s.autoReplicas()
	if _, ok := systemStreams[streamName]; ok {
		return cluster
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
			if kv.Replicas <= 0 {
				return cluster
			}
			if kv.Replicas < cluster {
				return kv.Replicas
			}
			return cluster
		}
	}
	return 0
}

// upgradeStreamReplicas issues a single UpdateStream and reports whether
// it landed. A JetStream error (10074 in particular) just means the
// cluster still can't place the requested count; returning false makes
// the caller schedule a retry. Logs at Info on success so growth is
// visible.
func (s *Server) upgradeStreamReplicas(js jetstream.JetStream, info *jetstream.StreamInfo, target int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), streamReplicasUpdateTimeout)
	defer cancel()

	cfg := info.Config
	from := cfg.Replicas
	cfg.Replicas = target
	if _, err := js.UpdateStream(ctx, cfg); err != nil {
		var jsErr jetstream.JetStreamError
		level := log.Warn()
		if errors.As(err, &jsErr) && jsErr.APIError() != nil {
			level = level.Uint16("code", uint16(jsErr.APIError().ErrorCode))
		}
		level.Err(err).Str("stream", cfg.Name).Int("from", from).Int("target", target).Msg("stream-replicas: upgrade failed")
		return false
	}
	log.Info().Str("stream", cfg.Name).Int("from", from).Int("to", target).Msg("stream-replicas: upgraded")
	return true
}
