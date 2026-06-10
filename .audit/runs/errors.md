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

---

## 2026-06-10 04:16 — Phase C, cycle 3, step [8], transient empty leader during degrade read

**Symptom:** Phase C cycle 3 grew 1→16 cleanly, then 7 degrade steps converged
with stab=1 at every step (live=16→15→14→13→12→11→10), and step [8] aborted
with `[8] no leader reported via 127.0.0.15 — aborting`. Cycles 1 and 2 of
Phase C passed; the failure is intermittent. Querying the dashboard a few
seconds later showed a healthy `leader=dev-node-19`, 9 nodes, normal snapshot:
the cluster itself was fine, the script captured a transient empty-leader
window in the snapshot.

**Cluster state at failure:** 9 nodes alive after step [7]'s kill. Per
`grep "became cluster leader\|lost leadership" /tmp/asty-dev-server-*.log`,
leadership flipped repeatedly across the run: e.g. server-7 became leader
03:41:51, refreshed every 5 s through 03:42:51, then at 03:42:58 logged
"lost leadership new_leader=dev-node-8 old_leader=dev-node-7". A 7 s gap
between the last successful refresh and "lost leadership" is shorter than
the 10 s leaderTTL, so the entry can't have expired naturally — node 7
relinquished leadership itself on a refresh failure during cluster churn.

**Investigation:** Re-read `ops/leader/campaign.go`. The refresh path
hard-codes self-step-down on ANY Put error:

```go
if _, err := e.bucket.Put(ctx, leaderKey, data); err != nil {
    e.isLeader = false                       // <-- step down here
    return fmt.Errorf("failed to refresh leadership: %w", err)
}
```

A transient `nats: no response from stream` during the asty-leader RAFT
group's own churn (a common occurrence while 16 nodes are joining or
leaving) immediately marks the holder as "not leader" — but the KV entry
is still alive (TTL not expired), so:

  - the next campaign tick on the same node hits the entry-exists CAS
    rejection (`err_code=10071 wrong last sequence`) and stays "not
    leader",
  - other nodes' tryBecomeLeader still sees the original holder in KV
    so they don't claim either,
  - until the TTL eventually expires, NOBODY is leader from the
    process-state perspective even though KV says one specific node
    holds it.

That mismatch is the source of the script's transient empty-leader read.
WatchLeadership is already the authoritative KV-driven leadership tracker;
it fires onBecomeLeader/onLoseLeadership on real KV state changes. The
in-process `e.isLeader=false` shortcut in refresh is an extra source of
truth that races the KV state — exactly the "одна source of truth, NATS-
idiomatic" rule in CLAUDE.md (`feedback_nats_idiomatic_single_source`).

User hint "лишний оверхед, который будет создавать гонку — выпилить" maps
directly onto this self-step-down line: pure overhead, races the KV.

**Patch:** `asty/internal/ops/leader/campaign.go` — drop the
`e.isLeader = false` line from `refreshLeadership`. A failed Put just
returns the error and waits for the next tick. WatchLeadership remains
the only writer of leadership state changes from the KV.

**Outcome:** REVERT. Phase A.1 passed twice with the patch but A.2 reproduced
the original 4→3 step [2] no-reconvergence pattern. Removing self-step-down
on Put failure changed how leadership flapping looks but did not eliminate
the underlying KV churn that drives the failure. Reverted with
`git checkout asty/internal/ops/leader/campaign.go`.

---

## 2026-06-10 04:47 — Phase C cycle 2/3 step [2..8], transient ErrKeyNotFound on GetLeader

**Symptom:** During Phase C 16-node degrade cycles, after a successful
convergence (degrade_wait_converged returned with new leader and stab=1),
the very next iteration's `degrade_snapshot` returned a JSON payload with
`"leader":""` — even though the dashboard queried a second or two later
showed a healthy leader. Cycle 3 step [8] and cycle 2 step [2] hit this.

First attempt: leader-info CACHE fed by WatchLeadership (`GetLeader`
returns cached value populated from KV-watch updates, no per-call KV read).
Built clean. PHASE A 3-of-3 passed, Phase B passed, Phase C cycle 1
passed. Phase C cycle 2 reproduced the same `"no leader reported"` abort
at step [2]. The cache did not help because the watch event for the new
leader arrived AFTER `degrade_wait_converged` confirmed the leader via a
direct dashboard GET, so step [2]'s next read raced the cache-update path
the same way the prior code raced the KV.Get. REVERT.

