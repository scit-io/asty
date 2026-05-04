# API Endpoints Documentation

## Overview

Asty API provides RESTful endpoints for cluster management and monitoring.

Base URL: `http://localhost:4747/api/v1`

## Implemented Endpoints

### Cluster Status

#### `GET /api/v1/status`
Returns cluster-wide status.

**Response:**
```json
{
  "cluster": {
    "leader": "agent-1",
    "is_leader": true,
    "nodes_total": 3,
    "nodes_healthy": 3
  },
  "services": {
    "loaded": 4
  }
}
```

### Nodes

#### `GET /api/v1/nodes`
List all cluster nodes.

**Response:**
```json
{
  "nodes": [
    {
      "id": "agent-1",
      "datacenter": "dc1",
      "status": "ready",
      "cpu_total": 8000,
      "cpu_available": 6000,
      "memory_total": 16384,
      "memory_available": 12000,
      "processes": ["gateway", "xauth"],
      "last_seen": "2026-05-04T10:00:00Z"
    }
  ],
  "count": 1
}
```

#### `GET /api/v1/nodes/:id`
Get node details.

**Response:** Same as single node object above.

#### `POST /api/v1/nodes/:id/drain`
Drain node (stop scheduling new allocations).

**Response:**
```json
{
  "node_id": "agent-1",
  "action": "drain",
  "message": "drain initiated (not yet fully implemented)"
}
```

#### `POST /api/v1/nodes/:id/pause`
Pause node.

**Response:**
```json
{
  "node_id": "agent-1",
  "action": "pause",
  "message": "pause initiated (not yet fully implemented)"
}
```

### Services

#### `GET /api/v1/services`
List loaded service definitions.

**Response:**
```json
{
  "services": [
    {
      "Name": "gateway",
      "Type": "system",
      "Resources": {
        "CPU": 200,
        "Memory": 128
      },
      "Health": {
        "Type": "http",
        "Path": "/health",
        "Interval": "10s",
        "Timeout": "5s"
      }
    }
  ],
  "count": 1
}
```

#### `GET /api/v1/services/:name`
Get service details with allocations.

**Response:**
```json
{
  "service": { /* ServiceDefinition */ },
  "allocations": [ /* array of allocations */ ]
}
```

### Allocations

#### `GET /api/v1/allocations?service=<name>`
List allocations for a service.

**Query params:**
- `service` (required) — service name
- `node_id` (optional) — filter by node

**Response:**
```json
{
  "service": "gateway",
  "allocations": [
    {
      "id": "alloc-123",
      "service_name": "gateway",
      "node_id": "agent-1",
      "status": "running",
      "version": "v1.0.0",
      "pid": 12345,
      "started_at": "2026-05-04T10:00:00Z",
      "health_status": "healthy",
      "cpu_usage": 10,
      "memory_usage": 64,
      "restarts": 0,
      "created_at": "2026-05-04T09:55:00Z",
      "updated_at": "2026-05-04T10:00:00Z"
    }
  ],
  "count": 1
}
```

#### `GET /api/v1/allocations/:id`
Get allocation details.

**Response:** Single allocation object (same structure as above).

#### `POST /api/v1/allocations/:id/restart`
Restart allocation.

**Response:**
```json
{
  "allocation_id": "alloc-123",
  "action": "restart",
  "message": "restart initiated (not yet fully implemented)"
}
```

#### `POST /api/v1/allocations/:id/stop`
Stop allocation.

**Response:**
```json
{
  "allocation_id": "alloc-123",
  "action": "stop",
  "message": "stop initiated (not yet fully implemented)"
}
```

### Logs

#### `GET /api/v1/logs/node/:id`
Get node logs.

**Query params:**
- `lines` (optional, default: 100) — number of log lines

**Response:**
```json
{
  "node_id": "agent-1",
  "lines": 100,
  "logs": [
    "[2026-05-04T10:00:00Z] Node agent started",
    "[2026-05-04T10:00:01Z] Connected to NATS server"
  ]
}
```

#### `GET /api/v1/logs/allocation/:id`
Get allocation logs.

**Query params:**
- `lines` (optional, default: 100) — number of log lines
- `follow` (optional, default: false) — stream logs via SSE

