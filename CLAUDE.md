# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Coding rules (read first)

Conventions for the asty package live in `.claude/coding-rules/`. Read the topic file that matches the work at hand before editing Go code under the asty package root:

- `.claude/coding-rules/README.md` — index of all rules.
- `.claude/coding-rules/code-idioms.md` — stdlib over handwritten, named constants, typed enums.
- `.claude/coding-rules/concurrency.md` — event-driven defaults, acceptable polling list.
- `.claude/coding-rules/testing.md` — race tests, `testutil/` fixtures.
- `.claude/coding-rules/clarity.md` — write so non-developers can follow.

Only the folder name `asty/` is stable (pattern: `**/asty/`); its parent path *may shift* between refactors — never hard-code it. Look for the directory containing `core/`, `infra/`, `domain/`, `ops/`, `api/`, `server/`, `agent/`.

## Project Overview

**Asty** is a microservices orchestrator with locality-aware autoscaling for NATS-based platforms. Single binary, two modes (server + agent), combining scheduling, autoscaling, and deployment.

The project consists of two main parts:
1. **Asty orchestrator** (`asty/`) — manages cluster state, schedules services, handles autoscaling. Provides HTTP JSON API. Web UI in `asty/web/` (React + Vite + shadcn/ui). **The orchestrator must remain agnostic of any specific managed service** — no `xauth`/`xhttp`/`xws` names, no demo-shaped paths or NATS subjects, no service-specific assumptions inside this package. Any leakage is a Critical defect.
2. **Demo services** (`demo/`) — microservices that Asty deploys (xauth, xhttp, xws). Use `nats.go/micro` directly, no platform SDK. Demo frontend in `demo/web/` (React + Vite). **These services, together with `deploy/{dev,prod}/`, `Makefile`'s `build-demo` target, and the coding-rule examples that reference them, are intentional customer-facing boilerplate.** The buyer of the platform removes them when shipping their own services. Mentions of demo names outside `asty/` are by design, not findings.

**Monitoring:** Asty's admin surface lives at `:7060` by default — dashboard REST + SSE at `/dashboard/v1/`, Prometheus exposition at `/metrics` (same listener), liveness at `/health`. Web UI (`asty/web/`) connects to it for cluster monitoring. Both port and prefix are configurable per surface (see Observability §).

**Web UI status:** feature-complete. The SPA in `asty/web/` covers every dashboard surface (cluster overview, nodes, services, allocations, logs, deploy, manual scale, drain/kill), wires every published SSE stream, and ships full EN/RU localisation with OS-detected default and persisted choice. Refactor history is in `.audit/13:51_24-05-26.md` (status: COMPLETE).
**Demo frontend:** `demo/web/` is a small React app that exercises the demo services (auth, CRUD, WebSocket) via the gateway.

## Toolchain & dependencies

**Always run on the latest stable releases** — Go, Go modules, and npm
packages alike. The pinned versions are not a stability floor; they're
the current latest at the time of the last bump. When a new release
lands upstream, update the pin and re-run the build/test suite.

**Go:** **1.26.3** at the time of writing. Pinned via `go.mod`. Go's
toolchain system auto-downloads the version named in `go.mod` if the
local install is older, so contributors don't need to upgrade
manually. Track releases at https://go.dev/dl/.

**Go modules:** `go get -u ./... && go mod tidy` walks every direct
and indirect dep to the latest in-range minor/patch. Major bumps
(`v2` → `v3` import paths) need manual handling.

**npm packages:** each web project (`asty/web/`, `demo/web/`,
`docs/`) maintains its own `package.json`. `npm update` inside each
honours the `^`/`~` ranges; a major bump beyond the range is flagged
by `npm outdated` and must be done deliberately (e.g. Tailwind 3 → 4
needs a config-and-class migration, not just a version line). Surface
those to the user before applying.

**No vendoring.** `vendor/` stays gitignored. We rely on
`proxy.golang.org` — Google's promise is that any module version
fetched through the proxy is cached permanently, so even abandoned
indirect deps (e.g. `github.com/munnerz/goautoneg`, pulled in via
`prometheus/common/expfmt`) won't break a future build. If supply-
chain isolation ever becomes critical, the surgical move is a per-
module `replace` directive in `go.mod` into `third_party/<name>/`,
not a 190 MB blanket vendor.

**Daily-update protocol:** at the first prompt of each new day, check
all three surfaces and offer the user a one-shot bump:

  1. Compare today's date against the most recent check (memory entry
     `project_deps_last_check`). If same day, skip; otherwise proceed.
  2. Latest Go stable (`https://go.dev/dl/`) vs `go.mod`'s `go` line.
  3. `go list -m -u all` for module updates.
  4. `(cd asty/web && npm outdated) ; (cd demo/web && npm outdated) ;
     (cd docs && npm outdated)`.
  5. Summarise: who's behind and by how much. Flag majors separately
     from in-range bumps. If everything is current, just say so.
  6. Offer to install. Only on the user's go-ahead, apply the bumps
     and rebuild every affected project (`go build ./... && go test
     ./...` for Go; `npm run build` in each web project that had a
     change).
  7. Update the `project_deps_last_check` memory with today's date so
     the next session in the same day doesn't repeat the check.

**MCP servers:** `.mcp.json` at the project root holds project-shared
MCP configs. Claude Code reads it on startup; `/mcp` lists connected
servers. Today it carries the `shadcn` MCP from
https://ui.shadcn.com/docs/registry/mcp, pointed at `asty/web/` via
`cwd` because that's where `components.json` lives. To add a private
shadcn registry: drop a `registries: { "@name": "url" }` block into
`asty/web/components.json` — the MCP server picks it up next restart.

## Build Commands

```bash
# Build orchestrator only
make build        # → bin/asty

# Build demo services
make build-demo   # → bin/xauth, bin/xhttp, bin/xws

# Build everything
make build-all

# Run tests
make test

# Run modes
make run-agent    # Run in agent mode
make run-server   # Run in server mode
```

## Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./asty/internal/api/gateway -v
go test ./asty/internal/ops/scheduler -v

# Run single test
go test ./asty/internal/ops/scheduler -v -run TestScheduler
go test ./asty/internal/domain/proximity -v -run TestProximity

