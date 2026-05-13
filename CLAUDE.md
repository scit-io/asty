# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Coding rules (read first)

Conventions established in the Phase-6 refactor of the asty package live in `.claude/coding-rules/`. Read the topic file that matches the work at hand before editing Go code under the asty package root:

- `.claude/coding-rules/README.md` — index of all rules.
- `.claude/coding-rules/file-layout.md` — ≤200-line cap, within-package vs sub-package splits.
- `.claude/coding-rules/code-idioms.md` — stdlib over handwritten, named constants, typed enums.
- `.claude/coding-rules/concurrency.md` — event-driven defaults, acceptable polling list.
- `.claude/coding-rules/architecture.md` — core/features/server/agent split, interfaces at boundaries.
- `.claude/coding-rules/testing.md` — race tests, `testutil/` fixtures.
- `.claude/coding-rules/clarity.md` — write so non-developers can follow.

Only the folder name `asty/` is stable (pattern: `**/asty/`); its parent path *may shift* between refactors — never hard-code it. Look for the directory containing `core/`, `features/`, `server/`, `agent/`.

## Project Overview

**Asty** is a microservices orchestrator with locality-aware autoscaling for NATS-based platforms. Single binary, two modes (server + agent), combining scheduling, autoscaling, and deployment.

The project consists of two main parts:
1. **Asty orchestrator** (`asty/`) — manages cluster state, schedules services, handles autoscaling. Provides HTTP JSON API. Web UI in `asty/web/` (React + Vite + shadcn/ui).
2. **Demo services** (`demo/`) — microservices that Asty deploys (xauth, xhttp, xws). Use `nats.go/micro` directly, no platform SDK. Demo frontend in `demo/web/` (React + Vite).

**Monitoring:** Asty exposes HTTP JSON API at `:4747`. Web UI (`asty/web/`) connects to it for cluster monitoring.
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
go test ./asty/internal/features/gateway -v
go test ./asty/internal/features/scheduling -v

# Run single test
go test ./asty/internal/features/scheduling -v -run TestScheduler
go test ./asty/internal/features/scheduling/proximity -v -run TestProximity

# With race detector
go test -race ./...
```

## Architecture

### Architecture

**Each node runs two processes:**
- `asty -mode server` — participates in leader election, active leader handles scheduling and autoscaling
- `asty -mode agent` — manages local processes, executes commands from server. The HTTP gateway runs inside this process (`features/gateway/`), reusing the agent's NATS connection; `gateway.enabled: false` in config.asty disables it on a given node.

**Leader election:** All servers participate in election via NATS KV. Only one server is active (leader) at any time. When leader fails, remaining servers automatically elect a new leader. This provides high availability with no single point of failure.

Communication between agents and server happens over NATS subjects (`asty.v1.*`). State is stored in NATS JetStream KV.

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

Autoscaler monitors:
1. Gateway valid_rps per node (>5 rps sustained → scale up)
2. Process CPU/Memory (>75% → add copy)
3. DC proximity matrix → place new copies nearest to traffic

Scale down maintains geo-diversity (min 3 copies in different DCs).

### Platform Services Architecture

```
HTTP Client → Gateway (:80) → NATS (127.0.0.1:4222) → [xhttp | xauth | xws]
                                                          ↓
                                                    PostgreSQL (xhttp only)
                                                    NATS KV (xhttp cache, xauth tokens)
