package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/minio/highwayhash"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// bootstrapReachableProbeTimeout caps each TCP probe of a bootstrap
// peer's cluster port. Tight on purpose: a live joiner has :6222 open
// (or is opening it), so the probe succeeds in <10 ms on loopback. A
// dead peer's port is shut and the probe returns ECONNREFUSED
// immediately, or unrouteable IP times out. Only called in the
// QUORUM_LOST gate, never on the hot path.
const bootstrapReachableProbeTimeout = 250 * time.Millisecond

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
// See REPLICAS_KV.md.
//
// The grow-vs-shrink differentiation is done by NATS RAFT timing alone:
// see handleQuorumLost below — peerCount stays 1 across one
// lostQuorumInterval (10s) iff this is a genuine shrink; a joining peer
// shows up within that window. No local "bootstrap" mirror is consulted,
// because under N=2 unplug the KV-watch that feeds it is dead and the
// mirror would lie indefinitely.

// quorumLostSubject is the JetStream advisory NATS pushes when a stream
// can no longer reach RAFT quorum.
const quorumLostSubject = "$JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.>"

// numReplicasRe matches the replica count in a stream's meta.inf JSON.
var numReplicasRe = regexp.MustCompile(`"num_replicas":\d+`)

// watchQuorumLost subscribes to the quorum-lost advisory and collapses
// this node to standalone when it is the survivor of a 2-node cluster.
//
// Uses SubscribeSync (not the async-callback Subscribe) because in
// practice the async dispatcher silently dropped these advisories on
// our agent connection — an external probe with the SAME creds + same
// subject on the SAME nats-server received them just fine, while the
// agent's async callback never fired (verified live, all 12 agents
// in a fresh N=12→K cluster: start=1, ok=1, fired=0). A sync read
// loop in our own goroutine bypasses that dispatcher entirely.
func (a *Agent) watchQuorumLost(ctx context.Context) {
	log.Info().Str("subject", quorumLostSubject).Msg("quorum-lost watcher: starting subscribe")
	sub, err := a.nc.SubscribeSync(quorumLostSubject)
	if err != nil {
		log.Warn().Err(err).Msg("quorum-lost watcher: subscribe failed; 2→1 auto-collapse disabled")
		return
	}
	if err := a.nc.Flush(); err != nil {
		log.Warn().Err(err).Msg("quorum-lost watcher: flush after subscribe failed")
	}
	log.Info().Str("subject", quorumLostSubject).Msg("quorum-lost watcher: subscribe OK, awaiting advisories")
	defer func() { _ = sub.Unsubscribe() }()

	for ctx.Err() == nil {
		m, err := sub.NextMsg(2 * time.Second)
		if err != nil {
			if err == nats.ErrTimeout {
				continue
			}
			if err == nats.ErrConnectionClosed || err == nats.ErrBadSubscription {
				log.Warn().Err(err).Msg("quorum-lost watcher: subscription ended")
				return
			}
			log.Debug().Err(err).Msg("quorum-lost watcher: NextMsg transient")
			continue
		}
		log.Info().Str("advisory", m.Subject).Msg("quorum-lost watcher: callback fired")
		a.handleQuorumLost(m)
	}
}

