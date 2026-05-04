# Asty Quick Start Guide

## Prerequisites

- **NATS Server** with JetStream enabled
- **Go 1.21+** for building
- **Node.js 18+** and **pnpm** for UI development

## Build

```bash
# Build Asty orchestrator
go build -o bin/asty ./cmd/asty

# Build UI
cd ui
pnpm install
pnpm build
cd ..
```

## Running Asty

### Option 1: Server with Mock Nodes (for testing UI)

**Recommended for UI development and testing.**

Start NATS server with JetStream:

```bash
# Using Docker (easiest)
docker run -d --name asty-nats -p 4222:4222 -p 8222:8222 nats:latest -js

# Or using local NATS binary
nats-server -js
```

Start Asty server with mock nodes:

```bash
# Enable dev mode with 3 mock nodes
A_DEV_MODE=true A_MOCK_NODES=3 ./bin/asty -mode server
```

This will:
- ✅ Create 3 mock nodes (mock-node-1, mock-node-2, mock-node-3)
- ✅ Load service definitions from `./deployments/infra/*.asty`
- ✅ Scheduler creates allocations (status: pending)
- ✅ Start API server on `:4747`
- ✅ UI fully functional with real data
- ❌ NOT run actual processes (needs agents)

**Wait 30-60 seconds** for scheduler to create allocations, then:

**Access UI:** Open http://localhost:4747

You should see:
- 3 nodes in "ready" status
- 4 services (gateway, xauth, xhttp, xws)
- ~12 allocations total (3 for each service)

### Option 2: Production Mode (requires DNS setup)

For real cluster deployment:

```bash
# Configure DNS
export A_DOMAIN=asty.example.com      # DNS A records point to node IPs
export A_TOKEN=your-cluster-token

# Start server
./bin/asty -mode server
```

Server discovers nodes via DNS A records. Each IP becomes a node.

### Option 3: Full Cluster (with agents)

**Not yet implemented** — requires:
- Agent mode implementation complete
- Service binaries available for download
- Artifact URLs configured in `.asty` files

## Configuration

Set environment variables:

```bash
# Required
export A_DOMAIN=localhost               # DNS domain for node discovery
export A_DATACENTER=dc1                 # Datacenter name

# NATS connection
export A_NATS_HOST=127.0.0.1
export A_NATS_PORT=4222
export A_NATS_USER=""                   # Optional
export A_NATS_PASSWORD=""               # Optional

# API server
export A_UI_ADDR=:4747                  # HTTP API listen address

# Logging
export A_LOG_LEVEL=info                 # debug, info, warn, error

# Autoscaling (optional)
export A_MIN_COPIES=3                   # Minimum service replicas
export A_TARGET_CPU=75                  # CPU threshold for scaling (%)
export A_TARGET_MEMORY=75               # Memory threshold for scaling (%)
```

## Viewing the UI

### Production Build

1. Build UI: `cd ui && pnpm build`
2. Start server: `./bin/asty -mode server`
3. Open: http://localhost:4747

The built UI is served from `ui/dist/`.

### Development Mode

For UI development with hot reload:

```bash
# Terminal 1: Start Asty server
./bin/asty -mode server

# Terminal 2: Start UI dev server
cd ui
pnpm dev
```

Open http://localhost:5173 (dev server with API proxy to `:4747`)

## What You'll See

**Dashboard:**
- Cluster stats (nodes, services)
- Nodes table with status, CPU, memory
- Click node → node detail page

**Node Detail:**
- Overview tab: resource usage
- Services tab: allocations running on node
- Logs tab: node logs (placeholder)
- Actions tab: drain/pause buttons

**Service Detail:**
- Overview tab: allocation details
- Health tab: health check status
- Logs tab: service logs (placeholder)
- Actions tab: restart/stop buttons

## Current Status

✅ **Working:**
- Server mode with leader election
- Service definitions loading from `.asty` files
- Scheduler creates allocations
- API endpoints respond with data
- UI displays nodes, services, allocations
- Theme switching (light/dark)

⚠️ **Placeholder:**
- Allocations stay in "pending" (no agents to run them)
- Logs return placeholder data
- Actions return success but don't execute

❌ **Not Implemented:**
- Agent mode (process execution)
- Artifact download
- Log streaming from processes
- Health checks
- Actual restart/stop/drain actions

## Troubleshooting

### No services loaded

**Check:**
```bash
ls -la deployments/infra/*.asty
```

Services should be in `deployments/infra/` not `services/`.

### No nodes in dashboard

Server discovers nodes via DNS A records for `A_DOMAIN`.
Without DNS setup, no nodes will be discovered.

**Quick fix:** Manually register nodes via NATS KV (server does this automatically for testing).

### NATS connection failed

**Check NATS is running:**
```bash
nc -zv localhost 4222
```

If using Docker:
```bash
docker ps | grep nats
docker logs nats
```

### Old data in UI

NATS JetStream KV persists data. To reset:

```bash
# Stop Asty
pkill -f bin/asty

# Delete KV bucket (requires nats CLI)
nats kv rm asty-cluster --force

# Or restart NATS server
docker restart nats
```

## Next Steps

1. **Implement Agent Mode** — run processes, health checks, log collection
2. **Build Service Binaries** — compile gateway, xauth, xhttp, xws
3. **Configure Artifacts** — host binaries, update `.asty` files with real URLs
4. **Test Full Deployment** — server schedules, agent runs, health checks pass

## API Documentation

See `dev-docs/api-endpoints.md` for complete API reference.

## Development

See:
- `dev-docs/ui.md` — UI architecture
- `dev-docs/README.md` — development plan
- `ui/TESTING.md` — UI testing guide
