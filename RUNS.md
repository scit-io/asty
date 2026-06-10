# RUNS.md — protocol for cluster-resilience test runs

This is the **only** protocol for running multi-cycle N→1 / 1→N
resilience tests against the asty/NATS cluster. Follow it verbatim.
Do not improvise, do not skip steps, do not re-derive from memory.

## ⚠️ Hall-of-shame: 2026-06-10 NATS-canon violation

Before reading the protocol — KNOW that this exact protocol has
been broken by improvisation on NATS-related code in the past, and
the cost was catastrophic.

What happened: I rewrote `ops/leader/Election` with a homebrew
"watch-driven, Put-without-CAS, 10 s TTL" state machine instead of
matching the canonical pattern (ripienaar/nats-kv-leader-elect,
Create+Update-CAS, demote-on-fail, ≥30 s TTL, single goroutine —
the impl docs.nats.io points to). I then spent hours chasing the
symptoms of my own deviations (transient empty leader at C-3-[8],
no reconvergence at C-9-[14], leadership flap under 16-node degrade
load) and reverting correct fixes because they didn't make the
test green. The user had to interrupt and explicitly point me at
the canonical reference before I would read it. Then, in the
canonical rewrite itself, I introduced `watchRetryDelay = 2*time.
Second` — a polling back-off in the file written to be strictly
event-driven. The user caught that on the very next message too.

If you are reading this and considering an "optimization" on any
NATS-touching code path: STOP. Open the canonical reference.
Match it byte-for-byte on observable behavior. See
`memory/feedback_nats_canon_strict_no_improv.md` and the §-1.1
no-timers rule below. Both were known. Both were violated.

## -1. Hard constraints (read first, every iteration)

These two constraints supersede every other section. Violating
either invalidates the iteration and the run.

### -1.1 Strictly event-driven — NO timers visible

The orchestrator must react to **events** (NATS advisories, KV
watch updates, process callbacks, TCP-probe results). No
`time.Sleep`, `time.After`, `time.NewTimer`, `time.NewTicker`,
`time.AfterFunc` in production code paths that influence cluster
decisions (leadership, collapse, replica reconcile, dead-peer
reap, …). A user reading the code must NOT see a timer used to
"wait a bit" before deciding.

The accepted (and documented) exception list is small:
operation timeouts (a single bounded `ctx.WithTimeout` around
one I/O call), protocol keepalives (SSE/WS pings), HTTP-server
read/write deadlines, periodic-snapshot rebuilds that are
already documented in CLAUDE.md as event-driven-with-safety-net.
Anything ELSE that wants to "wait" is wrong; replace with an
explicit subscribe-and-wait on the matching advisory or KV
watch.

### -1.2 Commit only after three consecutive passing rechecks

A fix that resolves the immediate error is **not** ready to
commit. Run the failing phase THREE times in a row with the
fix in place. All three must pass cleanly. Only then `git commit`
(no push, no force-push). The point: a flaky pass once means
nothing; three in a row eliminates the obvious race.

If any of the three rechecks fails, the fix is wrong — revert
it (§3.1) and try a different hypothesis.

## 0. Primary reference: NATS specification (live, not cache)

**Every** decision touching cluster, JetStream, RAFT, KV, replicas,
quorum, leadership, advisories, or peer-membership is anchored
**first** in the NATS specification — fetched **live**, not from
training memory, not from cached prior conversation, not from
restated CLAUDE.md / REPLICAS_KV.md paraphrases.

Live = a fresh `WebFetch` against `docs.nats.io`, the relevant
ADR under `github.com/nats-io/nats-architecture-and-design`,
the `nats-server` source on GitHub, AND `nats.go` source — checked
in the current session, on the current pinned NATS version.
Forum/issue search (github.com/nats-io issues, nats.io/forum) is
also live. Memory entries are pointers to where to look, not
substitutes for looking.

Never form a hypothesis about NATS behavior before the live read
returns. "I remember it works like X" is not a hypothesis — it is
a fast-path that has produced wrong fixes in every prior session.

## 1. The full test specification

Run **incrementally**. Do not jump to 16 nodes from a cold start —
verify 4-node first, then 8, then 16.