// handleQuorumLost decides whether this advisory is a genuine SHRINK
// to standalone (collapse) or just a transient quorum loss with peers
// still alive (skip).
//
// Event-driven, no timer: the gate is one TCP probe against each known
// peer's cluster port. ECONNREFUSED / timeout means "peer dead" (event
// from the kernel); SYN-ACK means "peer alive" (event). If any known
// peer answers right now, other survivors exist and the meta cluster
// will re-form on its own — collapsing would split-brain. Only when
// EVERY known peer is unreachable does this node go solo.
//
// Why TCP and not JSZ: under a leader-kill cascade the local nats-
// server's meta_cluster view fragments transiently — every survivor's
// JSZ briefly shows itself alone — but their cluster ports stay open
// the whole time. JSZ would say "alone" → collapse on all of them →
// split-brain. The TCP probe sidesteps that by asking the OS directly.
//
// 1→N grow safety: a joining peer's nats-server has :6222 open as soon
// as it starts (before it shows up in any other peer's JSZ). hasAny-
// ReachablePeer detects that and skips the collapse.
//
// `peers.allPeerIPs()` returns bootstrap ∪ byNode; bootstrap is seeded
// at start from $A_NATS_SEED + CmdAddPeer announces, byNode comes from
// the cluster-KV WatchNodes. Under quorum loss KV stalls but bootstrap
// still has every IP this node ever heard about — exactly the addresses
// to probe.
func (a *Agent) handleQuorumLost(m *nats.Msg) {
	if !a.clusteredNow() {
		return
	}
	if a.hasAnyReachablePeer() {
		log.Debug().Str("advisory", m.Subject).
			Msg("quorum-lost: a known peer is TCP-reachable; not alone, skipping collapse")
		return
	}
	log.Warn().Str("advisory", m.Subject).
		Msg("cluster lost quorum and no peer reachable: collapsing to single-node (KV reused from disk at R=1)")
	a.signalNATSSolo()
}

// performSoloTransition runs inside the supervisor after it has stopped
// the clustered child: drop the meta RAFT log (so the standalone restart
// becomes equivalent to a fresh single-node cold start), force every
// on-disk stream to R=1, then bring nats-server back up standalone on
// the same store. peers.reset() makes the rendered conf standalone
// (no cluster{} block); bootstrapNATS reuses the store dir, so the
// now-R=1 streams load and serve, and a fresh meta cluster is created
// with this node as its single peer — same shape as `start.sh 1`.
//
// Why drop the meta RAFT log: without this, the persisted $SYS/_js_/
// _meta_/ directory still remembers the previous incarnation's peer
// set (12 peers, of which only 1 — us — is alive). After bootstrap
// the standalone node loads that state and thinks it is part of a
// 12-peer meta cluster. When a fresh peer joins via `start.sh add`,
// the joining peer's KV consumer hits QUORUM_LOST against the old
// 12-peer meta with only 2 alive, and natssolo on the JOINING SIDE
// fires (countByNode==0 there) — the joiner self-collapses into a
// separate standalone cluster. Result: split-brain. By removing the
// stale meta RAFT state at solo transition, the survivor becomes
// indistinguishable on disk from a `start.sh 1` boot.
func (a *Agent) performSoloTransition(ctx context.Context) error {
	a.peers.reset()
	a.forceStreamsToR1()
	a.dropAllRAFTState()
	// Mark ourselves as "in solo recovery". watchNATSPeers will drop
	// late KV-watch upserts for now-dead peers that would otherwise
	// reintroduce them via byNode and trigger a spurious cold-restart
	// back into clustered mode. The flag is cleared the moment an
	// explicit incoming-peer signal arrives (CmdAddPeer / peer-announce)
	// — those are the only legitimate ways to leave solo mode.
	a.inSolo.Store(true)
	return a.bootstrapNATS(ctx)
}

// dropAllRAFTState walks the on-disk jetstream tree and removes every
// `_meta_` directory it finds. peerIDs are persisted in $SYS/_js_/_meta_,
// per-stream streams/*/_meta_, AND per-consumer streams/*/consumers/*/_meta_.
// Dropping just $SYS/_js_/_meta_ was not enough — observed live 2026-06-09
// that the dead-peer peerID survived the standalone restart and reappeared
// in cluster_size on the next regrow, because NATS rebuilt meta-cluster
// peers from stream/consumer RAFT logs left on disk. Best-effort: a
// missing directory is not fatal because bootstrapNATS will still bring
// up nats-server.
func (a *Agent) dropAllRAFTState() {
	root := filepath.Join(a.jetStreamStoreDir(), "jetstream")
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries; best-effort
		}
		if info.IsDir() && info.Name() == "_meta_" {
			if err := os.RemoveAll(path); err == nil {
				count++
			}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		log.Warn().Err(err).Str("root", root).Msg("solo: walking jetstream tree failed")
		return
	}
	log.Info().Str("root", root).Int("dropped", count).Msg("solo: dropped all _meta_ RAFT state under jetstream/")
}

