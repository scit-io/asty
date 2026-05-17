package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// streamReplicasInterval — how often the leader scans JetStream and
// upgrades the replica count of streams the cluster has outgrown. The
// only sensitive scenario is a cluster growing from one node to two
// or three; ten seconds is fast enough that newly created KV writes
// land replicated soon after the second node joins, and slow enough
// that the scan is invisible on a steady-state cluster.
const streamReplicasInterval = 10 * time.Second

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

// watchStreamReplicas runs on the leader. Every streamReplicasInterval
// it walks all streams in JetStream and bumps Replicas where the
// cluster has grown beyond what the stream was created with. It only
// ever raises Replicas — never lowers — so transient losses (one node
// briefly unreachable) don't shrink durable buckets.
func (s *Server) watchStreamReplicas(ctx context.Context) {
	ticker := time.NewTicker(streamReplicasInterval)
	defer ticker.Stop()

	s.reconcileStreamReplicas(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileStreamReplicas(ctx)
		}
	}
}

// reconcileStreamReplicas performs one pass: list streams, compute the
// target replica count for each, UpdateStream if the current value is
// below the target. Errors are logged and skipped — the next tick
// retries, and a single bad stream must not stall the rest.
func (s *Server) reconcileStreamReplicas(ctx context.Context) {
	js, err := jetstream.New(s.nc)
	if err != nil {
		log.Warn().Err(err).Msg("stream-replicas: jetstream init failed")
		return
	}

	infos := js.ListStreams(ctx)
	for info := range infos.Info() {
		if info == nil {
			continue
		}
		target := s.targetReplicasFor(info.Config.Name)
		if target == 0 || target <= info.Config.Replicas {
			continue
		}
		s.upgradeStreamReplicas(js, info, target)
	}
	if err := infos.Err(); err != nil {
		log.Warn().Err(err).Msg("stream-replicas: listing streams failed")
	}
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

// upgradeStreamReplicas issues a single UpdateStream. ErrJetStream
// errors (10074 in particular) just mean the cluster still can't
// place the requested count — that's fine, the next tick will try
// again. We log at Info on success so growth events are visible.
func (s *Server) upgradeStreamReplicas(js jetstream.JetStream, info *jetstream.StreamInfo, target int) {
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
		return
	}
	log.Info().Str("stream", cfg.Name).Int("from", from).Int("to", target).Msg("stream-replicas: upgraded")
}