```

- **Gateway** (`asty/internal/features/gateway/`) — sole HTTP entry point, embedded in the asty agent process; proxies HTTP → NATS Request-Reply, upgrades WebSocket connections
- **xauth** (`demo/internal/xauth/`) — JWT authentication (HMAC-SHA256), refresh token revocation in NATS KV. Uses `nats.go/micro` directly.
- **xhttp** (`demo/internal/xhttp/`) — demo CRUD with PostgreSQL + NATS KV cache. Uses `nats.go/micro` directly.
- **xws** (`demo/internal/xws/`) — WebSocket session manager. Uses raw `nats.go` (pub/sub, not request-reply).

All inter-service communication is NATS Pub/Sub. No service-to-service HTTP calls.

## Configuration

### Orchestrator (Asty)
Asty (server and agent) reads its configuration from a YAML file via the `-config` flag:

```
asty -mode server -config /etc/asty/config.asty
asty -mode agent  -config /etc/asty/config.asty
```

Without `-config`, the default `./config.asty` is consulted and a missing file is tolerated (env-only deployment). Sections mirror the runtime layout (`nats:`, `autoscale:`, `resources:`, `ui:`, `agent:`, `gateway:`). Sample: `deploy/dev/config.asty`.

**Env-var overrides** (loaded after YAML; `A_` prefix) — useful for per-node values and secrets:

- `A_DOMAIN`, `A_TOKEN` — required outside `dev_mode`
- `A_DATACENTER`, `A_NODE_ID`, `A_NODE_IP`, `A_LOG_LEVEL`
- `A_NATS_HOST`, `A_NATS_PORT`, `A_NATS_USER`, `A_NATS_PASSWORD`
- `A_MIN_COPIES`, `A_TARGET_CPU`, `A_TARGET_MEMORY`, `A_TRAFFIC_RPS_THRESHOLD`, `A_EVAL_INTERVAL`, `A_COOLDOWN_UP`, `A_COOLDOWN_DOWN`
- `A_UI_ADDR`, `A_WORK_DIR`, `A_SERVICE_DIR`
- `A_CPU_TOTAL` / `A_MEMORY_TOTAL` — override auto-detected node capacity

**Gateway-specific env vars** (override fields under `gateway:`):
- `A_GATEWAY_ENABLED` — toggle the embedded gateway on the local node
- `A_HTTP_ADDR`, `A_HTTP_READ_TIMEOUT`, `A_HTTP_WRITE_TIMEOUT`, `A_HTTP_IDLE_TIMEOUT`
- `A_ALLOWED_HOSTS` — comma-separated CORS origins
- `A_GATEWAY_RATE_LIMIT`, `A_GATEWAY_RATE_BURST`, `A_GATEWAY_MAX_WS_CONNS`, `A_GATEWAY_TRUSTED_PROXY`
- `A_GATEWAY_METRICS_ADDR` — Prometheus `/metrics` listener (default `127.0.0.1:8081`)

**Local development with multiple nodes**: `start.sh` exports per-node `A_NODE_ID`, `A_NODE_IP`, `A_NATS_PORT`, `A_UI_ADDR`, `A_WORK_DIR` on top of the shared `config.asty`.

Authoritative struct layout: `internal/platform/asty/core/config/config.go` (+ `gateway.go`, `env.go`, `load.go`).

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
```

**Restart Policy:**
- When a process exits unexpectedly, the agent automatically attempts to restart it
- After `restart.attempts` failures, the allocation is marked as permanently failed
- Failed allocations are removed and rescheduled to different nodes by the server
- Restart counter resets to 0 on successful start

Variable substitution: `${A_NATS_USER}`, `${VERSION}`, `${ARCH}` expanded from orchestrator's environment.

## Key Implementation Files

### Feature-Based Architecture (`asty/internal/`)