**Cluster state at failure:** 16-node cluster, one step into degrade.
Watch on 127.0.0.15 had not yet processed the new-leader claim event
(or had processed a transient delete-marker first) at the moment the
script polled the dashboard.

**Investigation:** Cross-referenced the 2.14.2 changelog one more time
(`gh api repos/nats-io/nats-server/compare/v2.14.0...v2.14.2`). Commit
92cf2e3 "[FIXED] Filestore only stores last block when MaxMsgsPerSubject
1" stood out: the patch fixes a filestore block-tracking bug where the
first-block pointer could lag behind the last-block pointer, causing
`Get`-style lookups to read an obsolete block — i.e. ErrKeyNotFound on
an entry that logically exists, exactly the transient our test hits.
The fix is gated on `MaxMsgsPerSubject == 1`. A KV bucket with History=N
maps to MaxMsgsPerSubject=N, so asty-leader (History=5) does NOT benefit
from the 2.14.2 fix; asty-cluster (History=1) does. The b174307 commit
already called out that History>1 is incompatible with the 2.14.x KV
semantics; the same is true for filestore correctness.

Asty never reads past revisions of `current-leader` — History=5 was pure
overhead, exactly the kind of legacy carry-over the user warned about.

**Patch:** `asty/internal/ops/leader/election.go` — asty-leader bucket
History: 5 → 1 (and a comment citing 92cf2e3 so the next reader knows
not to bump it back up). Bucket-wide TTL preserved.

**Outcome:** PASS for the original step [8] / step [2] empty-leader transient,
and for everything earlier in the run. Three consecutive `start.sh 4 +
start.sh degrade` runs converged through 4→3→2→1 with stab=1 at every step;
Phase B grew 1→8 and shrank 8→1 cleanly; Phase C cycles 1 and 2 both walked
1→16→1 with no missed convergence. Phase C cycle 3 reproduced a DIFFERENT
failure at step [14] (3-node → 2-node transition, no reconvergence within
300 s), tracked separately in the next entry. The History=5→1 fix stands;
the cycle-3 step-14 stall is its own root-cause investigation.

---

## 2026-06-10 06:11 — Phase C cycle 3, step [14], 3→2 transition no reconvergence

**Symptom:** After Phase C cycle 1 and 2 walked cleanly through 16→1, cycle
3 grew 1→16 normally and degrade ran 13 steps cleanly. Step [14] killed the
current leader (dev-node-9) from a 3-node cluster and waited 300 s on SSE
for reconvergence; `wait_converged` timed out with empty output. Surviving
nodes were dev-node-10 and dev-node-16. Dashboard read (long after the
abort) showed `nodes_total=0`, no leader, QUORUM_LOST advisories on KV_*.

**Cluster state at failure:** 2 nodes alive of 3 expected after kill; meta
cluster apparently never elected a new leader for the survivors. Server-10
became leader once at 06:12:01 and successfully reaped dev-node-9 from the
meta one second later — then went silent for 7 minutes (no further refresh
logs, no "lost leadership" message), and at 06:19:42 the server-10 process
was running a FRESH "starting asty server" boot sequence. That's not a
crash-and-restart by start.sh (no supervisor for the asty server in
deploy/dev/start.sh), so something else induced the re-init — likely an
inbound CmdShutdown reach-around or the agent's own restart on a fatal
condition. The 2-survivor cluster never re-stabilised; KV_asty-cluster /
KV_asty-leader / KV_xhttp_cache / KV_authms_refresh_tokens stayed
QUORUM_LOST after that point, with natssolo correctly skipping the
collapse because the other survivor was still TCP-reachable.

Distinguishing from Phase A's 3→2 case: Phase A starts fresh, this is the
third cycle of grow/degrade on the SAME cluster, ~2 hours of accumulated
KV traffic and meta-peer churn. ready_nodes=12 on server-10's later
restart (when nodes_alive=2) shows the cluster KV still held stale
NodeInfo entries for departed nodes well past their nodeKVTTL — TTL renew
on per-key was blocked by the lost-quorum bucket, exactly the chicken-and-
egg case the b174307 commit message warns about.

**Investigation:** _open — root cause not yet identified. The History=5→1
patch from the previous entry is now committed, and the cycle-3 stall is a
distinct failure with its own follow-up needed._

**Patch:** _none yet_

**Outcome:** _pending — investigation continues; if RUNS.md is to be
satisfied, this failure has to be fixed and the entire ladder (Phase A 3×
+ Phase B + Phase C 12 cycles) re-run from scratch._