### Phase A — 4-node smoke (must pass before B)

1. `start.sh 4` — bring up 4 nodes (server + agent on each).
2. `start.sh degrade` — N→1 degrade (SIGKILL leader every step,
   wait full reconvergence). Expected: 4→3→2→1, `stab=1` at every
   step, including the 2→1 collapse via natssolo.
3. Result is **PASS** only if every step reported `stab=1` and the
   final survivor serves `/dashboard/v1/` showing `nodes_total=1`.

### Phase B — 8-node smoke (must pass before C)

4. From the survivor (1 node), `start.sh add` 7 times → 1→8.
5. `start.sh degrade` → 8→1.
6. Result is PASS under the same criterion.

### Phase C — 16-node multi-cycle (the actual goal)

7. From the survivor (1 node), `start.sh add` 15 times → 1→16.
8. `start.sh degrade` → 16→1.
9. Repeat steps 7–8 **12 times** (12 cycles total at 16 nodes).
10. Result is PASS when all 12 cycles complete without error.

### Constraints during a run

- **Any error at any phase halts the run.** The very moment a
  step reports a failure — `wait_converged` timeout, `stab=0`
  past budget, missing leader, broken `add`, build error,
  unexpected log line — the run **STOPS**. Do not retry, do not
  patch around, do not press on to the next phase. The flow
  switches to the failure-investigation track below.
- **No `start.sh stop` during the run.** Allowed only AFTER an
  error has been detected and recorded. Mid-run stops invalidate
  the measurement.
- **N→1 = unplug = SIGKILL.** `start.sh degrade` uses `kill -9` on
  processes — server, agent wrapper, agent child, nats-server.
  Do **not** use dashboard kill, `start.sh kill <N>`, or anything
  that issues `CmdShutdown` first. Soft kill defeats the
  survivor-side-recovery measurement (see
  `memory/feedback_unplug_test_default.md`).
- **Incremental discipline.** If Phase A fails, do NOT run B or C.
  Diagnose per §0 + §3.0, fix the root cause, re-run A, only then
  proceed.

### Failure-investigation track (when a phase errors)

When a run errors, before any fix attempt:

1. **Capture the failure**. Snapshot logs, JSZ, KV state, last
   degrade-output. Pin exact step ([k] in degrade, which node
   was killed, what `wait_converged` saw last).
2. **Live NATS research, not cache.**
   - WebFetch the relevant docs.nats.io pages for the subsystem
     that failed (JetStream cluster, KV watcher, RAFT, advisories,
     SERVER.REMOVE, peer-remove, …).
   - WebFetch / `gh api` the relevant nats-server source files on
     the **pinned version** the cluster runs.
   - WebSearch nats.io forum + github.com/nats-io issues for the
     exact error string / advisory subject / behavior pattern.
   - Do NOT propose a hypothesis until this research is done in
     the current session. Past sessions don't count.
3. **Only then** form a hypothesis grounded in cited NATS sources.
4. Apply ONE minimal change (§3.1). Re-run the failing phase.
5. If the change did not fix the root cause, revert it (§3.2).

### After fixing any error — restart from Phase A.1

Once a fix is applied, **the entire run restarts from Phase A
step 1** (`start.sh 4`). No "continue where we left off." The
cluster state at the moment of failure is poisoned (mixed
versions of code, half-applied recovery, dangling KV entries);
keeping it would let an earlier bug mask the next one. Every
recovery iteration walks the same A → B → C ladder from the
beginning.

Sequence:

1. `start.sh stop` (allowed now — error was detected).
2. Rebuild: `go build -o bin/asty ./asty/cmd`.
3. Clean: `rm -rf /tmp/asty-dev/* /tmp/asty-dev-*.log`.
4. Restart from Phase A step 1.

A "small fix" does not get an exemption. The whole ladder runs.

## 2. Success criterion

The user personally writes `готово` after their own verification.
Until they do, the task is not solved. Nothing else (a green build,
a passing single run, a clean stat output) counts as completion.

## 3. Iteration rules (when something fails)

