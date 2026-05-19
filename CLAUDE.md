# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Coding rules (read first)

Conventions established in the Phase-6 refactor of the asty package live in `.claude/coding-rules/`. Read the topic file that matches the work at hand before editing Go code under the asty package root:

- `.claude/coding-rules/README.md` — index of all rules.
- `.claude/coding-rules/file-layout.md` — ≤200-line cap, within-package vs sub-package splits.
- `.claude/coding-rules/code-idioms.md` — stdlib over handwritten, named constants, typed enums.
- `.claude/coding-rules/concurrency.md` — event-driven defaults, acceptable polling list.
- `.claude/coding-rules/architecture.md` — L0..L4 onion (core / infra / domain / ops / api) + composition roots, interfaces at boundaries.
- `.claude/coding-rules/testing.md` — race tests, `testutil/` fixtures.
- `.claude/coding-rules/clarity.md` — write so non-developers can follow.

Only the folder name `asty/` is stable (pattern: `**/asty/`); its parent path *may shift* between refactors — never hard-code it. Look for the directory containing `core/`, `infra/`, `domain/`, `ops/`, `api/`, `server/`, `agent/`.

## Project Overview

**Asty** is a microservices orchestrator with locality-aware autoscaling for NATS-based platforms. Single binary, two modes (server + agent), combining scheduling, autoscaling, and deployment.

The project consists of two main parts:
1. **Asty orchestrator** (`asty/`) — manages cluster state, schedules services, handles autoscaling. Provides HTTP JSON API. Web UI in `asty/web/` (React + Vite + shadcn/ui). **The orchestrator must remain agnostic of any specific managed service** — no `xauth`/`xhttp`/`xws` names, no demo-shaped paths or NATS subjects, no service-specific assumptions inside this package. Any leakage is a Critical defect.
2. **Demo services** (`demo/`) — microservices that Asty deploys (xauth, xhttp, xws). Use `nats.go/micro` directly, no platform SDK. Demo frontend in `demo/web/` (React + Vite). **These services, together with `deploy/{dev,prod}/`, `Makefile`'s `build-demo` target, and the coding-rule examples that reference them, are intentional customer-facing boilerplate.** The buyer of the platform removes them when shipping their own services. Mentions of demo names outside `asty/` are by design, not findings.

**Monitoring:** Asty's admin surface lives at `:7060` by default — dashboard REST + SSE at `/dashboard/v1/`, Prometheus exposition at `/metrics` (same listener), liveness at `/health`. Web UI (`asty/web/`) connects to it for cluster monitoring. Both port and prefix are configurable per surface (see Observability §).
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
   The hot path is taken whenever both the old and new conf carry a
   `cluster{}` block — i.e. the JetStream mode itself does not flip.

   A cold restart (SIGTERM → re-`bootstrapNATS`) only happens for the
   `standalone ↔ clustered` transition (first peer joining, or the
   last peer leaving), which structurally requires a fresh JS init.
   `nats.go`-based clients reconnect automatically across the cold
   path; JS data survives because the store directory does.

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

`resolveNATSPeers` checks three sources in order — first one with a
non-empty value wins. Self-IP is filtered out so a node never routes
to itself.

1. `A_NATS_PEERS_FILE` — path to a file with one IP per line (or
   comma-separated). Used in dev to imitate a DNS A-record: `start.sh`
   maintains it, agents re-read on every watcher tick, `start.sh
   add` appends a line to grow the cluster live.
2. `A_NATS_PEERS` — comma-separated env var. Static, doesn't change
   during process lifetime; useful for CI and one-shot launches.
3. DNS `LookupIP(cfg.Domain)` — the prod path. Operators add/remove
   nodes by editing the A-record; agents pick up changes on the
   watcher's next tick.

A single-node cluster boots with an empty peer list — `render.go`
omits the `cluster{}` block and NATS runs in standalone JetStream
mode. KV buckets land with `replicas=1`; when peers appear and the
broker is restarted in clustered mode, `server/streamreplicas.go`
(leader-only) upgrades existing streams via `UpdateStream`. The pair
forms a symmetric loop: `server/kv.go:ensureKVBucket` degrades on
create when the cluster can't place the requested replicas (10005 or
10074), and `watchStreamReplicas` raises them back up later.

For offline inspection of what would be written:
`asty -mode nats-conf -config <path> -peers <ip1,ip2,...>` prints the
rendered nats.conf to stdout without launching anything.

### Service Deployment Model

Asty deploys services as **raw binaries** (not containers):
- Services defined in `.asty` files (YAML declarative format, see `services/*.asty`)
- Agent downloads binaries from URLs with checksum verification
- Process lifecycle: start → health check → run → graceful shutdown (SIGTERM → SIGKILL)
- Two service types:
  - `type: system` — one copy per node (currently only platform demos; the HTTP gateway is no longer a `.asty` service — it lives inside the agent binary)
  - `type: service` — autoscaled based on load (e.g., xauth, xhttp)