# With race detector
go test -race ./...
```

## Architecture

### Architecture

**Each node runs two Asty processes:**
- `asty -mode server` — participates in leader election, active leader handles scheduling and autoscaling
- `asty -mode agent` — supervises a local `nats-server` child process (see "NATS supervision" below), manages user-service processes, executes commands from server. The HTTP gateway runs inside this process (`api/gateway/`), reusing the agent's NATS connection; `gateway.enabled: false` in config.asty disables it on a given node.

**Leader election:** All servers participate in election via NATS KV. Only one server is active (leader) at any time. When leader fails, remaining servers automatically elect a new leader. This provides high availability with no single point of failure.

Communication between agents and server happens over NATS subjects (`asty.v1.*`). State is stored in NATS JetStream KV.

### NATS supervision

NATS is part of Asty's runtime, not a separately-managed dependency.
Each `asty -mode agent` at startup:

1. Reads the `nats:` section of `.asty` (which has BOTH client-side
   credentials and a `server:` subsection — the full nats-server
   config in YAML form).
2. Renders the on-disk `nats.conf` via `core/natsconf.Render` from
   that subsection + the node's identity (`A_NODE_ID`, `A_NODE_IP`)
   + the resolved peer list (see "Peer discovery" below).
3. exec's the `nats-server` binary (found next to the asty binary or
   on `$PATH` — install via `make nats-server`).
4. Probes TCP readiness, then continues with the rest of bootstrap.
5. `agent/natswatch.go` keeps two goroutines running for the rest of
   the process lifetime:
   - `superviseNATS` owns the child: graceful stop on ctx-cancel,
     restart on peer-list change (see below), Fatal on unexpected exit.
   - `watchNATSPeers` re-resolves the peer list every 5 s and signals
     the supervisor when the sorted set changes.

   On a peer change the supervisor first tries `tryHotReloadNATS`:
   write the new `nats.conf`, then `kill -HUP` the child. NATS applies
   the routes delta live, no client reconnect, no JS metadata election.

   The hot-reload renderer pins `cluster{}` on (`KeepClusterBlock=true`
   in `natsconf.Render`), even when the peer list became empty. NATS
   rejects an empty-routes `cluster{}` on cold start but accepts it on
   SIGHUP, so a shrinking cluster stays clustered all the way down to
   one node. The flip to standalone is what would otherwise refuse to
   load previously-replicated streams ("replicas > 1 not supported in
   non-clustered mode", err_code=10074).

   A cold restart (SIGTERM → re-`bootstrapNATS`) is only triggered when
   the existing `nats.conf` has no `cluster{}` block and a peer just
   joined — the standalone→clustered direction structurally requires
   a fresh JS init. `nats.go`-based clients reconnect automatically;
   JS data survives because the store directory does.

The agent opens **two** NATS client connections and ships a third set of
credentials to spawned services:

- `user`/`password` (ASTY account) — main connection (`agent.nc`): cluster
  KV, `asty.v1.*`, gateway (the embedded gateway reuses this connection),
  ping, log streams.
- `observer_user`/`observer_password` (SYS account) — optional connection
  (`agent.ncSys`): only STATSZ/JSZ request-reply, feeds `asty_node_nats_*`.
  If unset, the agent still comes up and those metrics stay at zero.
- `app_user`/`app_password` (ASTY account) — **not a connection**. Just
  credentials the agent exports to spawned services via
  `A_NATS_USER`/`A_NATS_PASSWORD`. MUST differ from the agent's own
  `user`; otherwise apps inherit JS KV access to `asty-cluster`. If unset,
  the agent does not export those env vars (apps fail loudly at startup).

#### Peer discovery

`resolveNATSPeers` has one source: DNS `LookupIP(cfg.Domain)`. Self-IP
is filtered out so a node never routes to itself. Agents re-resolve on
every watcher tick, so operators grow/shrink the cluster by editing the
domain's A-records.

- **Prod**: `cfg.Domain` is the cluster domain; its A-records are the
  nodes.
- **Dev**: `cfg.Domain` is `asty.test`, which `start.sh`'s `sync_hosts`
  points at every node's loopback alias in `/etc/hosts` (rebuilt from
  the peer-IP list on start / `add` / `remove`). Same `LookupIP` code
  path as prod — no file/env stand-in.

Render-time asymmetry: cold bootstrap with an empty peer list (i.e.
single-node startup) omits the `cluster{}` block and NATS runs in
standalone JetStream mode. Hot reload of an already-clustered process
keeps the block on (see `KeepClusterBlock` above), so a shrink to 1
node does NOT flip back to standalone. KV buckets land with
`replicas=1` initially; when peers appear and the broker restarts in
clustered mode, `server/streamreplicas.go` (leader-only) upgrades
existing streams via `UpdateStream`. The pair forms a symmetric loop:
`server/kv.go:ensureKVBucket` degrades on create when the cluster
can't place the requested replicas (10005 or 10074), and
`watchStreamReplicas` raises them back up later.

#### Graceful node decommission

When the agent receives SIGTERM and its departure leaves the cluster
non-empty, `agent/natsleave.go` runs before `stopNATSSupervisor`:

- **`decommissionSelf`** publishes on `$JS.API.SERVER.REMOVE`
  (SYS account) and awaits the `$JS.EVENT.ADVISORY.SERVER.REMOVED`
  advisory. One meta-RAFT `EntryRemovePeer` proposal both shrinks the
  meta-cluster config AND remaps every stream we belong to. Skipping
  it would leave dead peers in the meta config, the quorum target
  unreachable for any later proposal, and the next dying node's
  shutdown work hung.
- When `surviving==1` (the 2→1 step), `shrinkStreamsToSingle` runs
  FIRST: for every stream with `Replicas>1` it transfers leadership
  away from this node (`STREAM.LEADER.STEPDOWN` + the
  `LEADER_ELECTED` advisory) and then `UpdateStream(Replicas=1)`.
  The order matters — `SERVER.REMOVE` disables our JS, so any stream
  update has to land before it.
- The `surviving` count comes from `survivingClusterPeers()`
  (`agent/natsleave.go`), which reads live `node.<id>` entries from
  cluster KV minus self — not from DNS. The DNS A-records update only
  on a `start.sh remove` or an operator editing them; a dashboard kill
  touches neither, so trusting them would miscount the 2→1 step and
  skip the shrink, pinning streams at R=3 with one alive peer (no
  quorum → `nats: no response from stream` on every write). KV is the
  orchestrator's own membership view, kept current by every agent's
  `RemoveNode` on graceful exit. Falls back to the DNS count only when
  KV is unreachable.

Permissions: the `asty-observer` SYS user is granted publish on
`$JS.API.SERVER.REMOVE` and subscribe on
`$JS.EVENT.ADVISORY.SERVER.REMOVED` in both `deploy/dev/config.asty`
and `deploy/prod/config.asty`.

For offline inspection of what would be written:
`asty -mode nats-conf -config <path> -peers <ip1,ip2,...>` prints the
rendered nats.conf to stdout without launching anything.

### Service Deployment Model

Asty deploys services as **raw binaries** (not containers):
- Services defined in `.asty` files (YAML declarative format, see `services/*.asty`)
- Agent downloads binaries from URLs; sha256 checksum is verified when
  the `.asty` supplies a real value, skipped when the field is empty or
  contains an unresolved `${...}` template placeholder
- Process lifecycle: start → health check → run → graceful shutdown (SIGTERM → SIGKILL)
- Two service types:
  - `type: system` — one copy per node (currently only platform demos; the HTTP gateway is no longer a `.asty` service — it lives inside the agent binary)
  - `type: service` — autoscaled based on load (e.g., xauth, xhttp)

**Version pin.** Per-service desired version is stored in KV at
`service.<name>.version` as `{current, previous}` and is the sole input
the scheduler reads when creating a fresh allocation (`createAllocation`
→ `versionFor` → fallback `"latest"` only when the pin is absent). The
deployer is the only writer: sets `{current: target, previous: old}`
on Deploy.Begin, clears `previous` on success, restores `current =
previous` on revert. Result: autoscaler-spawned copies created during
or after a deploy run the deployed version, never the stale "latest".

### Locality-Aware Autoscaling

Traffic is routed by geographic load balancer to the nearest node. The gateway on that node validates requests and serves them **locally** (same-node NATS at `127.0.0.1:4222`). This is Asty's main differentiator from generic orchestrators.

Autoscaler monitors (`ops/autoscaler/scale_up.go`):
1. Gateway valid-RPS per node — sustained average ≥ `TrafficRPSThreshold`
   (default 5) over a 60s window (`trafficWindow`) → scale up onto the
   ready node that has traffic but no local copy.
2. Resource pressure on an existing copy: `CPUUsage` (%) > `TargetCPU`
   (default 75) **OR** the allocation's `MemoryUsage` (MB) translated
   to a percentage of its service's declared `Resources.Memory` exceeds
   `TargetMemory` (default 75). Both checks read as "over 75 % of
   budget" — `TargetMemory` is a **percent of the per-copy memory
   limit**, not a literal MB floor. The actual comparison
   (`scale_up.go:115`) is
   `(MemoryUsage * 100 / svc.Resources.Memory) > TargetMemory`;
   services that didn't declare `Resources.Memory` skip the memory
   check entirely.
3. Scheduler's `PickCandidates` (incl. DC proximity matrix) chooses the
   target node for the new copy.

Scale down (`scale_down.go`) honours the per-service **floor** (override
from `kv.SetServiceScale` when set, otherwise `cfg.Autoscale.MinCopies`,
default 3) and adds hysteresis: average usage across running copies
must drop below `TargetCPU/2` AND `TargetMemory/2` (`idleFloorDivisor=2`).
Victim selection is shared with the dashboard's manual-scale handler
via `scheduler.PickRemovalVictims` — prefers the most-crowded DC, ties
break by NodeID ascending. There is no clamp to floor>=1: an override
of 0 truly empties the service (re-creation is then driven by
locality-based scale-up if any traffic shows).

**Autoscaler gates.** `EvaluateService` short-circuits to noop when
either of two service-level flags is set in
`ServiceCooldown` (`infra/kv/cooldowns.go`):

- `RollbackFailed` — last deploy's auto_revert failed; the cluster is
  in mixed-version limbo. Cleared by the operator via the API.
- `DeployInProgress` — a rollout is currently running. Set by the
  deployer on Deploy.Begin, cleared on any terminal state. Eliminates
  the race where the autoscaler places a stale-version copy on a node
  whose existing allocation is briefly Restarting.

### Manual scale (per-service floor)

The dashboard's "Min copies" control writes a per-service override to
`service.<name>.scale` in KV. Contract:

- The override **replaces** `cfg.Autoscale.MinCopies` as the scheduler's
  placement target (`scheduler.TargetCopies`) AND as the autoscaler's
  scale-down floor (`evaluateScaleDown`).
- The override is **not a ceiling**. The autoscaler can still grow
  copies above it in response to traffic or pressure, capped by
  `cfg.Autoscale.MaxCopies`.
- Setting count below the current live copy count stops the excess
  copies immediately via the same DC-aware picker the autoscaler uses
  (`scheduler.PickRemovalVictims`).
- `POST /services/{name}/scale` rejects:
  - `count > MaxCopies` (when MaxCopies > 0) → 400, because the
    autoscaler would refuse to honour it anyway.
  - `type: system` services → 409, because the scheduler immediately
    re-creates whatever the handler killed (one-per-node invariant).

The effective floor is surfaced in two places: snapshot's `min_copies`
(used by the global services SSE) and `/services/{name}/autoscaler`
which also reports `min_copies_default` and `min_copies_override` so
the UI can flag "overridden" without a second round-trip.

### Reconciliation controller

The `ops/reconciler/` package owns the active control
loop and **runs only on the elected leader** (`server/leadership.go`
starts it via `startLeaderWork`, `watchLeadership` flips it on/off when
leadership changes). Followers run no reconciler; their server process
is otherwise identical (still serves the API, still subscribes to log
buffers) but does not place or dispatch allocations.

Inside the controller (`controller.go`):
- **Startup warmup** (`warmup.go`). The very first `enqueueAllServices`
  is gated by an event-driven debouncer on `WatchNodes`: every new
  `NodeReady` transition resets a `warmupQuietWindow` (3 s) timer; the
  first reconcile fires only after the cluster goes that long without
  a new ready node, or after `warmupMaxWait` (30 s). Without this the
  leader's first pass would run while only a subset of expected nodes
  has heartbeat'd, placing services on those few and leaving the
  late-joining nodes empty (the scheduler only *adds* — once `target`
  is met it never moves an existing copy). Subsequent reconciles fire
  on watcher events and the safety-net resync as usual; warmup runs
  once per leader incarnation.
- A k8s-style **`Workqueue`** (`workqueue.go`) — deduplicated FIFO with
  per-key rate-limiting. Failed reconciles are re-enqueued via
  `AddRateLimited`; delay grows `BaseDelay * 2^failures`, capped at
  `MaxDelay`. Defaults are `500ms → 60s`. `failureLimitDefault = 8` in
  `controller.go:26` is informational only — the queue itself doesn't
  consult it.
- **Producers**: `enqueueAllServices` on startup, `watchAllocsToQueue`
  on any non-trivial alloc status change (and explicitly *not* on the
  controller-owned `Starting → Pending` rollback), `watchNodesToQueue`
  on any node status change (re-enqueues every service), and
  `periodicResync` every `resyncEvery` (default `60s`) as a safety
  net. API handlers `scale`, `stop`, and `deploy` call `Enqueue(name)`
  directly so user actions don't wait on the tick. The per-alloc
  `restart` handler does NOT enqueue — the agent owns the FSM
  in-place (see "Restart / Stop allocation" below), so the controller
  has nothing to reconcile.
- Per-key `reconcile` (`reconcile.go:22`) is a strict pipeline:
  `scheduler.ReconcileService` → `dispatchPending` → `pruneFailed` →
  `autoscaleOnce` (skipped for `ServiceTypeSystem`, since system
  services run one-per-node and have no autoscale dimension).
- `dispatchPending` first runs `unstickStarting`: any allocation that
  has been in `AllocStarting` longer than `startingStuckAfter = 90s`
  (`reconcile.go:17`) is CAS'd back to `AllocPending` so the loop can
  retry. Then for each `Pending`: CAS to `Starting`, send the start
  RPC, roll back to `Pending + AddRateLimited` on dispatch failure.
- `pruneFailed` deletes allocations with `ConsecutiveFailures ≥
  svc.Restart.Attempts`; the scheduler picks a fresh node next pass.

### Drain

`ops/drainer/DrainManager` (always allocated in
`server/boot.go`, effective only when invoked from leader-side API
routes) coordinates a node drain via the small `DrainDeps` interface
(no `server → drain → server` import cycle).

- `Start(nodeID)`: collect live allocations on the node, CAS node to
  `NodeDraining`, then either fast-path to `NodeDrained` (zero
  allocations) or spawn `runDrain`.
- **System allocations** (`type: system`) are dismantled in **parallel
  goroutines** — there's no place to migrate them to (one copy per
  node), so the agent gets a stop, the manager waits for the
  allocation to hit `AllocStopped`/`AllocFailed` (budget
  `kill_timeout + 10s`), then deletes the KV record.
- **Regular allocations** are processed in **bounded-parallel
  goroutines** (`run.go:66-89`): one goroutine per alloc, gated by a
  `chan struct{}` semaphore sized `maxConcurrentMigrations` so the
  scheduler and NATS aren't drowned by a large drain. Per alloc:
  `placeReplacement` picks a target (or falls back to letting the
  controller place it), `waitForHealthyOnNode` /
  `waitForHealthyReplacement` blocks up to `drainHealthDeadline = 2m`,
  then `finalizeMigration` (Stop + wait + delete on the source). When
  no peer is ready at all (single-node teardown), regulars are
  promoted into the system-alloc path (`run.go:44-47`) and dismantled
  without waiting for replacements.
- After all goroutines join, `completeNodeDrain` CAS's the node to
  `NodeDrained` and publishes a final status event on
  `asty.v1.drain.progress` (NATS subject, JSON), which `streamHub`
  fans out to SSE subscribers.
- `Resume(nodeID)` cancels the drain context and CAS's the node back
  to `NodeReady`.

### Kill (abrupt decommission)

The dashboard equivalent of `deploy/dev/start.sh remove` — for nodes
that are unresponsive, or when the operator deliberately wants to skip
graceful migration. Two-step UI confirmation (warning + typed-name
input), then `POST /dashboard/v1/nodes/{id}/kill` with body
`{"confirm_name": "<node-id>"}`.

Server-side flow (`api/dashboard/nodes.go:handleNodeKill`):

1. **CmdShutdown over NATS** (`asty.v1.agent.<nodeID>.cmd.shutdown`).
   The agent acks immediately, then cancels its own derived context;
   the existing SIGTERM-graceful path runs (decommission NATS, shrink
   streams if shrinking to one survivor, deregister node from KV, stop
   all spawned processes). If the agent is unreachable the request
   times out — the next step still runs.
2. **Force-purge from server side**: every live allocation on the node
   is `DeleteAllocation`'d, then `RemoveNode` clears the node KV
   record. Both operations are idempotent — no-ops when the agent's
   graceful path already handled them.
3. **Reconciler** picks up the deleted allocations on its next pass
   (node-watch fires `enqueueAllServices`) and reschedules them onto
   surviving nodes.

The agent's `CmdShutdown` handler (`agent/commands.go`) is intentionally
async: ack first, cancel after, so the response wins the race against
NATS going down inside the graceful path. There is no `Resume` for
kill — it's terminal.

The local **server** process is a separate process and is NOT signalled
by CmdShutdown. It exits via `server/selfremoval.go:watchSelfRemoval`,
which watches `node.<id>` in cluster KV and cancels `Start`'s ctx when
its own entry is deleted. The deletion arrives from the agent's
graceful path (RemoveNode runs before nats-server is stopped). Without
this, the server would survive a dashboard kill and keep refreshing
the leader lease over auto-discovered NATS peers, blocking any other
node from claiming leadership.

### Restart / Stop allocation

Per-allocation operator actions exposed at
`POST /dashboard/v1/nodes/{id}/allocations/{allocId}/{restart,stop}`.
The finest-grained tool the dashboard offers: act on one specific
copy on one specific node, distinct from scale (per-service),
drain/kill (per-node), and deploy (per-service version roll).

**Restart** — synchronous in-place restart that preserves allocation
identity. The handler (`api/dashboard/allocations.go:handleAllocationRestart`)
calls `ServerContext.RestartServiceOnNode` which resolves the current
`ServiceDefinition` via `serviceLoader.GetService` and dispatches
`CmdRestart` (server `sendRestartCommand`) with timeout sized to
`svc.GetKillTimeout() + agentStartCommandTimeout`. The agent's
`RestartService` (`agent/services.go`):

  1. CAS the allocation to `AllocRestarting` and `Restarts++` in one
     mutation. `ConsecutiveFailures` is left alone (this is operator-
     initiated, not a crash, so it should not feed `pruneFailed`'s
     budget); a successful start below resets it to 0.
  2. `stopProcess` (shared with `StopService`) — kill the local
     process and unregister health/metrics, WITHOUT touching KV.
  3. `StartService` — fresh artifact resolve, spawn, mark `Running`
     with new PID. On failure: roll the alloc back to `Pending` and
     return the error; the controller's rate-limited dispatch will
     retry via `CmdStart` (with backoff).

KV transitions are `Running → Restarting → Running` (or `→ Pending`
on failure). The slot is held throughout — `AllocRestarting` is in
both `IsLive` and `Occupies`, so the scheduler does not place a
competing copy elsewhere, and the node stays the same. No KV
mutation from the dashboard side and no `ReconcileService` call —
the agent owns the FSM end-to-end.

The same `RestartService` path is used by the deployer's rolling
update (`ops/deployer/sendUpdateCommand` → `CmdRestart`), so deploy
restarts also increment `Restarts` and preserve identity.

**Stop** — synchronous removal: `CmdStop` (agent fast-acks, runs
`StopService` in a goroutine, KV transitions `Stopping → Stopped`),
then the dashboard waits for `Stopped`/`Failed` via
`WatchAllocation` (`api/dashboard/allocations.go:waitForAllocationStopped`)
with budget `svc.GetKillTimeout() + 10s` (fallback `60s` for orphans
with no resolvable service def), then `DeleteAllocation`, then
`ReconcileService`. Mirrors the drain pattern (`ops/drainer/wait.go`)
so the two paths don't drift; the helper `stopAndDeleteAllocation`
is shared with the scale-down victims loop in
`api/dashboard/services.go`.

Rejected with **409** for `type: system` services — the scheduler
would immediately re-create the slot on the same node (one-per-node
invariant), making Stop a no-op. To remove a system-service copy
from a node, drain or pause the node instead. Restart on system
allocations is still allowed (the only way to bounce a system copy
without touching its neighbours).

The post-Stop UI behaviour is to navigate to `/nodes` after the
toast — the reconciler may have backfilled the copy on a different
node (regular service) or the same node (system service, theoretical
since we reject; left for symmetry), and the operator needs the
cluster-wide view to find it. The current allocation page would 404.

Failure-recovery loop interaction: the agent's own
`attemptRestart` (`agent/restart.go`) also writes
`AllocRestarting` when a process crashes. The two paths share the
state but operate on disjoint local-process state. The post-delay
`Restarting → Pending` transition in `attemptRestart` is CAS-guarded
on `Status == AllocRestarting`, so a concurrent operator restart
that flips the alloc back to `Running` does not get downgraded.

### Deploy (per-service rolling update)

`POST /dashboard/v1/services/{name}/deploy` with body `{"version":"..."}`
triggers a rolling update. Three observable properties: **async over
HTTP**, **autoscaler-paused**, **version-pinned at the service level**.

Flow (`server/deployment.go:DeployService`):

1. Pre-checks: reject empty version (400); reject if a deploy is
   already running for this service (409, `deployer.ErrDeployInFlight`).
2. Load the freshly-parsed `.asty` so per-deploy edits to env, resources,
   health, etc. are honoured.
3. Read the version pin (`kv.GetServiceVersion`) — its `Current` becomes
   `plan.CurrentVersion` (the rollback target). Absent pin means first
   deploy, CurrentVersion is left empty so revert refuses to fire.
4. List allocations, filter to `IsLive`, sort by NodeID ascending.
   Sorted+filtered set means canary always picks the same copy on
   retry, and Failed/Stopped allocs don't get spurious restarts.
5. **Bootstrap** (no live allocs): just write the pin and call
   `ReconcileService(name)`. Scheduler creates the first copies at the
   pinned version on its next pass. Returns Completed immediately.
6. **Normal path**: build plan, launch `deployer.Deploy(s.lifeCtx, plan)`
   in a goroutine, return 202 with the initial running status.

Deployer (`ops/deployer/deployer.go:Deploy`):

1. `claim(serviceName)` — fails with `ErrDeployInFlight` on a second
   concurrent call.
2. `resetTouched`, `beginRecord` (in-memory history + KV `service.<name>.deployment` + SSE `asty.v1.deploy.progress.<name>`).
3. `pinVersion({current: target, previous: plan.CurrentVersion})`.
4. `setDeployGate(true)` — flips `ServiceCooldown.DeployInProgress`
   so the autoscaler stops touching this service.
5. Canary phase (if `update.canary > 0`): markPending → CmdRestart →
   waitForBatchHealth, with `CanaryRetries+1` attempts.
6. Rolling phase: batches of `max_parallel`, sequentially.
7. On success: `pinVersion({current: target})` (Previous cleared),
   `updateLastRecord(Completed)`, gate cleared via `defer`.
8. On failure with `auto_revert: true`: `revertDeployment` re-dispatches
   `touched` allocs at `plan.CurrentVersion`, waits for health. On
   success: `pinVersion({current: plan.CurrentVersion})` (rollback
   pinned). On failure: `markRollbackFailed` sets
   `ServiceCooldown.RollbackFailed = true`; operator must clear via API.

SSE: `GET /dashboard/v1/services/{name}/deploy` with
`Accept: text/event-stream` streams `progress` events carrying the
full DeploymentRecord JSON for the requested service (filtered on the
server side from the `asty.v1.deploy.progress.>` wildcard subject).
The JSON flavour of the same path returns history (in-memory ring,
capped at 100).

Concurrency guards live in `ops/deployer/lifecycle.go` —
`claim`/`release`/`IsInFlight` + `pinVersion`/`setDeployGate` — kept
separate from `deployer.go` to stay under the file-size cap.

### Platform Services Architecture

```
HTTP Client → Gateway (:80) → NATS (127.0.0.1:4222) → [xhttp | xauth | xws]
                                                          ↓
                                                    PostgreSQL (xhttp only)
                                                    NATS KV (xhttp cache, xauth tokens)
```

- **Gateway** (`asty/internal/api/gateway/`) — sole HTTP entry point, embedded in the asty agent process; proxies HTTP → NATS Request-Reply, upgrades WebSocket connections
- **xauth** (`demo/internal/xauth/`) — JWT authentication (HMAC-SHA256), refresh token revocation in NATS KV. Uses `nats.go/micro` directly.
- **xhttp** (`demo/internal/xhttp/`) — demo CRUD with PostgreSQL + NATS KV cache. Uses `nats.go/micro` directly.
- **xws** (`demo/internal/xws/`) — WebSocket session manager. Uses raw `nats.go` (pub/sub, not request-reply).

All inter-service communication is NATS Pub/Sub. No service-to-service HTTP calls.

## Observability

**Mirror rule — UI and Prometheus stay in lockstep.** Every metric the
web UI displays must also be exposed on `/metrics` (and vice versa,
when that direction makes sense). The same number is meaningful to a
human glancing at the dashboard and to an alerting system parsing the
scrape output — divergence between the two surfaces is a bug, not a
stylistic choice.

**Logs are not on this surface.** The mirror rule is about *metrics*
— numeric time series. Application logs (zerolog JSON) flow through
their own pair of consumers: the dashboard's live SSE tail and
in-memory history, plus an external log shipper (Vector, Datadog,
Loki, …) configured per deployment. Do not add log-event counters or
log lines to `/metrics` — it is the wrong format for log data, and
the external shipper is the source of truth for log retention and
content-based alerting.

### HTTP surfaces

Per node, three HTTP surfaces — each with its own port and prefix,
all configurable via env. Defaults align so a typical deployment
only needs one firewall rule for the dashboard/scrape pair:

| Surface | Default listen | Default prefix | Env knobs |
|---|---|---|---|
| Dashboard (admin REST + SSE) | `127.0.0.1:7060` | `/dashboard/v1` | `A_DASHBOARD_{HOST,PORT,PREFIX}` |
| Prometheus exposition | `127.0.0.1:7060` (shared) | `/metrics` (exact match) | `A_PROMETHEUS_{HOST,PORT,PREFIX}` |
| Gateway (user traffic) | `0.0.0.0:80` | `/api/v1` | `A_GATEWAY_{HOST,PORT,PREFIX}` |

When `A_DASHBOARD_PORT` and `A_PROMETHEUS_PORT` are equal (the
default) the same `http.Server` mounts both routes. When they differ,
`server.runStandalonePrometheus` spawns a second listener.

**The gateway is intentionally observability-silent.** It does not
expose its own `/metrics` listener — Asty must not serve metrics
about user-deployed services. The only data the gateway publishes
back is the `GatewayMetricsReport` over NATS
(`asty.v1.metrics.gateway.<nodeID>`, `codec.Wire`), which feeds the
autoscaler's locality-aware scale-up trigger via
`server.subscribeGatewayMetrics`.

**NATS exposes no HTTP listener.** The agent supervises a local
`nats-server` per node (see "NATS supervision" below) and pulls
server stats over the existing NATS connection via
`$SYS.REQ.SERVER.<id>.STATSZ` + `$SYS.REQ.SERVER.<id>.JSZ`, then
surfaces them as `asty_node_nats_*` / `asty_cluster_nats_*` on the
Prometheus exposition. No `http_port` directive is rendered into
`nats.conf`.

The dashboard surface is the only HTTP entry for cluster data. Web
UI subscribes to its SSE flavour, CLI tooling fetches JSON over the
same routes. URLs match the navigation hierarchy 1:1:

- `/dashboard/v1/` — cluster snapshot (`GET /` under the prefix).
- `/dashboard/v1/nodes`, `/dashboard/v1/nodes/{id}`,
  `/dashboard/v1/nodes/{id}/allocations/{allocId}/logs`.
- `/dashboard/v1/nodes/{id}/drain` (POST start/cancel, GET status),
  `/dashboard/v1/nodes/{id}/pause` (POST),
  `/dashboard/v1/nodes/{id}/kill` (POST — abrupt decommission, see "Kill" §).
- `/dashboard/v1/services`, `/dashboard/v1/services/{name}/allocations`,
  `/dashboard/v1/services/{name}/autoscaler`,
  `/dashboard/v1/services/{name}/deploy`.

`/health` stays at the root for kube-probes. The dashboard prefix
comes from `cfg.Dashboard.Prefix`; the SPA reads the matching
`API_PREFIX` from `asty/web/src/lib/routes.ts` — change both in
lockstep when re-configuring. Every SPA URL — backend endpoint or
react-router target — flows through that file's `routes` /
`apiPaths` exports.

The dashboard listener (and therefore `/metrics` when shared) is
served by **every** server in the cluster, not just the leader.
`POST` handlers are gated by the `leaderOnly` middleware that
returns 307 to the leader's address. Followers serve GETs and SSE
streams unchanged. The `asty_leader` metric is built from the
cluster snapshot's `Cluster.Leader`, so every server's exposition
reports the same row with the same `node_id` label.

Write endpoints under `/dashboard/v1/*` carry three middleware
layers in order:

  `tokenAuth → leaderOnly → auditLog → handler`

  - `tokenAuth` constant-time-compares the request token (Authorization:
    Bearer / X-Asty-Token) against `cfg.Token`.
  - `leaderOnly` server-side reverse-proxies follower writes to the
    leader's dashboard listener (`httputil.NewSingleHostReverseProxy`)
    and streams the response back. The follower adds `X-Asty-Leader`
    so clients can observe which node actually served the call. No
    307-redirect — browsers/proxy libraries handle cross-origin POST
    redirects poorly (preflight, body loss); server-side forwarding
    keeps the round-trip single-hop from the client's view.
  - `auditLog` publishes a `types.AuditEvent` to
    `asty.v1.audit.<resource>.<action>` on NATS (CBOR via `codec.Wire`),
    capturing status, target, actor IP, and X-Request-Id.

### Metric naming convention

All orchestrator-emitted gauges/counters carry a domain prefix so
extension stays orderly:

| Prefix | Scope | Labels | Examples |
|---|---|---|---|
| `asty_cluster_*` | cluster-wide aggregates | none | `nodes_total`, `nodes_healthy`, `services_loaded`, `cpu_total_mhz`, `cpu_available_mhz`, `cpu_used_mhz`, `memory_total_mb`, `memory_available_mb`, `memory_used_mb`, `disk_total_mb`, `disk_available_mb`, `disk_used_mb`, `disks_ssd`, `disks_hdd`, `disks_unknown`, `swap_total_mb`, `swap_available_mb`, `swap_used_mb`, `rps`, `health_percent` |
| `asty_node_*` | per-node | `node_id`, `datacenter` (+ `status` on `_status`, + `disk_type` on `_disk_type`) | `cpu_total_mhz`, `cpu_available_mhz`, `memory_total_mb`, `memory_available_mb`, `disk_total_mb`, `disk_available_mb`, `disk_type`, `swap_total_mb`, `swap_available_mb`, `allocations_running`, `allocations_planned`, `status`, `self_cpu_percent`, `self_memory_mb`, `self_disk_mb` |
| `asty_service_*` | per-service | `service` | `copies_current`, `min_copies`, `cpu_avg_percent`, `memory_avg_mb`, `cooldown_up_active`, `cooldown_down_active` |
| `asty_alloc_*` | per-allocation | `service`, `node_id`, `alloc_id` (+ `state` on `_health`, `status` on `_status`) | `cpu_percent`, `memory_mb`, `disk_mb`, `rps`, `restarts_total`, `uptime_seconds`, `health`, `status` |
| `asty_deploy_*` | per-deployment | `service` (+ `state` on `_state`) | `state`, `progress_percent` |
| `asty_leader` | leader-election state | `node_id` | Always 1 with the leader's `node_id` label; emitted on every server's `/metrics` (built from the snapshot's `Cluster.Leader`), not leader-only. |
| `asty_node_nats_*` | pulled from local NATS via `$SYS.REQ.SERVER.<id>.STATSZ` + `JSZ` | `node_id`, `datacenter` | `cpu_percent`, `memory_mb`, `connections`, `subscriptions`, `slow_consumers`, `in_msgs_total` (counter), `out_msgs_total` (counter), `jetstream_messages`, `jetstream_bytes`, `disk_mb` (binary baseline + JS bytes) |
| `asty_cluster_nats_*` | per-cluster NATS aggregates | none | `connections`, `jetstream_messages`, `jetstream_bytes` |

### Adding a new metric

When the UI gains a metric (new tile, new chart, new column), add the
matching Prometheus instrument in the same change. Pick a prefix from
the table above. Use `prometheus.NewGaugeFunc` with a closure that
reads from `api.ctx` / `streamHub.Snapshot()` so the value stays
consistent with what the UI sees. Counters need a real `Inc()` call
site, not a periodic snapshot.

## Configuration

### Orchestrator (Asty)
Asty (server and agent) reads its configuration from a YAML file via the `-config` flag:

```
asty -mode server -config /etc/asty/config.asty
asty -mode agent  -config /etc/asty/config.asty
```

Without `-config`, the default `./config.asty` is consulted and a missing file is tolerated (env-only deployment). Sections mirror the runtime layout (`nats:`, `autoscale:`, `resources:`, `dashboard:`, `prometheus:`, `agent:`, `gateway:`, `artifact:`). Sample: `deploy/dev/config.asty`.

**Env-var overrides** apply on top of YAML defaults. Two paths exist:
fields routed through `Config` via `core/config/env.go:applyEnvOverrides`,
and fields read directly via `os.Getenv` at the point of use. Both are
listed below; together they are everything `A_*` Asty actually consumes.

Routed through `Config` (`core/config/env.go`):

- `A_DOMAIN`, `A_TOKEN` — required outside `dev_mode`.
- `A_DATACENTER`, `A_NODE_ID`, `A_NODE_IP`, `A_LOG_LEVEL`.
- `A_DEV_MODE` (bool) — opt out of `Validate`; also flips `codec.Wire`
  and `codec.State` to JSON for human-readable NATS payloads.
- `A_MOCK_NODES` (int) — seed N fake `NodeReady` entries into KV for
  scheduling experiments without real agents (server-only).
- `A_NATS_USER`, `A_NATS_PASSWORD` — ASTY-account main connection used
  by server and agent.
- `A_NATS_OBSERVER_USER`, `A_NATS_OBSERVER_PASSWORD` — SYS-account
  read-only connection the agent uses for `$SYS.REQ.SERVER.*.STATSZ`/`JSZ`.
- `A_NATS_APP_USER`, `A_NATS_APP_PASSWORD` — ASTY-account credentials
  handed to spawned services. MUST differ from `A_NATS_USER`.
- `A_NATS_*_PASSWORD` env vars are also substituted into
  `nats.accounts.*.users[].password` in `config.asty` at load time via
  `${VAR}` expansion (bare `$NAME` is left alone so NATS subjects like
  `$SYS.REQ.*` survive).
- Autoscaler: `A_MIN_COPIES`, `A_MAX_COPIES`, `A_TARGET_CPU`,
  `A_TARGET_MEMORY`, `A_TRAFFIC_RPS_THRESHOLD`, `A_TRAFFIC_WINDOW`,
  `A_IDLE_HOLD`, `A_EVAL_INTERVAL`, `A_COOLDOWN_UP`, `A_COOLDOWN_DOWN`,
  `A_DC_LATENCY`, `A_CONTROLLER_WORKERS`.
- Reserved capacity (subtracted before offering to workloads):
  `A_RESERVED_CPU`, `A_RESERVED_MEMORY`.
- Dashboard listener (admin REST + SSE): `A_DASHBOARD_HOST`,
  `A_DASHBOARD_PORT` (default 7060), `A_DASHBOARD_PREFIX` (default
  `/dashboard/v1`).
- Prometheus exposition: `A_PROMETHEUS_HOST`, `A_PROMETHEUS_PORT`
  (default 7060 — shared with dashboard when equal), `A_PROMETHEUS_PREFIX`
  (default `/metrics`).
- Agent capacity overrides: `A_CPU_TOTAL`, `A_MEMORY_TOTAL`,
  `A_DISK_TOTAL`, `A_SWAP_TOTAL`, `A_DISK_OS_BASELINE`,
  `A_NATS_DISK_BASELINE`, `A_DISK_TYPE`.
- Artifact URL templating (server-side): `A_ARCH` (fallback
  `runtime.GOARCH`), `A_GITHUB_REPO`.
- NATS peer discovery: none — resolved from `A_DOMAIN`'s DNS A-records.
- Agent paths: `A_WORK_DIR` (default `/var/lib/asty`), `A_SERVICE_DIR`
  (default `/etc/asty/services`).

Every `A_*` env Asty consumes is read through `core/config`. CI's
`make layer-check` fails the build if any `os.Getenv` / `os.LookupEnv`
appears outside `core/config` (`Makefile:layer-check` target).

**Gateway-specific env vars** (override fields under `gateway:`):

- `A_GATEWAY_ENABLED` — toggle the embedded gateway on the local node.
- Listener: `A_GATEWAY_HOST` (default `0.0.0.0`),
  `A_GATEWAY_PORT` (default `80`), `A_GATEWAY_PREFIX` (default `/api/v1`).
- HTTP server timeouts: `A_GATEWAY_READ_HEADER_TIMEOUT`,
  `A_GATEWAY_READ_TIMEOUT`, `A_GATEWAY_WRITE_TIMEOUT`,
  `A_GATEWAY_IDLE_TIMEOUT`.
- NATS round-trip: `A_GATEWAY_NATS_REQUEST_TIMEOUT`,
  `A_GATEWAY_NATS_RETRY_DELAY`, `A_GATEWAY_WS_CONNECT_TIMEOUT`.
- `A_ALLOWED_HOSTS` — comma-separated CORS origins.
- Rate limit: `A_GATEWAY_RATE_LIMIT` (per-IP rate),
  `A_GATEWAY_RATE_BURST`, `A_GATEWAY_MAX_WS_CONNS`,
  `A_GATEWAY_TRUSTED_PROXY`, `A_GATEWAY_RATE_LIMIT_MAX_IPS`.

**Local development with multiple nodes**: `start.sh` first sources
`deploy/dev/.env` (secrets + simulated-hardware tunables), then
exports per-node `A_NODE_ID`, `A_NODE_IP`, `A_DASHBOARD_PORT`,
`A_GATEWAY_PORT`, `A_WORK_DIR`, `A_DISK_TYPE` on top of the shared
`config.asty`. Peer discovery is the prod DNS path: `config.asty` sets
`domain: asty.test`, and `start.sh`'s `sync_hosts` maps `asty.test` to
every live node's `127.0.0.$i` in `/etc/hosts` (one A-record each),
rebuilt on start / `add` / `remove` from the per-node pidfiles.

```
deploy/dev/start.sh             # 1 node
deploy/dev/start.sh 3           # 3 nodes (server + agent each)
deploy/dev/start.sh add         # grow the running cluster by one
deploy/dev/start.sh remove [N]  # shrink (graceful: SERVER.REMOVE on the leaver)
deploy/dev/start.sh stop        # tear down everything
```

`add` publishes the new node's `asty.test` A-record, brings up its
loopback alias (`127.0.0.$i`), and starts a fresh server+agent pair.
Existing agents notice the DNS change on their next watcher tick
(~5 s). For a cluster already at N>1 they SIGHUP their `nats-server`
to apply the routes delta live (no downtime); growing from N=1 takes
a cold restart on the existing node because JetStream flips from
standalone to clustered. Either way the leader's `watchStreamReplicas`
then raises replicas on existing KV buckets so the cluster has grown.

`remove` shrinks the cluster. The SIGTERM'd agent runs the
graceful-decommission flow in `agent/natsleave.go` before its
`nats-server` is stopped (see "Graceful node decommission" above).
Surviving agents see the peer-list change and SIGHUP their broker
WITH the `cluster{}` block pinned — the surviving cluster stays
clustered even down to one node.

**Web UI in dev**: `cd asty/web && npm run dev` starts Vite on
`localhost:5173` and proxies `/dashboard` to `localhost:7060`. The
SPA reads `VITE_ASTY_TOKEN` at build time;
`asty/web/.env.development` ships a default that matches
`deploy/dev/.env` `A_TOKEN` so writes (drain, scale, deploy, …)
authenticate cleanly. For production builds (`npm run build`),
inject the token at runtime via an inline
`window.__ASTY_TOKEN__ = "..."` script before the bundle loads.

PID bookkeeping is per-node (`$DATA_BASE/pids-$i`, two lines:
server, agent) so each `add` leaves a self-contained record;
`stop` iterates the whole set.

Authoritative struct layout: `asty/internal/core/config/` —
`config.go`, `nats.go`, `gateway.go`, `env.go`, `load.go`.

### Platform Services
Demo services (xauth, xhttp, xws) keep their `A_` and `X_` env vars: `A_NATS_HOST`/`A_NATS_PORT` for the local NATS, `A_LOG_LEVEL` for zerolog, and `X_*` for service-specific tunables (`X_AUTH_PASSWORD`, `X_HTTP_DATABASE_URL`, …). These are examples only and will be replaced by real business services.

## Configuration conventions

**Comment rules for configs:** see `.claude/coding-rules/comments.md`
(key-focused inlines for `.asty`, block-style for `.vars`).

**dev and prod configs must be structurally identical** (same set of keys, same
nesting). Only VALUES may differ between environments (artifact URL, user,
replicas, allowed_hosts, etc.). Missing keys in one env vs the other count as
structural divergence and are forbidden — see `feedback_dev_prod_sync` memory.

**dev cluster size is variable (1..N nodes, including even N).** Do not assume
dev = single node. Use the same `replicas: 3` as prod and rely on the server's
fallback in `server/kv.go` to reduce replicas when the cluster is smaller.

## Service Definition Format

`.asty` files in `deploy/` directory define deployments. Key fields:
```yaml
name: service-name
type: system | service      # system = 1 per node; service = autoscaled

artifact:
  url: https://.../binary.tar.gz
  checksum: sha256:...

command: ./binary [args]
user: root | nobody
env: { KEY: "value" }

kv:                           # KV buckets provisioned by server before start
  - bucket: my_bucket
    history: 1
    ttl: 24h                  # optional
    replicas: 3               # 0 or omitted = auto (min(cluster_size, 3))

resources:
  cpu: 200      # MHz
  memory: 64    # MB

health:
  type: http
  path: /health
  interval: 10s

restart:
  attempts: 3       # Max restart attempts before giving up (default: 3)
  delay: 5s         # Delay between restart attempts (default: 5s)

update:                      # rolling-update parameters consumed by the deployer
  canary: 1                  # canary copies; 0 skips the canary phase (default: 0)
  canary_retries: 1          # extra canary attempts on health failure (default: 1)
  max_parallel: 2            # rolling-batch size (default: 1)
  min_healthy_time: 10s      # default
  healthy_deadline: 3m       # per-batch budget (default: 3m)
  auto_revert: true          # roll back to the pinned previous version on failure
```

**Restart Policy:**
- When a process exits unexpectedly, the agent automatically attempts to restart it
- After `restart.attempts` failures, the allocation is marked as permanently failed
- Failed allocations are removed and rescheduled to different nodes by the server
- Restart counter resets to 0 on successful start

**Deployment behaviour** (verified against `ops/deployer/` and
`server/deployment.go`):

- **Canary respects `update.canary`** from the `.asty` file. Defaults
  to 0 (no canary phase) via `Update.Canary`. Set it to >0 to opt in.
- **`update.max_parallel`** is normalised by `ServiceDefinition.Resolve`
  to 1 when omitted, so deploy never deadlocks on `MaxParallel == 0`.
- **Versions are pinned at the service level**. KV key
  `service.<name>.version` stores `{current, previous}`. The deployer
  writes it on begin/success/revert; the scheduler's
  `createAllocation` reads it so autoscaler-spawned copies pick up
  the deployed version instead of drifting to "latest". Bootstrap
  (first deploy on a service with no live allocs) just sets the pin
  and lets reconcile create the allocs at that version — no canary
  phase because there's nothing to update.
- **Plan filters and sorts**. `ListAllocations` order is not
  deterministic; `server.DeployService` filters to `IsLive` allocs and
  sorts by `NodeID` ascending before handing the plan to the deployer,
  so canary always picks the same copy on retry.
- **`auto_revert: true` performs a real rollback**. `revertDeployment`
  (`ops/deployer/history.go`) re-dispatches every touched allocation
  at `plan.CurrentVersion` (read from the version pin's `previous`
  field), waits for the batch to be healthy, and on success restores
  `current = previous` in the pin. On rollback failure the deployment
  ends in `StateRollbackFailed`, the service-level
  `ServiceCooldown.RollbackFailed` flag is set in KV, and the
  autoscaler refuses to act on the service until the operator clears
  it via the API.
- **Autoscaler is paused during a deploy.** Deployer sets
  `ServiceCooldown.DeployInProgress = true` on start and false on any
  terminal state; the autoscaler's `EvaluateService` returns noop
  while the flag is set so a stale-version copy can't be placed on a
  node that briefly looks uncovered during a Restarting transition.
- **Deploy is async over HTTP.** `POST /services/{name}/deploy`
  returns 202 immediately with the initial running status; the rollout
  runs in a goroutine on the server lifecycle context. The dashboard
  subscribes to `asty.v1.deploy.progress.<service>` via SSE (GET on
  the same path with `Accept: text/event-stream`) for live progress.
  Concurrent deploys on the same service are refused with 409
  (`deployer.ErrDeployInFlight`).
- **Checksum is optional.** The artifact downloader skips verification
  when the field is empty or contains an unresolved `${...}` placeholder
  (the prod-config pattern). When a real `sha256:...` is supplied it's
  enforced as before.

Variable substitution: `${A_NATS_USER}`, `${VERSION}`, `${ARCH}` expanded from orchestrator's environment.

## Key Implementation Files

### Layered architecture (`asty/internal/`)

Per TZ §3.1 / §12 the tree follows an onion model — L0 core, L1
infra, L2 domain, L3 ops, L4 api — enforced by `make layer-check`
plus depguard rules in `.golangci.yml`. No `features/` directory.

```
asty/internal/
├── core/                          # L0 — no internal/* deps
│   ├── codec/                     # codec.Wire (CBOR) / codec.State —
│   │                              #   single switch point for internal
│   │                              #   serialization (JSON in dev_mode)
│   ├── config/                    # config.go, env.go, gateway.go,
│   │                              #   load.go, nats.go — YAML schema +
│   │                              #   env overrides + Load/Validate.
│   │                              #   The only package permitted to
│   │                              #   call os.Getenv (layer-check
│   │                              #   enforces it).
│   ├── errors/                    # Typed errors (ErrNotLeader, …)
│   ├── natsconf/                  # render.go — builds nats.conf from
│   │                              #   NATSConfig + node identity + peers
│   ├── netutil/                   # host.go (Hostname, LocalIPv4),
│   │                              #   nats.go (ConnectNATS), kv.go
│   ├── types/                     # NodeInfo, ServiceDefinition,
│   │                              #   Allocation, Events, Commands,
│   │                              #   Snapshot, Health, Metrics,
│   │                              #   Scaling, AuditEvent, MustJSON,
│   │                              #   ServiceVersion (deployer pin)
│   └── util/ringbuf/              # generic ring buffer (logs/events)
├── infra/                         # L1 — adapters wrapping external systems
│   ├── kv/                        # JetStream KV (state.go, nodes.go,
│   │                              #   allocations.go, cooldowns.go,
│   │                              #   scale.go, version.go, snapshot.go,
│   │                              #   watch.go, deployments.go)
│   ├── process/                   # exec + monitor + rotation + tail
│   ├── probe/                     # HTTP/NATS health checks (checker.go,
│   │                              #   probe.go) — package is `probe`
│   │                              #   (was `health`)
│   ├── artifact/                  # tar.gz download + sha256 + extract
│   ├── events/                    # EventBuffer ring
│   ├── logs/                      # LogBuffer + NATSWriter + entry.go
│   └── metrics/                   # platform-specific CPU/Memory collector
├── domain/                        # L2 — pure types + FSMs (no I/O)
│   └── proximity/                 # Matrix + sort + validate
├── ops/                           # L3 — use cases (orchestration)
│   ├── reconciler/                # workqueue + reconcile pipeline.
│   │                              #   warmup.go event-debounces the
│   │                              #   first pass until NodeReady set
│   │                              #   stabilises (no missing nodes at
│   │                              #   initial placement).
│   ├── scheduler/                 # placement + DC diversity. helpers.go
│   │                              #   exports PickRemovalVictims (shared
│   │                              #   with manual-scale handler).
│   ├── autoscaler/                # EvaluateService + Execute +
│   │                              #   metrics/store.go (RPS timeseries).
│   │                              #   Skips work when service has
│   │                              #   RollbackFailed or DeployInProgress.
│   ├── deployer/                  # canary + rolling + REAL rollback.
│   │                              #   lifecycle.go holds the in-flight
│   │                              #   guard + KV pin/gate helpers.
│   ├── drainer/                   # parallel migration
│   ├── leader/                    # election + watch
│   └── discovery/                 # DNS node discovery
├── api/                           # L4 — HTTP edges
│   ├── dashboard/                 # admin REST + SSE router under
│   │                              #   cfg.Dashboard.Prefix (default
│   │                              #   /dashboard/v1). Includes
│   │                              #   leaderguard.go, tokenauth.go,
│   │                              #   audit.go middleware.
│   ├── prometheus/                # /metrics exposition, six collectors
│   │                              #   (cluster, nodes, services, allocs,
│   │                              #   deploy, nats). Imports
│   │                              #   `prometheus/client_golang` aliased
│   │                              #   to `prometheusclient` to avoid the
│   │                              #   self-name collision.
│   ├── stream/                    # SSE plumbing + per-resource handlers
│   │                              #   (Cluster, Node, Service, Allocation,
│   │                              #   Nodes, Services, Deploy). dashboard
│   │                              #   imports them through a narrowed
│   │                              #   Context. Deploy filters the
│   │                              #   wildcard `deploy.progress.>` to
│   │                              #   one service.
│   ├── health/                    # GET /health handler
│   └── gateway/                   # /api/v1 (default) embedded gateway —
│                                  #   gateway.go, routing.go, http.go,
│                                  #   websocket.go, wssession.go,
│                                  #   ratelimit.go, middleware.go,
│                                  #   hosts.go, rpsreporter.go, errors.go
├── server/                        # composition root for -mode server
│   ├── server.go                  # Server struct + New
│   ├── boot.go                    # Start (boot sequence)
│   ├── tunables.go                # metricsRetention, streamHubInterval, …
│   ├── context.go                 # ServerContext + DrainDeps getters
│   ├── nats.go, commands.go       # NATS connection + agent RPC
│   ├── deployment.go              # DeployService — async wrapper:
│   │                              #   validates, builds plan from the
│   │                              #   version pin + live allocs (sorted),
│   │                              #   launches goroutine on s.lifeCtx,
│   │                              #   returns 202. Bootstrap path when
│   │                              #   no live allocs: pin + reconcile.
│   ├── artifact.go                # artifact-side helpers
│   ├── leadership.go              # watchLeadership + leader-scoped work
│   ├── logbuffer.go, metrics.go   # NATS log/metrics subscriptions
│   ├── snapshot.go                # ClusterSnapshot builder
│   ├── kv.go                      # provisionKVBuckets, ensureKVBucket,
│   │                              #   autoReplicas (degrade on 10005/10074)
│   ├── streamreplicas.go          # leader-only: watchStreamReplicas
│   │                              #   bumps Replicas via UpdateStream when
│   │                              #   the cluster grows
│   ├── selfremoval.go             # watchSelfRemoval — exits server when
│   │                              #   own node.<id> KV entry is deleted
│   │                              #   (companion to dashboard kill)
│   └── streamhub*.go              # streamhub.go, streamhub_run.go,
│                                  #   streamhub_subs.go, streamhub_pubsub.go.
│                                  #   Four NATS-driven fanouts: snapshots,
│                                  #   drain.progress, cluster events,
│                                  #   deploy.progress.> (per-service).
└── agent/                         # Agent sub-package
    ├── agent.go                   # Agent struct + Start
    ├── natssup.go                 # bootstrapNATS, resolveNATSPeers,
    │                              #   resolveNodeIP, findNATSServerBinary
    ├── natswatch.go               # superviseNATS + watchNATSPeers —
    │                              #   live restart of the nats-server
    │                              #   child on peer-list changes
    ├── natsleave.go               # graceful shutdown:
    │                              #   SERVER.REMOVE + shrinkStreamsToSingle
    │                              #   + transferStreamLeader +
    │                              #   survivingClusterPeers (cluster KV
    │                              #   is the source of truth, not DNS)
    ├── natsstats.go               # STATSZ/JSZ poller → asty_node_nats_*
    ├── gateway.go                 # runGateway + serveGateway
    ├── services.go                # StartService / StopService
    ├── nodeinfo.go                # NodeInfo builder (uses sysinfo_*.go)
    ├── disk.go                    # work_dir disk-usage helpers
    ├── ping.go                    # responds to proximity ping probes
    ├── commands.go                # NATS command handlers (start/stop/getlogs)
    ├── heartbeat.go               # publishHeartbeat / publishProcessMetrics
    ├── restart.go                 # Event-driven restart loop (Process.OnExit)
    ├── logstream.go               # streamProcessLogs (uses Process.Done())
    ├── sysinfo.go                 # cross-platform helpers (env overrides)
    ├── sysinfo_{darwin,linux}.go  # CPU/memory/disk capacity detection
    └── sysinfo_usage_{darwin,linux}.go  # CPU/memory usage sampling
```

`core/natsconf/render.go` builds the nats.conf string from
`config.NATSConfig` + node identity + peer list. It is the single
source of truth for what the supervised nats-server runs with; the
`asty -mode nats-conf` subcommand prints the same output to stdout
for offline inspection.

**File-size rule**: 200-line guideline per Go file — coding rule,
not a CI gate (the Makefile's `layer-check` enforces the env-read
boundary, not file size). Current snapshot has six files over
the cap that should be considered for splitting on the next pass:

  `ops/deployer/history.go`        270
  `ops/deployer/deployer.go`       250
  `api/dashboard/services.go`      235
  `server/snapshot.go`             234
  `ops/drainer/manager.go`         224
  `ops/reconciler/workqueue.go`    214  (k8s-style data structure,
                                         the original documented exception)

**Status enums**: allocation lifecycle (`AllocPending`, `AllocStarting`,
`AllocRunning`, `AllocRestarting`, `AllocStopping`, `AllocStopped`,
`AllocFailed`, `AllocDeleted`) and node lifecycle (`NodeJoining`,
`NodeReady`, `NodeStale`, `NodeDraining`, `NodeDrained`, `NodePaused`,
`NodeDown`, `NodeDeleted`) live in `core/types` as typed strings.
`NodeInfo.EffectiveStatus(now)` folds heartbeat age into the persisted
Status so scheduler/UI code only needs to compare against `NodeReady`
to get the freshness-aware view.

**Polling vs event-driven**: reactive paths use NATS `KV.Watch` and
process callbacks (`Process.OnExit`, `Process.Done()`). Polling is
retained only where there is no event to react to, or as an explicit
safety net: leader TTL refresh (5 s), controller resync safety net
(60 s), streamHub snapshot rebuild (event-driven via a debounced
`KV.Watch` notify, with a safety-net ticker behind it), agent
heartbeat (5 s), process metrics sampling (10 s), gateway RPS
sample-and-report (5 s), HTTP health probes (1 s), TailLogs file
polling (100 ms), proximity validation (1 h), NATS peer re-resolve
(5 s — DNS exposes no watch API), NATS server-stats poll (STATSZ/JSZ,
5 s — NATS monitoring is request-reply, not push), and the leader's
stream-replica upgrade scan (10 s — doubles as the retry for when the
cluster cannot yet place the requested replicas). Protocol keepalives
(SSE/WS pings) and the rate-limiter LRU eviction are timers but not
state polling. Each is documented at its definition.

### Orchestrator Entrypoints
- `asty/cmd/main.go` — imports `agent`, `server`, `config` packages directly (no root asty package).
- `asty/internal/server/` — composition root for `-mode server`: wires L1+L2+L3+L4, runs leader election, mounts the dashboard API.
- `asty/internal/agent/` — composition root for `-mode agent`: supervises NATS + processes, embeds the gateway.
- `asty/internal/api/dashboard/` — REST + SSE router; depends on `ops/*` and `infra/*` only through the `ServerContext` interface declared in `context.go`.
- `asty/internal/core/netutil/` — shared NATS connect / hostname / KV bucket helpers (agent and server both use them).

### Demo Services (`demo/`)
Demo services use `nats.go/micro` directly — no platform SDK, no shared middleware.
- `demo/cmd/{xauth,xhttp,xws}/main.go` — standalone entry points
- `demo/internal/{xauth,xhttp,xws}/` — handlers, config, business logic
- KV buckets are provisioned by the server at deploy time (declared in `.asty` files)
- Services connect to pre-existing buckets via `js.KeyValue(ctx, os.Getenv("A_KV_..."))`

## Development Workflow

1. Modify code
2. Run tests: `go test ./...` or `make test`
3. Build: `make build` (orchestrator only) or `make build-all` (everything)
4. Run locally: `make run-agent` or `make run-server`

### Adding New Service
1. Create `demo/internal/myservice/` with config.go, handlers.go
2. Create `demo/cmd/myservice/main.go` — connect NATS, `micro.AddService`, register endpoints
3. Create `deploy/dev/myservice.asty` — deployment definition with `kv:` section if needed
4. Asty server provisions KV buckets and deploys the binary to target nodes

## Important Notes

- **No Docker/containers** — Asty deploys raw Linux binaries
- **Local NATS only** — each Asty agent supervises its own `nats-server` (configured from `.asty`, see "NATS supervision"); services always connect to it via the per-node `NodeIP`
- **State is authoritative in NATS KV** — not in-memory, not file-based
- **Leader election is automatic** — server mode handles failover via TTL heartbeats
- **Gateway is critical** — it's the only HTTP entry point. **Embedded inside the agent binary**, not a `.asty` service. One per node; togglable via `gateway.enabled` (or `A_GATEWAY_ENABLED=false`) on control-plane-only nodes.
- **Geo-diversity matters** — autoscaler prioritizes spreading services across DCs
- **Bot traffic is filtered** — autoscaler counts only validated RPS from Gateway (authenticated, rate-limited)
- **KV buckets are server-managed** — declared in `.asty` `kv:` section, provisioned at deploy time with auto-replicas and degradation logic. Services just connect to the ready bucket via env var.
