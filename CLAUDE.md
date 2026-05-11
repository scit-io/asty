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

**Asty** is a microservices orchestrator with locality-aware autoscaling for NATS-based platforms. It replaces Nomad with a simpler, integrated solution that combines scheduling, autoscaling, and deployment in a single binary.

The project consists of two main parts:
1. **Asty orchestrator** (`internal/platform/asty/`) — manages cluster state, schedules services, handles autoscaling. Provides HTTP JSON API (no built-in UI).
2. **Platform services** (`internal/services/`, `cmd/`) — microservices that Asty deploys (Gateway, xauth, xhttp, xws)

**Monitoring:** Asty exposes only HTTP JSON API. For Web UI monitoring, use **ui** (separate React application in `ui/` directory).

## Build Commands

```bash
# Build orchestrator only
go build -o asty ./cmd/asty

# Build all services (orchestrator + microservices)
go build -o bin/ ./cmd/asty ./cmd/gateway ./cmd/xauth ./cmd/xhttp ./cmd/xws

# Using Makefile
make build      # Build asty binary only
make test       # Run all tests
make run-agent  # Run in agent mode
make run-server # Run in server mode
```

## Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/platform/asty -v
go test ./internal/services/gateway -v

# Run single test
go test ./internal/platform/asty -v -run TestScheduler
go test ./internal/platform/asty -v -run TestProximity