### Locality-Aware Autoscaling

Traffic is routed by geographic load balancer to the nearest node. The gateway on that node validates requests and serves them **locally** (same-node NATS at `127.0.0.1:4222`). This is Asty's main differentiator from generic orchestrators.

Autoscaler monitors (`ops/autoscaler/scale_up.go`):
1. Gateway valid-RPS per node — sustained average ≥ `TrafficRPSThreshold`
   (default 5) over a 60s window (`trafficWindow`) → scale up onto the
   ready node that has traffic but no local copy.
2. Resource pressure on an existing copy: `CPUUsage` (%) > `TargetCPU`
   (default 75) **OR** `MemoryUsage` (MB) > `TargetMemory` (default 75).
   The CPU check is "over 75%" as expected; the memory check is "over
   75 MB" — `MemoryUsage` is stored in MB (`types/allocation.go:47`),
   so `TargetMemory=75` is a literal 75 MB floor, not a percent, despite
   the symmetric naming with `TargetCPU`.
3. Scheduler's `PickCandidates` (incl. DC proximity matrix) chooses the
   target node for the new copy.

Scale down (`scale_down.go`) honours `MinCopies` (default 3) and adds
hysteresis: average usage across running copies must drop below
`TargetCPU/2` AND `TargetMemory/2` (`idleFloorDivisor=2`). The victim
is chosen from the most-crowded DC to preserve geo-diversity.

### Reconciliation controller

The `ops/reconciler/` package owns the active control
loop and **runs only on the elected leader** (`server/leadership.go`
starts it via `startLeaderWork`, `watchLeadership` flips it on/off when
leadership changes). Followers run no reconciler; their server process
is otherwise identical (still serves the API, still subscribes to log
buffers) but does not place or dispatch allocations.

Inside the controller (`controller.go`):
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
  net. API handlers (`scale`, `restart`, `stop`, `deploy`) call
  `Enqueue(name)` directly so user actions don't wait on the tick.
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
- **Regular allocations** are processed **sequentially** in the order
  returned by `collectAllocs` — `run.go:39` is a plain `for ... range`
  with no per-alloc goroutine. For each: `SelectNearestForReplacement`
  picks a target (or falls back to letting the controller place it),
  `waitForHealthyOnNode` / `waitForHealthyReplacement` blocks up to
  `drainHealthDeadline = 2m`, and only then does `finalizeMigration`
  (Stop + wait + delete on the source) run — that last step *is* in a
  goroutine, so finalize is parallel across already-placed
  replacements.
- After all goroutines join, `completeNodeDrain` CAS's the node to
  `NodeDrained` and publishes a final status event on
  `asty.v1.drain.progress` (NATS subject, JSON), which `streamHub`
  fans out to SSE subscribers.
- `Resume(nodeID)` cancels the drain context and CAS's the node back
  to `NodeReady`.

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
- `/dashboard/v1/services`, `/dashboard/v1/services/{name}/allocations`,
  `/dashboard/v1/services/{name}/autoscaler`,
  `/dashboard/v1/services/{name}/deploy`.

`/health` stays at the root for kube-probes. The dashboard prefix
comes from `cfg.Dashboard.Prefix`; the SPA reads the matching
`API_PREFIX` from `asty/web/src/api/client.ts` — change both in
lockstep when re-configuring.

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
  - `leaderOnly` 307-redirects followers to the leader.
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
| `asty_alloc_*` | per-allocation | `service`, `node_id`, `alloc_id` (+ `state` on `_health`, `status` on `_status`) | `cpu_percent`, `memory_mb`, `disk_mb`, `restarts_total`, `uptime_seconds`, `health`, `status` |
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
- NATS peer discovery: `A_NATS_PEERS_FILE`, `A_NATS_PEERS`.
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

**Local development with multiple nodes**: `start.sh` exports per-node
`A_NODE_ID`, `A_NODE_IP`, `A_DASHBOARD_PORT`, `A_GATEWAY_PORT`,
`A_WORK_DIR`, `A_DISK_TYPE` on top of the shared `config.asty`, and
points all agents at `A_NATS_PEERS_FILE=/tmp/asty-dev/peers.txt` for
live peer discovery.

```
deploy/dev/start.sh        # 1 node
deploy/dev/start.sh 3      # 3 nodes (server + agent each)
deploy/dev/start.sh add    # grow the running cluster by one
deploy/dev/start.sh stop   # tear down everything
```

