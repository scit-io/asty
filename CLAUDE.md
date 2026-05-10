# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

See `internal/platform/asty/config.go` for full list.

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
│   ├── types/                     # Node, ServiceDefinition, Allocation, Events, Commands
│   └── errors/                    # Typed errors (ErrNotLeader, ErrNodeNotFound)
├── features/                      # Vertical feature slices
│   ├── clustering/
│   │   ├── controller/            # ServiceController + Workqueue (reconciliation engine)
│   │   ├── discovery/             # DNS node discovery
│   │   ├── leader/                # TTL-based leader election
│   │   └── state/                 # ClusterState — NATS KV (nodes, allocations, watch)
│   ├── scheduling/
│   │   ├── proximity/             # DC latency matrix
│   │   └── *.go                   # Scheduler, helpers, placement logic
│   ├── autoscaling/
│   │   ├── metrics/               # MetricsStore (RPS timeseries, scaling events)
│   │   └── autoscaler.go          # Scale-up/down decisions
│   ├── deployment/
│   │   ├── artifacts/             # tar.gz download + SHA256 verification
│   │   ├── deployer.go            # Rolling updates, canary, auto-revert
│   │   └── loader.go              # .asty file loading
│   ├── draining/                  # DrainManager with DrainDeps interface
│   ├── execution/
│   │   ├── process/               # Process lifecycle (start/stop/signals)
│   │   └── health/                # HTTP probe scheduler
│   └── observability/
│       ├── metrics/               # CPU/Memory collector (platform-specific)
│       ├── logs/                   # LogBuffer + NATSWriter
│       └── events/                # EventBuffer (ring buffer)
├── agent.go, agent_commands.go, agent_lifecycle.go  # Agent entry
├── server.go                      # Server entry (thin orchestrator)
├── api_*.go                       # HTTP API handlers (split by domain)
├── streamhub.go                   # SSE snapshot hub
└── *.go                           # Backward-compatible type alias wrappers
```

### Orchestrator Entrypoints
- `agent.go` + `agent_commands.go` + `agent_lifecycle.go` — process management, NATS commands, heartbeat, metrics
- `server.go` — leader election, controller wiring, NATS subscriptions
- `api_setup.go` — HTTP mux + helpers; `api_nodes.go`, `api_services.go`, `api_allocations.go`, `api_autoscaler.go`, `api_logs.go`, `api_stream.go`, `api_status.go`
- `streamhub.go` — periodic snapshot + SSE fanout
- `controller.go` / `workqueue.go` — thin aliases to `features/clustering/controller/`

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
