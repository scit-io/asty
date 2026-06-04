package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/minio/highwayhash"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Collapse to a single node when a 2-node cluster loses its peer.
//
// NATS JetStream cannot serve R>1 streams without quorum, and a standalone
// server refuses to load them at all (err 10074). But the data is intact
// on this node's disk — it was a full replica. So on the genuine 2→1
// transition we rewrite each stream's on-disk `num_replicas` to 1 (and
// recompute its `meta.sum` checksum), then restart nats-server standalone
// reusing the SAME store: the streams load at R=1 and KV keeps serving.
// No data copy, no in-RAM mirror.
//
// Trigger is event-driven, not polled: NATS pushes
// $JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.<stream> a few seconds after a
// peer dies and a stream can no longer reach RAFT quorum.
//
// ACCEPTED LIMITATION — 2-node split-brain. On a true network partition
// (both nodes alive but unable to see each other) both lose quorum and
// both collapse, diverging. This is a deliberate trade-off for the
// 2-node case; real safety needs a third node or an external arbiter.
// See REPLICAS_KV.md. The collapse is therefore GATED to genuine 2→1
// (countByNode ≤ 1): at N≥3 a QUORUM_LOST advisory means we are a
// partitioned minority that RAFT correctly stalls, and the majority keeps
// serving — collapsing there would be split-brain, so we ignore it.

// quorumLostSubject is the JetStream advisory NATS pushes when a stream
// can no longer reach RAFT quorum.
const quorumLostSubject = "$JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.>"

// numReplicasRe matches the replica count in a stream's meta.inf JSON.
var numReplicasRe = regexp.MustCompile(`"num_replicas":\d+`)

// watchQuorumLost subscribes to the quorum-lost advisory and collapses
// this node to standalone when it is the survivor of a 2-node cluster.
// Event-driven (NATS push) — no polling. Lives for the agent's lifetime;
// the subscription survives the supervised nats-server restarts because
// the agent's client reconnects.
func (a *Agent) watchQuorumLost(ctx context.Context) {
	sub, err := a.nc.Subscribe(quorumLostSubject, func(m *nats.Msg) {
		// Only the genuine 2→1 case. At N≥3 this advisory also reaches a
		// partitioned minority — but there RAFT stalls us and the majority
		// keeps serving, so collapsing would split-brain. Skip it.
		if !a.clusteredNow() || a.peers.countByNode() > 1 {
			return
		}
		log.Warn().Str("advisory", m.Subject).
			Msg("2-node cluster lost quorum: collapsing to single-node (KV reused from disk at R=1)")
		a.signalNATSSolo()
	})
	if err != nil {
		log.Warn().Err(err).Msg("quorum-lost watcher: subscribe failed; 2→1 auto-collapse disabled")
		return
	}
	defer func() { _ = sub.Unsubscribe() }()
	<-ctx.Done()
}

// performSoloTransition runs inside the supervisor after it has stopped
// the clustered child: force every on-disk stream to R=1, then bring
// nats-server back up standalone on the same store. peers.reset() makes
// the rendered conf standalone (no cluster{} block); bootstrapNATS reuses
// the store dir, so the now-R=1 streams load and serve.
func (a *Agent) performSoloTransition(ctx context.Context) error {
	a.peers.reset()
	a.forceStreamsToR1()
	return a.bootstrapNATS(ctx)
}

// forceStreamsToR1 rewrites num_replicas→1 in every account stream's
// on-disk meta.inf (and recomputes meta.sum) so a standalone server loads
// them. Only `<store>/jetstream/<account>/streams/*` is touched; the
// `SYS/_js_` RAFT state is left alone (standalone ignores it).
func (a *Agent) forceStreamsToR1() {
	glob := filepath.Join(a.jetStreamStoreDir(), "jetstream", "*", "streams", "*", "meta.inf")
	matches, err := filepath.Glob(glob)
	if err != nil {
		log.Warn().Err(err).Msg("solo: globbing streams failed")
		return
	}
	for _, metaPath := range matches {
		stream := filepath.Base(filepath.Dir(metaPath))
		changed, err := forceStreamMetaToR1(metaPath, stream)
		switch {
		case err != nil:
			log.Warn().Err(err).Str("stream", stream).Msg("solo: rewriting meta.inf to R=1 failed")
		case changed:
			log.Info().Str("stream", stream).Msg("solo: forced stream to R=1 on disk")
		}
	}
}

// forceStreamMetaToR1 sets num_replicas to 1 in one meta.inf and rewrites
// its meta.sum. Returns false (no write) when the stream is already R=1
// or has no replica field. meta.sum is computed before the meta.inf is
// written so a checksum failure never leaves a half-edited stream.
func forceStreamMetaToR1(metaPath, stream string) (bool, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return false, err
	}
	edited := numReplicasRe.ReplaceAll(data, []byte(`"num_replicas":1`))
	if bytes.Equal(edited, data) {
		return false, nil
	}
	sum := metaSum(stream, edited)
	if sum == "" {
		return false, fmt.Errorf("meta.sum compute failed for %q", stream)
	}
	if err := os.WriteFile(metaPath, edited, 0o600); err != nil {
		return false, err
	}
	sumPath := filepath.Join(filepath.Dir(metaPath), "meta.sum")
	return true, os.WriteFile(sumPath, []byte(sum), 0o600)
}

// metaSum reproduces NATS's meta.sum: HighwayHash-64 of the meta.inf
// bytes, keyed by sha256(streamName), hex-encoded. Verified byte-for-byte
// against a real on-disk meta.sum.
func metaSum(stream string, meta []byte) string {
	key := sha256.Sum256([]byte(stream))
	hh, err := highwayhash.New64(key[:])
	if err != nil {
		return ""
	}
	_, _ = hh.Write(meta)
	return hex.EncodeToString(hh.Sum(nil))
}

// jetStreamStoreDir is the on-disk JetStream store of the supervised
// nats-server (store_dir from config; ${A_NODE_ID} already expanded at
// load). NATS keeps streams under `<store_dir>/jetstream/`.
func (a *Agent) jetStreamStoreDir() string {
	return a.cfg.NATS.Server.JetStream.StoreDir
}

// clusteredNow reports whether the live nats.conf carries a cluster{}
// block — i.e. this node is running clustered, so collapsing to solo is
// meaningful. A node that cold-started standalone never has one.
func (a *Agent) clusteredNow() bool {
	raw, err := os.ReadFile(a.natsConfPath())
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "cluster {")
}

// signalNATSSolo tells the supervisor to perform the standalone
// transition. Buffered drop-on-full — multiple streams fire QUORUM_LOST,
// but we collapse exactly once.
func (a *Agent) signalNATSSolo() {
	select {
	case a.natsSoloCh <- struct{}{}:
	default:
	}
}