These are **load-bearing** — violating them is how patches get
piled and the cluster ends up in a tangle nobody can untangle.

### 3.0 Halt-then-research, always

A failed step is a STOP signal, not a try-again signal. The first
move is §1's failure-investigation track: capture + live NATS
research + cited hypothesis. Patch attempts before that step are
guesswork and have made things worse in every prior session.

### 3.1 One patch per iteration

Each fix attempt:

1. Form a hypothesis about the root cause.
2. Make ONE minimal change.
3. Build, restart the cluster, run the test.
4. Verify whether the patch SOLVED THE ROOT CAUSE — not just made
   one symptom go away.
5. If it did NOT solve the root cause → **revert THAT patch**
   before trying anything else. Do not extend, do not pile a
   second patch on top.

### 3.2 Partial revert, not session-wide revert

When reverting an unworkable patch, revert ONLY that patch. Keep
the other patches in the session that DID work. Do not sweep-revert.

### 3.3 Destructive git operations need backup first

Before any of these in a session with uncommitted work:

- `git checkout <ref> -- .` (wide pathspec)
- `git checkout <ref> -- <dir>` (covering more than one approved file)
- `git reset --hard`
- `git clean -fd`
- mass `rm -rf` on tracked files

…do all three of:

1. **Inventory in chat**: `git status --short` + one-line note per
   file ("experimental" / "approved this session" / "docs").
2. **Stash the approved subset**: `git stash push -m "..." -- <paths>`.
3. **Announce specifically** what will be destroyed and **wait for
   explicit confirmation** before running the destructive command.

A single-file `git checkout <ref> -- specific.go` is fine. The
wide-pathspec form is the dangerous one and has destroyed a week of
user-tested work in this project before (memory/`feedback_no_destructive_git_without_backup.md`).

### 3.4 Commit approval

Never `git commit` until the user has explicitly approved with
`+` / `коммить` / equivalent. Do not commit on assumption.

## 4. Tools

### Build

```
go build -o bin/asty ./asty/cmd
```

Or for the demo binaries:

```
make build-all
```

### Run-script

`deploy/dev/start.sh` is the operator. Subcommands used during runs:

- `start.sh N` — bring up N nodes from a clean slate.
- `start.sh add` — add one node to a running cluster.
- `start.sh degrade` — hard N→1 SIGKILL loop with reconvergence wait.
- `start.sh stop` — tear everything down (only after an error).

The script requires `sudo` for `/etc/hosts`, loopback aliases,
and root-owned PID cleanup. `/tmp/asty-dev.sudoers` permits NOPASSWD
for the script and a few related commands.

### Logs

Per-node logs at `/tmp/asty-dev-server-<i>.log` and
`/tmp/asty-dev-agent-<i>.log`. Degrade output mirrored to
`/tmp/asty-dev-degrade.log`.

### Observability during the run

- `curl -s http://127.0.0.1:7060/metrics | grep stabilized`
- `curl -s -H "Authorization: Bearer <A_TOKEN>" http://127.0.0.<i>:7060/dashboard/v1/`
- `kill -9` survivors' PIDs from `/tmp/asty-dev/pids-<i>` if they need
  manual intervention.

## 5. Reference

- `CLAUDE.md` — overall project guidance.
- `REPLICAS_KV.md` — N→1 collapse mechanism in detail.
- `NODE-DISCOVERY.md` — peer discovery flow (Russian).
- `memory/project_n1_degradation.md` — verified-live mechanics
  (last green baseline: 11→1 on 2026-06-04).
- `memory/feedback_unplug_test_default.md` — SIGKILL discipline.
- `memory/feedback_one_patch_per_iteration.md` — iteration rule.
- `memory/feedback_no_destructive_git_without_backup.md` —
  destructive-git protocol.

## 6. What this file is NOT

This is a runs protocol. It does not contain:

- Audit findings (those go in `.audit/{HH:MM_DD-MM-YY}.md`).
- Architectural decisions (those live in `CLAUDE.md`,
  `REPLICAS_KV.md`).
- Diagnostic chat (that's the conversation, not a checked-in file).

When in doubt: do less, ask less, run the protocol.
