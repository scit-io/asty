# Asty

Microservices orchestrator with locality-aware autoscaling for NATS-based platforms.

Single binary: agent + server. Configuration files: `.asty`. Namespace: `asty`.

## Architecture

```
Client → LB (geo) → Gateway (:80) → NATS (127.0.0.1:4222) → Service (same node)
```

**On each node:** NATS, Asty Agent, Gateway (type=system), services (0..N).  
**One per cluster:** Asty Server (scheduling, autoscaling, Web UI, deployments) — leader election via NATS.

Agent ↔ Server communication via NATS subjects `asty.v1.*`. State stored in NATS JetStream KV.

## Project Structure

```
asty/
  cmd/asty/main.go                       — entry point, single binary
  internal/platform/asty/
    agent.go                             — agent lifecycle
    server.go                            — server lifecycle
    process.go                           — start/stop processes
    health.go                            — HTTP health checks
    collector.go                         — CPU/Memory metrics per process
    logs.go                              — log rotation
    artifact.go                          — binary download + checksum
    scheduler.go                         — locality-aware placement
    autoscaler.go                        — scaling decisions
    deployer.go                          — rolling update, canary
    leader.go                            — leader election (NATS JetStream)
    discovery.go                         — node discovery via DNS
    state.go                             — cluster state (JetStream KV)
    proximity.go                         — DC latency matrix
    config.go                            — A_* environment variables
    service.go                           — .asty definition parser
    api.go                               — HTTP API (REST)
    handlers.go                          — handlers: nodes, services, deploy
    ui.go                                — embedded Web UI
  deployments/systemd/asty.service       — systemd unit
  services/                              — service definitions (.asty)
```

## Service Types

- **system** — one copy per node (e.g., Gateway)
- **service** — managed by autoscaler, placement based on load

## Locality-Aware Autoscaling

Requests are processed locally. Gateway reports only validated traffic to the autoscaler (authenticated, passed rate limiting). Placement triggers on sustained flow — threshold of 5 valid rps over a sliding 1-minute window.

**Scale UP:**
1. Gateway valid traffic on node without service (>5 rps, 1m window) → launch copy
2. Process overloaded (CPU/Memory >75%) → add copy to same node
3. Node resources exhausted → nearest node in same DC
4. DC full → nearest DC by latency matrix

**Scale DOWN:** remove copies from least loaded nodes, maintain geo-diversity (min=3 in different DCs), cooldown 5m.

## Configuration

Environment variables with `A_` prefix. Services defined in `.asty` files (declarative YAML).

## Deployment

Rolling update: canary → health check → promote (max_parallel) → auto_revert on failure.

## Web UI

Built-in admin panel at `127.0.0.1:4646` (SSH tunnel). Nodes, services, deployments, logs, alerts.

## Add New Node

```bash
wget -qO- https://raw.githubusercontent.com/org/asty/main/setup.sh | \
  A_DOMAIN=nodes.example.com \
  A_DATACENTER=eu-west \
  bash
```

## Development

See `dev-docs/` for technical documentation, implementation plan, and architecture details.

## Build

```bash
make build     # Build binary
make test      # Run tests
make run-agent # Run agent mode
```