// dropMetaRAFTLog removes the JetStream system-account state directory
// ($SYS/_js_/) so the next standalone restart starts with a completely
// fresh meta cluster — equivalent to a clean cold start. Dropping just
// _meta_/ was not enough: observed live 2026-06-09 a phantom peerID
// (sFjW3TF5, "Server name unknown") survived under $SYS/_js_/ in sibling
// directories and inflated cluster_size on the rebuilt single-node
// meta, so the next 1→N regrow targeted R=3 on a 2-node cluster and
// the leader's UpdateStream rejected with err_code=10005 "no suitable
// peers for placement". Best-effort: a missing directory or read error
// is not fatal because bootstrapNATS will still bring up nats-server.
func (a *Agent) dropMetaRAFTLog() {
	dir := filepath.Join(a.jetStreamStoreDir(), "jetstream", "$SYS", "_js_")
	if err := os.RemoveAll(dir); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("solo: failed to drop $SYS/_js_/ state")
		return
	}
	log.Info().Str("dir", dir).Msg("solo: dropped $SYS/_js_/ so restart looks like a fresh single-node boot")
}

// forceStreamsToR1 rewrites num_replicas→1 in every account stream's
// on-disk meta.inf (and recomputes meta.sum), AND drops each stream's
// `_meta_/` RAFT log so a standalone server loads them with no RAFT
// history. The data dirs (`msgs/`) stay intact — only the per-stream
// replication-group state is reset. Observed live 2026-06-09 that
// dropping just `$SYS/_js_/` was not enough: every joiner brought back
// a phantom peerID ("Server name unknown") into meta_cluster, inflating
// cluster_size by 1 per join and pushing target replicas above the
// available node count (err_code=10005 "no suitable peers for
// placement"). The phantoms live in per-stream RAFT logs that NATS
// carries forward across the standalone rebuild.
func (a *Agent) forceStreamsToR1() {
	glob := filepath.Join(a.jetStreamStoreDir(), "jetstream", "*", "streams", "*", "meta.inf")
	matches, err := filepath.Glob(glob)
	if err != nil {
		log.Warn().Err(err).Msg("solo: globbing streams failed")
		return
	}
	for _, metaPath := range matches {
		stream := filepath.Base(filepath.Dir(metaPath))
		streamDir := filepath.Dir(metaPath)
		changed, err := forceStreamMetaToR1(metaPath, stream)
		switch {
		case err != nil:
			log.Warn().Err(err).Str("stream", stream).Msg("solo: rewriting meta.inf to R=1 failed")
		case changed:
			log.Info().Str("stream", stream).Msg("solo: forced stream to R=1 on disk")
		}
		raftDir := filepath.Join(streamDir, "_meta_")
		if err := os.RemoveAll(raftDir); err != nil {
			log.Warn().Err(err).Str("stream", stream).Msg("solo: dropping per-stream _meta_ failed")
			continue
		}
		log.Info().Str("stream", stream).Msg("solo: dropped per-stream RAFT history")
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

// hasAnyReachablePeer reports whether ANY known peer (byNode ∪ bootstrap)
// answers on its nats cluster port. Used by the QUORUM_LOST gate to tell
// "we are alone, collapse" from "someone is still out there, don't collapse"
// without relying on byNode/bootstrap freshness (both can be stale when
// per-key TTL deletes are blocked by a below-quorum KV bucket).
func (a *Agent) hasAnyReachablePeer() bool {
	port := a.cfg.NATS.Server.Cluster.Port
	if port == 0 {
		// No clue what port to probe; fall back to the original byNode
		// + bootstrap presence check (stale or not, it's all we have).
		return a.peers.countByNode() > 0 || a.peers.hasBootstrap()
	}
	for _, ip := range a.peers.allPeerIPs() {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), bootstrapReachableProbeTimeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
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