`add` appends the new node's IP to `peers.txt`, brings up its
loopback alias (`127.0.0.$i`), and starts a fresh server+agent pair.
Existing agents notice the file change on their next watcher tick
(~5 s). For a cluster already at N>1 they SIGHUP their `nats-server`
to apply the routes delta live (no downtime); growing from N=1 takes
a cold restart on the existing node because JetStream flips from
standalone to clustered. Either way the leader's `watchStreamReplicas`
then raises replicas on existing KV buckets so the cluster has grown.

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
  max_parallel: 2            # REQUIRED if you ever call Deploy — see caveat below
  min_healthy_time: 10s      # default
  healthy_deadline: 3m       # default
  progress_deadline: 10m     # default (currently unused by the deployer loop)
  auto_revert: true          # see caveat below
```

**Restart Policy:**
- When a process exits unexpectedly, the agent automatically attempts to restart it
- After `restart.attempts` failures, the allocation is marked as permanently failed
- Failed allocations are removed and rescheduled to different nodes by the server
- Restart counter resets to 0 on successful start

**Deployment Caveats** (verified against `ops/deployer/` and
`server/deployment.go`):

- **Canary count is hard-coded to `1`** in `server.DeployService`
  (`server/deployment.go:63`). There is no `update.canary` field on
  `ServiceDefinition` — every deploy first updates one allocation, waits
  for it to be healthy, and only then enters the rolling phase. To skip
  canary you'd need to change the server code, not the `.asty` file.
- **`update.max_parallel` must be > 0** if you intend to deploy. The
  server pipes the value straight into the deployer without a default,
  and `rolling.go:29` does `for i := 0; i < len(remaining); i += MaxParallel`
  — a zero or negative value here is an infinite loop. The `.asty`
  loader does not enforce a minimum.
- **`auto_revert: true` now performs a real rollback** (TZ §4.4).
  `revertDeployment` (`ops/deployer/history.go`) re-dispatches every
  touched allocation at `CurrentVersion` and waits for the batch to be
  healthy. On failure the deployment ends in the new `StateRollbackFailed`
  terminal state and the service-level `ServiceCooldown.RollbackFailed`
  flag is set in KV; the autoscaler reads this flag and refuses to act
  on the service until the operator clears it via the API.
- `update.canary_retries` (default 1) governs how many times an
  unhealthy canary is re-dispatched before the deploy fails (TZ §4.4).
- `current_version` displayed in deployment records is read from
  `allocs[0].Version` (`server/deployment.go:42`) — fine for a uniform
  cluster, but during a half-finished previous deploy it just shows
  whichever version happened to come first.

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
│   │                              #   Scaling, AuditEvent, MustJSON
│   └── util/ringbuf/              # generic ring buffer (logs/events)
├── infra/                         # L1 — adapters wrapping external systems
│   ├── kv/                        # JetStream KV (state.go, nodes.go,
│   │                              #   allocations.go, cooldowns.go,
│   │                              #   scale.go, snapshot.go, watch.go)
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
│   ├── reconciler/                # workqueue + reconcile pipeline
│   ├── scheduler/                 # placement + DC diversity
│   ├── autoscaler/                # EvaluateService + Execute +
│   │                              #   metrics/store.go (RPS timeseries)
│   ├── deployer/                  # canary + rolling + REAL rollback
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
│   │                              #   Nodes, Services). dashboard imports
│   │                              #   them through a narrowed Context.
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
│   ├── deployment.go              # DeployService (plan builder)
│   ├── artifact.go                # artifact-side helpers
│   ├── leadership.go              # watchLeadership + leader-scoped work
│   ├── logbuffer.go, metrics.go   # NATS log/metrics subscriptions
│   ├── snapshot.go                # ClusterSnapshot builder
│   ├── kv.go                      # provisionKVBuckets, ensureKVBucket,
│   │                              #   autoReplicas (degrade on 10005/10074)
│   ├── streamreplicas.go          # leader-only: watchStreamReplicas
│   │                              #   bumps Replicas via UpdateStream when
│   │                              #   the cluster grows
│   └── streamhub*.go              # streamhub.go, streamhub_run.go,
│                                  #   streamhub_subs.go, streamhub_pubsub.go
└── agent/                         # Agent sub-package
    ├── agent.go                   # Agent struct + Start
    ├── natssup.go                 # bootstrapNATS, resolveNATSPeers,
    │                              #   resolveNodeIP, findNATSServerBinary
    ├── natswatch.go               # superviseNATS + watchNATSPeers —
    │                              #   live restart of the nats-server
    │                              #   child on peer-list changes
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

**File-size rule**: every Go file is under 200 lines. Only one
documented exception: `ops/reconciler/workqueue.go` (214 — a cohesive
k8s-style data structure that doesn't benefit from splitting). CI
fails the build on any new file over the cap.

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
retained only for: leader TTL refresh (5 s), controller resync safety
net (60 s), agent heartbeat (5 s), process metrics sampling (10 s),
HTTP health probes (1 s), TailLogs file polling (100 ms), proximity
validation (1 h). Each is documented at its definition.

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