```
asty/internal/
├── core/                          # Shared primitives
│   ├── config/                    # Config struct + Load() + Validate()
│   ├── types/                     # NodeInfo, ServiceDefinition, Allocation, Events,
│   │                              #   Commands, Snapshot, typed status enums, MustJSON
│   ├── errors/                    # Typed errors (ErrNotLeader, ErrNodeNotFound)
│   └── netutil/                   # ConnectNATS, EnsureBucket, Hostname, LocalIPv4
├── features/                      # Vertical feature slices
│   ├── api/                       # HTTP API
│   │   ├── api.go, context.go, method.go     # router, ServerContext, methodGuard
│   │   ├── nodes.go, services.go             # node & service handlers
│   │   ├── allocations.go, status.go, autoscaler.go
│   │   ├── stream.go + stream_{cluster,node,service,allocation}.go
│   │   └── logs.go + logs_{cluster,node,allocation}.go
│   ├── clustering/
│   │   ├── controller/            # controller.go, reconcile.go, watch.go,
│   │   │                          #   autoscale.go, workqueue.go
│   │   ├── discovery/             # DNS node discovery
│   │   ├── leader/                # election.go, campaign.go, watch.go
│   │   └── state/                 # state.go, nodes.go, allocations.go,
│   │                              #   watch.go (generic watchKV), services.go
│   ├── scheduling/
│   │   ├── proximity/             # matrix.go, sort.go, validate.go
│   │   ├── scheduler.go, reconcile.go, candidates.go
│   │   └── helpers.go             # LiveAllocations, OccupiedNodes, …
│   ├── autoscaling/
│   │   ├── metrics/               # MetricsStore (RPS timeseries, scaling events)
│   │   ├── autoscaler.go          # EvaluateService + ExecuteScalingDecision
│   │   ├── cooldown.go, scale_up.go, scale_down.go, execute.go
│   ├── deployment/
│   │   ├── artifacts/             # tar.gz download + SHA256 verification
│   │   ├── deployer.go            # struct + Deploy
│   │   ├── canary.go, rolling.go, wait.go, history.go
│   │   └── loader.go              # .asty file loading
│   ├── draining/                  # DrainManager with DrainDeps interface
│   │   └── manager.go, run.go, system.go, migrate.go, wait.go
│   ├── execution/
│   │   ├── process/               # process.go, monitor.go, logs.go
│   │   │                          #   (Process.OnExit, Process.Done())
│   │   └── health/                # checker.go, probe.go
│   ├── gateway/                   # embedded HTTP/WS entry point (runs
│   │                              #   inside the agent process)
│   │   ├── gateway.go             # Gateway struct + New + Handler
│   │   ├── routing.go             # CORS middleware + /v1/ router
│   │   ├── http.go                # HTTP → NATS Request-Reply
│   │   ├── websocket.go           # WS bridge to NATS Pub/Sub
│   │   ├── wssession.go           # ws coordination primitives
│   │   ├── ratelimit.go           # per-IP token-bucket + LRU
│   │   ├── middleware.go          # rate-limit middleware + realIP
│   │   └── errors.go              # NATS error → HTTP status mapping
│   └── observability/
│       ├── metrics/               # CPU/Memory collector (platform-specific)
│       ├── logs/                  # LogBuffer + NATSWriter
│       └── events/                # EventBuffer (ring buffer)
├── server/                        # Server sub-package
│   ├── server.go                  # Server struct + New
│   ├── boot.go                    # Start (boot sequence)
│   ├── tunables.go                # metricsRetention, streamHubInterval, …
│   ├── context.go                 # ServerContext + DrainDeps getters
│   ├── nats.go, commands.go       # NATS connection + agent RPC
│   ├── deployment.go              # DeployService (plan builder)
│   ├── leadership.go              # watchLeadership + leader-scoped work
│   ├── logbuffer.go, metrics.go   # NATS log/metrics subscriptions
│   ├── snapshot.go, allocindex.go # ClusterSnapshot builder
│   └── streamhub*.go              # hub.go, run.go, subs.go (generic
│                                  #   subscribers[T]), pubsub.go
└── agent/                         # Agent sub-package
    ├── agent.go                   # Agent struct + Start
    ├── gateway.go                 # runGateway + serveGateway + metrics server
    ├── services.go                # StartService / StopService
    ├── nodeinfo.go                # NodeInfo builder
    ├── commands.go                # NATS command handlers (start/stop/getlogs)
    ├── heartbeat.go               # publishHeartbeat / publishProcessMetrics
    ├── restart.go                 # Event-driven restart loop (Process.OnExit)
    ├── logstream.go               # streamProcessLogs (uses Process.Done())
    ├── sysinfo_darwin.go          # detectCPUMHz / detectMemoryMB for macOS
    └── sysinfo_linux.go           # detectCPUMHz / detectMemoryMB for Linux
```

**File-size rule**: every Go file is under 200 lines (only exception:
`features/clustering/controller/workqueue.go` at 214 — a cohesive
k8s-style data structure that doesn't benefit from splitting).

**Status enums**: allocation lifecycle (`AllocPending`, `AllocStarting`,
`AllocRunning`, `AllocStopped`, `AllocFailed`, `AllocDeleted`) and node
lifecycle (`NodeReady`, `NodeDraining`, `NodeDrained`, `NodeDown`,
`NodeDeleted`) live in `core/types` as typed strings — the compiler
catches stray literals while the JSON wire format stays unchanged.

**Polling vs event-driven**: reactive paths use NATS `KV.Watch` and
process callbacks (`Process.OnExit`, `Process.Done()`). Polling is
retained only for: leader TTL refresh (5 s), controller resync safety
net (60 s), agent heartbeat (5 s), process metrics sampling (10 s),
HTTP health probes (1 s), TailLogs file polling (100 ms), proximity
validation (1 h). Each is documented at its definition.

### Orchestrator Entrypoints
- `asty/cmd/main.go` — imports `agent`, `server`, `config` packages directly (no root asty package)
- `asty/internal/server/` — leader election, controller wiring, NATS subscriptions, implements `api.ServerContext`
- `asty/internal/agent/` — process management, NATS commands, heartbeat, metrics
- `asty/internal/features/api/` — HTTP API handlers, decoupled from server via `ServerContext` interface
- `asty/internal/core/netutil/` — shared NATS connect / hostname / KV bucket helpers (agent and server both use them)

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
- **Local NATS only** — services always connect to `127.0.0.1:4222`, never remote NATS
- **State is authoritative in NATS KV** — not in-memory, not file-based
- **Leader election is automatic** — server mode handles failover via TTL heartbeats
- **Gateway is critical** — it's the only HTTP entry point; deployed as `type: system` (one per node)
- **Geo-diversity matters** — autoscaler prioritizes spreading services across DCs
- **Bot traffic is filtered** — autoscaler counts only validated RPS from Gateway (authenticated, rate-limited)
- **KV buckets are server-managed** — declared in `.asty` `kv:` section, provisioned at deploy time with auto-replicas and degradation logic. Services just connect to the ready bucket via env var.
