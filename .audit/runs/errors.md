# RUNS error log

Each error encountered during a multi-cycle resilience run gets one entry
here. Format:

```
## YYYY-MM-DD HH:MM — Phase {A|B|C}, step [k], symptom slug

**Symptom:** one-line description of what failed in operator-visible
terms (degrade output, dashboard view, log spam).

**Cluster state at failure:** N nodes alive before kill, which one was
killed, which streams stalled, which KV bucket lost quorum, etc.

**Investigation:** what we read live from NATS docs / nats-server source
(cite commit SHAs / docs URLs) and what hypothesis grounded in those
sources we settled on.

**Patch:** what changed in our tree, one bullet per file. ONE patch per
iteration per RUNS.md §3.1.

**Outcome:** PASS (3 consecutive degrade runs converged) or REVERT (root
cause unchanged → patch reverted per §3.2).
```

The history grows append-only — fixed errors stay in the file so future
runs see what was tried before. Add the **Outcome** line only after the
next live run actually verifies the fix.

---

## 2026-06-10 02:43 — Phase A, step [2], no reconvergence after 2nd leader-kill

**Symptom:** `start.sh degrade` stuck at step [2]. After killing 4-node
cluster's first leader (dev-node-2) and observing stab=1 on new leader
dev-node-1, killing dev-node-1 produced `wait_converged` timeout: no new
leader elected within 300 s. Surviving nodes dev-node-3 and dev-node-4
left with `cluster.leader=""`, `nodes_total=0`, repeated
`asty.v1.cluster.peer_announce`-side advisories
`$JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.KV_asty-cluster` /
`KV_asty-leader` / `KV_authms_refresh_tokens` / `KV_xhttp_cache`. NATS
log: `JetStream cluster no metadata leader`.

**Cluster state at failure:** 4-node cluster, system_kv_replicas=5
capped to 4 at start, app_kv_replicas=3. After step [1] reaping
dev-node-2 from meta and lowering system streams to R=3, KV_asty-leader
should have been placed on {1,3,4}. After killing dev-node-1 the meta
should have had {3,4} alive of {1,3,4} = 2 of 3 = quorum. Instead NATS
reported "no metadata leader" → meta-cluster size was still seen as the
full 4 by the survivors, putting them at 2 of 4 = below quorum.
Survivor-side natssolo correctly skipped collapse (the other survivor
was TCP-reachable, so this is the not-alone branch).

**Investigation:** Live NATS docs + 2.14.0→2.14.2 changelog diff via
`gh api repos/nats-io/nats-server/compare/v2.14.0...v2.14.2`.
- #8258 "Peer set desync/re-add after stream peer-remove" — fixed in
  2.14.2; previously a SERVER.REMOVE'd peer could be auto re-added.
  Not our trigger (we yank via SIGKILL, no heartbeats).
- #8253 "Stream and consumer scale down consistency" — UpdateStream
  scale-down now correctly keeps leader+online peers; this matters for
  the 4→3 system-stream lower.
- #92cf2e3 "Filestore only stores last block when MaxMsgsPerSubject 1"
  — fixes block-tracking lookups after restart specifically for
  MaxMsgsPerSubject=1 (i.e. KV History=1).
- Synadia blog "Renew NATS KV Key TTL" + docs.nats.io discussion #7264
  — bucket-wide TTL (MaxAge) takes precedence over per-key TTL, and any
  successful write refreshes the entry at the bucket level.
- b174307 commit (live-verified 2026-06-09): on nats-server 2.14.x,
  `History>1 silently disables the per-key Nats-TTL on the stream`.

asty-leader bucket was configured with History=5; asty-cluster was
already migrated to History=1 + LimitMarkerTTL. The 2.14.2 fixes
(#92cf2e3 etc.) only kick in on History=1, so asty-leader was the only
KV in the system that did NOT benefit from the 2.14.2 filestore
correctness for KV semantics. Hypothesis: under the back-to-back leader
SIGKILL the asty-leader stream's filestore lookups returned a stale
block (the bug #92cf2e3 fixes), and the campaign Get→Create cycle never
observed the real head, so neither survivor managed to claim the slot.
Match RUNS.md §-1.1: no timers added or removed; the change is a config
constant.

**Patch:** `asty/internal/ops/leader/election.go` — asty-leader bucket
History: 5 → 1, matching asty-cluster. Bucket-wide TTL (MaxAge=leaderTTL)
preserved.

**Outcome:** REVERT. Patch built clean, but the next Phase A run reproduced
the exact same wait_converged timeout at step [2]. The bucket-config tweak
did nothing for the meta-quorum failure. Reverted via
`git checkout asty/internal/ops/leader/election.go`.

---

## 2026-06-10 03:33 — Phase A, step [2..3] race between STREAM.PEER.REMOVE and SERVER.REMOVE

**Symptom:** identical to the prior entry — the meta cluster ended up
with no leader after the second back-to-back leader-kill, and the
surviving nodes all reported "JetStream cluster no metadata leader"
indefinitely.

**Cluster state at failure:** same as above. After re-reading the
reconcile pipeline I noticed `reconcileCluster` ran TWO peer-removal
machinery in series within ONE pass:

  1. `reconcileStreamReplicas` →
     `repairStreamPlacement(info)` → per-stream
     `$JS.API.STREAM.PEER.REMOVE.<stream>` for every offline
     replica (server/streamplacement.go), then
  2. `reapDeadPeers` → meta-wide
     `$JS.API.SERVER.REMOVE` for every offline meta peer.

`$JS.API.SERVER.REMOVE` already evicts the peer from the entire meta
cluster (which includes every stream RAFT group, since they are sub-
groups of the meta) and triggers NATS's own auto-reassignment of that
peer's replicas to live nodes — the per-stream REMOVE pass on top of
it is REDUNDANT and races with the meta-level eviction on the same
dead peer at the stream-RAFT layer. Confirms the user's "лишний
оверхед, который будет создавать гонку" hint: this is exactly that
overhead.

**Investigation:** No NATS-docs / source needed — the redundancy was
visible in our own `reconcileCluster` once we read the docstring of
`$JS.API.SERVER.REMOVE` alongside `$JS.API.STREAM.PEER.REMOVE`.
docs.nats.io confirms SERVER.REMOVE evicts the peer cluster-wide and
JetStream re-places its replicas.

**Patch:** removed the entire per-stream removal path:
  - deleted `asty/internal/server/streamplacement.go`
  - removed the `s.repairStreamPlacement(info)` call from
    `reconcileStreamReplicas` (`asty/internal/server/streamreplicas.go`)
    plus the docstring justifying it.

ONE patch per RUNS.md §3.1; no other changes.

**Outcome:** PASS. Three consecutive `start.sh 4` + `start.sh degrade`
runs converged through 4→3→2→1 with stab=1 reported at every step and
a single-node survivor serving `nodes_total=1` at the end:

  - run #1 (03:31): leaders dev-node-2 → 4 → 1 → 3
  - run #2 (03:34): leaders dev-node-2 → 1 → 4 → 3
  - run #3 (03:37): leaders dev-node-2 → 4 → 3 → 1

Phase A green. Moving to Phase B.
