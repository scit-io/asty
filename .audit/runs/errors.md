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

---

## 2026-06-10 07:00 — Phase B + Phase C cycle 2..3, transient `"leader":""` in dashboard JSON

**Symptom:** Phase A 3/3 passed but Phase B sporadically aborted with
`[N] no leader reported via <seed> — aborting` mid-degrade, even though
the previous step's wait_converged had just confirmed the new leader. A
direct curl to the same dashboard a few seconds later showed a healthy
leader. The dashboard handler is `/dashboard/v1/` (status.go).

**Cluster state at failure:** the asty-cluster bucket's RAFT group was
mid-election while the dashboard request was in flight. status.go's
`fetchClusterJSON` called `ClusterState().ListNodes()` directly, which is
a `snapshotKVByPattern("node.*")` watch-and-drain on the live bucket —
i.e. it blocks until the bucket returns the full key history. Under
quorum churn this drains slowly enough to exceed the test script's 3 s
curl timeout; curl gives up with empty body, the script greps the empty
body and finds `"leader":""`, aborts.

**Investigation:** `streamHub.Snapshot()` already publishes the same
`Cluster.Leader` / `Cluster.NodesTotal` / `Cluster.NodesHealthy` payload,
served from an in-memory map (sub-millisecond), fed by WatchNodes +
WatchAllocations + a new WatchLeadership-fed leader cache. The direct-KV
path in `fetchClusterJSON` was redundant overhead — and the racy one.

**Patch:**
- `asty/internal/ops/leader/election.go` — Election now caches the
  leader info from KV-watch events; GetLeader returns the cache without
  touching KV on the hot path.
- `asty/internal/ops/leader/watch.go` — WatchLeadership feeds the
  cache on every update + delete, with an end-of-replay primer that
  doesn't clobber a real entry seen during replay.
- `asty/internal/api/dashboard/status.go` — `fetchClusterJSON` now
  reads `StreamHub().Snapshot().Cluster` and drops the per-call
  `ListNodes()` + `GetLeader()` round-trip.

Phase A 3/3, Phase B, and Phase C cycle 1 all pass. Phase C cycle 2 hit
a SEPARATE failure (server-side concurrent-map crash, recorded below);
the dashboard-snapshot fix stands.

**Outcome:** PARTIAL PASS — dashboard transient gone. Cycle 2 failure
moves to the next entry.

---

## 2026-06-10 07:24 — Phase C cycle 2 grow iter 1, server `fatal: concurrent map iteration and map write`

**Symptom:** Phase C cycle 1 walked 1→16→1 cleanly. Cycle 2's first
`start.sh add` aborted with `no live cluster node to seed from`. Probing
the surviving node (dev-node-14) showed only the agent process up — the
SERVER process had crashed. Log tail:

  fatal error: concurrent map iteration and map write
  ...
  asty/internal/core/types.MarshalStartCommand(...)
  asty/internal/server.(*Server).sendStartCommand(...)

The reconciler dispatched a CmdStart for some service; CBOR-encoding
the ServiceDefinition's `Env` map ran concurrently with another goroutine
writing to that same map.

**Cluster state at failure:** server-14 was holding leadership after the
1-node survival, multiple in-flight dispatches were queued for the
services the reconciler was re-placing on the lone survivor.

**Investigation:** `server/commands.go::sendStartCommand` /
`sendRestartCommand` both call `kvEnvForAllocation(svc, kvEnv)` BEFORE
the resolved-copy step. The "resolved copy" is just `resolved := *svc`
— a shallow copy that shares the underlying map references. So every
caller that holds the same `*ServiceDefinition` (the reconciler workers,
the dashboard restart handler, the deployer) mutates the SAME
`svc.Env` map while another goroutine's MarshalStartCommand iterates
it. The comment on `kvEnvForAllocation` claimed "ServiceDefinition is
freshly loaded for each deploy and not shared" — false in practice,
the loader caches the same pointer and the controller workers share it.

**Patch:** `asty/internal/server/commands.go` —
- `resolvedSvcForDispatch` now deep-copies the `Env` map alongside the
  shallow struct copy.
- `sendStartCommand` and `sendRestartCommand` reorder so the
  per-dispatch `resolved` copy is made FIRST, then `kvEnvForAllocation`
  writes into `resolved.Env` (private to this dispatch) instead of
  `svc.Env` (shared with every other in-flight worker).

`go test -race ./asty/internal/server/...` passes.

**Outcome:** PASS. After the Env-map fix, Phase A 3/3 + Phase B + Phase C
cycles 1..8 all walked 1→16→1 cleanly with stab=1 at every step and a
single-node survivor serving `nodes_total=1`. Cycle 9 hit a DIFFERENT
failure mode (recovery exceeded the script's 300 s per-step budget — the
cluster did keep recovering on its own afterwards; a healthy
`leader=dev-node-72, nodes_healthy=12` was visible on the dashboard
shortly after the abort). Tracked separately as the next entry.

---

## 2026-06-10 10:33 — Phase C cycle 9, step [4], recovery exceeds 300 s budget (not a crash)

**Symptom:** Phase C cycles 1..8 all walked 1→16→1 cleanly. Cycle 9 step
[4] killed dev-node-82 from a 13-node cluster and `wait_converged`
timed out with empty output after 300 s. Unlike earlier failures the
cluster was NOT crashed — minutes after the script aborted, the
dashboard on a survivor returned a healthy snapshot: `leader=dev-node-72,
nodes_total=12, nodes_healthy=12, stab=1`. No server panic anywhere
across `/tmp/asty-dev-server-*.log`.

**Cluster state at failure:** 12 nodes alive of an expected 12 after the
step-[4] kill; meta cluster recovery (SERVER.REMOVE of dev-node-82,
leader re-election, stream UpdateStream from R=5 to whatever the new
clusterSize warrants) took longer than the test budget but completed.
The asty server's own clusterHealed/stab=1 returned eventually.

**Investigation:** _open — root cause not yet identified. Hypothesis:
after several thousand heartbeat writes, KV-watch updates, and dead-peer
SERVER.REMOVE round-trips accumulated across cycles 1..8, the meta
cluster's recovery walltime on a single-process-per-node dev cluster on
macOS slows enough that a deep-step kill on cycle 9 can exceed 300 s.
Production-class hardware has not been measured. Investigation continues
on a separate pass._

**Patch:** _none yet — recording the boundary so the next run starts
from a known position._

**Outcome:** _pending — Phase C cycles 1..8 all green; cycle 9
investigation deferred._