# With race detector
go test -race ./...
```

## Architecture

### Architecture

**Each node runs two processes:**
- `asty -mode server` — participates in leader election, active leader handles scheduling and autoscaling
- `asty -mode agent` — manages local processes, executes commands from server

**Leader election:** All servers participate in election via NATS KV. Only one server is active (leader) at any time. When leader fails, remaining servers automatically elect a new leader. This provides high availability with no single point of failure.

Communication between agents and server happens over NATS subjects (`asty.v1.*`). State is stored in NATS JetStream KV.

### Service Deployment Model

Asty deploys services as **raw binaries** (not containers), similar to Nomad's `raw_exec` driver:
- Services defined in `.asty` files (YAML declarative format, see `services/*.asty`)
- Agent downloads binaries from URLs with checksum verification
- Process lifecycle: start → health check → run → graceful shutdown (SIGTERM → SIGKILL)
- Two service types:
  - `type: system` — one copy per node (e.g., Gateway)
  - `type: service` — autoscaled based on load (e.g., xauth, xhttp)

### Locality-Aware Autoscaling

The key differentiator from Nomad. Traffic is routed by geographic load balancer to nearest node. Gateway on that node validates requests and serves them **locally** (same-node NATS at `127.0.0.1:4222`).

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

- **Gateway** (`internal/services/gateway/`) — sole HTTP entry point, proxies HTTP → NATS Request-Reply, upgrades WebSocket connections
- **xauth** (`internal/services/xauth/`) — JWT authentication (HMAC-SHA256), refresh token revocation in NATS KV
- **xhttp** (`internal/services/xhttp/`) — demo CRUD with PostgreSQL + NATS KV cache
- **xws** (`internal/services/xws/`) — WebSocket session manager

All inter-service communication is NATS Pub/Sub. No service-to-service HTTP calls.

## Configuration

### Orchestrator (Asty)
Environment variables with `A_` prefix:
- `A_DOMAIN` — DNS for node discovery (required)
- `A_DATACENTER` — datacenter name (default: dc1)
- `A_NODE_ID` — explicit node ID (default: auto-generated from hostname)
- `A_NODE_IP` — explicit node IP address (default: auto-detected; for loopback NATS connections uses A_NATS_HOST)
- `A_NATS_HOST`, `A_NATS_PORT` — NATS connection (default: 127.0.0.1:4222)
- `A_NATS_USER`, `A_NATS_PASSWORD` — NATS auth
- `A_LOG_LEVEL` — zerolog level (debug/info/warn/error)
- `A_MIN_COPIES` — minimum service replicas (default: 3)
- `A_TARGET_CPU`, `A_TARGET_MEMORY` — autoscaling thresholds (default: 75%)
- `A_TRAFFIC_RPS_THRESHOLD` — sustained RPS to trigger scale-up (default: 5)
- `A_CPU_TOTAL` — override total CPU MHz (default: auto-detect from system)
- `A_MEMORY_TOTAL` — override total Memory MB (default: auto-detect from system)

**Local development with multiple nodes**: Set `A_NODE_IP` and `A_NATS_HOST` to unique loopback IPs (e.g., 127.0.0.2, 127.0.0.3) for each agent.

See `internal/platform/asty/core/config/config.go` for full list.

### Platform Services
Environment variables with `A_` prefix:
- `A_NATS_HOST`, `A_NATS_PORT` — local NATS (always 127.0.0.1:4222)
- `A_NATS_USER`, `A_NATS_PASSWORD`
- `A_ALLOWED_HOSTS` — comma-separated CORS origins for Gateway
- `A_LOG_LEVEL` — zerolog level

Demo services (xauth, xhttp, xws) use `X_` prefix (e.g., `X_AUTH_PASSWORD`, `X_HTTP_DATABASE_URL`). These are examples only and will be replaced by real business services.

## Service Definition Format

`.asty` files in `services/` directory define deployments. Key fields:
```yaml
name: service-name
type: system | service      # system = 1 per node; service = autoscaled

artifact:
  url: https://.../binary.tar.gz
  checksum: sha256:...

command: ./binary [args]
user: root | nobody
env: { KEY: "value" }

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

### Feature-Based Architecture (`internal/platform/asty/`)

```
internal/platform/asty/
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
- `cmd/asty/main.go` — imports `agent`, `server`, `config` packages directly (no root asty package)
- `server/` — leader election, controller wiring, NATS subscriptions, implements `api.ServerContext`
- `agent/` — process management, NATS commands, heartbeat, metrics
- `features/api/` — HTTP API handlers, decoupled from server via `ServerContext` interface
- `core/netutil/` — shared NATS connect / hostname / KV bucket helpers (agent and server both use them)

### Platform Layer
- `internal/platform/nc/` — NATS client wrapper (JetStream, KV helpers)
- `internal/platform/logger/` — zerolog factory with service name
- `internal/platform/metrics/` — Prometheus metrics (HTTP, NATS, WebSocket)
- `internal/middleware/` — NATS message middleware (recover, JWT auth)
- `utils/` — reply helpers, env parsing, CORS validation, cookie handling

### Services
Gateway is the only production component. x-services (xauth, xhttp, xws) are demos showing platform usage patterns.

## Development Workflow

1. Modify code
2. Run tests: `go test ./...` or `make test`
3. Build: `make build` (orchestrator only) or `go build -o bin/ ./cmd/...` (all services)
4. Run locally: `make run-agent` or `make run-server`

### Adding New Service
1. Create `internal/services/myservice/` with config.go, handlers.go
2. Create `cmd/myservice/main.go` — load config, connect NATS, register handlers
3. Create `services/myservice.asty` — deployment definition
4. Asty will download and run the binary on target nodes

## Integration with platform.go

This project was initially split from `../platform.go`. Common code:
- `internal/platform/nc/`, `logger/`, `metrics/` — copied 1:1
- `internal/services/` and `cmd/` for Gateway, xauth, xhttp, xws — copied 1:1
- Import paths changed from `platform/` to `asty/`

If updating shared code (nc, logger, metrics), consider syncing changes back to platform.go if applicable.

## Important Notes

- **No Docker/containers** — Asty deploys raw Linux binaries
- **Local NATS only** — services always connect to `127.0.0.1:4222`, never remote NATS
- **State is authoritative in NATS KV** — not in-memory, not file-based
- **Leader election is automatic** — server mode handles failover via TTL heartbeats
- **Gateway is critical** — it's the only HTTP entry point; deployed as `type: system` (one per node)
- **Geo-diversity matters** — autoscaler prioritizes spreading services across DCs
- **Bot traffic is filtered** — autoscaler counts only validated RPS from Gateway (authenticated, rate-limited)

## Technical Documentation

See `dev-docs/` for detailed implementation notes:
- `README.md` — development plan with 6 phases
- `architecture.md` — Nomad → Asty mapping
- `autoscaling.md` — locality-aware algorithm details
- `configuration.md` — all A_* variables, .asty format reference
- `monitoring.md` — metrics, Web UI design
- `phase*.md` — completion notes for each phase