**Response (follow=false):**
```json
{
  "allocation_id": "alloc-123",
  "lines": 100,
  "logs": [
    {"timestamp": 1746360000, "message": "[INFO] Service started"},
    {"timestamp": 1746360001, "message": "[INFO] Listening on :8080"}
  ]
}
```

**Response (follow=true):** SSE stream with `data:` events.

### Deployments

#### `POST /api/v1/deploy`
Initiate deployment.

**Request:**
```json
{
  "service": "gateway",
  "version": "v1.0.1"
}
```

**Response:**
```json
{
  "deployment_id": "deploy-123",
  "service": "gateway",
  "version": "v1.0.1",
  "status": "pending"
}
```

#### `GET /api/v1/deployments`
List deployment history.

**Response:**
```json
{
  "deployments": [],
  "count": 0
}
```

### Metrics

#### `GET /api/v1/metrics/cluster?period=1h`
Get cluster-wide metrics.

**Query params:**
- `period` (optional, default: 1h) — time period (e.g., 1h, 6h, 24h)

**Response:**
```json
{
  "cpu": [
    {"timestamp": 1746360000, "value": 45.5},
    {"timestamp": 1746360300, "value": 50.2}
  ],
  "memory": [
    {"timestamp": 1746360000, "value": 60.0}
  ],
  "period": "1h"
}
```

#### `GET /api/v1/metrics/nodes/:id?period=1h`
Get per-node metrics.

**Response:**
```json
{
  "node_id": "agent-1",
  "cpu": [ /* MetricPoint[] */ ],
  "memory": [ /* MetricPoint[] */ ],
  "period": "1h"
}
```

### Events

#### `GET /api/v1/events?since=<timestamp>&limit=100`
Get cluster events.

**Response:**
```json
{
  "events": [],
  "count": 0
}
```

### Streaming (SSE)

#### `GET /api/v1/stream?topics=status,nodes`
Server-Sent Events stream for real-time updates.

**Query params:**
- `topics` (optional) — comma-separated list of topics to subscribe

**Events:**
- `status` — cluster status updates (every 5s)
- `node` — node state changes
- `allocation` — allocation state changes

**Example:**
```
event: status
data: {"cluster": {"leader": "agent-1", ...}, "timestamp": 1746360000}

event: node
data: {"node_id": "agent-1", "status": "ready", ...}
```

## Health & Metrics

#### `GET /health`
Health check.

**Response:**
```json
{
  "status": "ok",
  "timestamp": 1746360000
}
```

#### `GET /metrics`
Prometheus metrics (text format).

**Response:**
```
# HELP asty_nodes_total Total number of nodes
# TYPE asty_nodes_total gauge
asty_nodes_total 3

# HELP asty_nodes_healthy Number of healthy nodes
# TYPE asty_nodes_healthy gauge
asty_nodes_healthy 3
```

## Implementation Status

✅ **Fully implemented:**
- `/api/v1/status`
- `/api/v1/nodes` (list)
- `/api/v1/nodes/:id` (detail)
- `/api/v1/services` (list)
- `/api/v1/services/:name` (detail)
- `/api/v1/allocations` (list with filters)
- `/api/v1/allocations/:id` (detail)
- `/api/v1/metrics/cluster`
- `/api/v1/metrics/nodes/:id`
- `/api/v1/stream` (SSE)

⚠️ **Partially implemented (placeholders):**
- `/api/v1/nodes/:id/drain` — returns success but doesn't execute
- `/api/v1/nodes/:id/pause` — returns success but doesn't execute
- `/api/v1/allocations/:id/restart` — returns success but doesn't execute
- `/api/v1/allocations/:id/stop` — returns success but doesn't execute
- `/api/v1/logs/node/:id` — returns placeholder logs
- `/api/v1/logs/allocation/:id` — returns placeholder logs

❌ **Not implemented (TODO):**
- `/api/v1/deployments` — empty response
- `/api/v1/events` — empty response
- Actual action execution (drain, pause, restart, stop)
- Real log streaming from agents

## Error Responses

All endpoints return errors in this format:

```json
{
  "error": "human-readable message",
  "status": 404,
  "detail": "technical error details"
}
```

Common status codes:
- `400` — Bad Request (invalid parameters)
- `404` — Not Found (resource doesn't exist)
- `405` — Method Not Allowed (wrong HTTP method)
- `500` — Internal Server Error
- `503` — Service Unavailable (not leader)
