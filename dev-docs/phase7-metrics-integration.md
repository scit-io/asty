# Phase 7: Full Metrics & API Integration — COMPLETED

## Problem

UI отображает только базовую информацию (список нод, аллокации, логи). Множество API endpoints не интегрированы, метрики по сервисам отсутствуют полностью, timeseries данные не используются.

## Gap Analysis

| Feature | Backend API | UI |
|---------|------------|-----|
| Cluster CPU/Memory timeseries | `/api/v1/metrics/cluster` | не используется |
| Node CPU/Memory timeseries | `/api/v1/metrics/nodes/:id` | тип готов, не запрашивается |
| Allocation CPU/Memory timeseries | нет | нет (тип готов) |
| Service list page | `/api/v1/services` | client есть, страницы нет |
| Per-service aggregate metrics | нет | нет |
| Traffic/RPS metrics | только Prometheus (gateway) | нет |
| Deploy form + history | `/api/v1/deploy`, `/api/v1/deployments` | нет |
| Autoscaler events/decisions | нет | нет |
| SSE real-time stream | `/api/v1/stream` | нет |
| Scale action | `/api/v1/services/:name/scale` (TODO) | нет |
| Cluster status cards | `/api/v1/status` | нет (только таблица нод) |

## Plan

### Step 1: Backend — Per-Service Metrics API

**File:** `internal/platform/asty/api.go`

Add endpoints:
```
GET /api/v1/metrics/services/:name?period=1h
```

Response:
```json
{
  "service": "gateway",
  "cpu": [{"timestamp": 123, "value": 45.2}],
  "memory": [{"timestamp": 123, "value": 62.1}],
  "rps": [{"timestamp": 123, "value": 120}],
  "allocations_count": [{"timestamp": 123, "value": 3}],
  "period": "1h"
}
```

**File:** `internal/platform/asty/metrics_store.go`

Extend `collectMetrics()`:
- Add per-service aggregation: iterate allocations, sum CPU/Memory per service
- Store keys: `service.<name>.cpu`, `service.<name>.memory`, `service.<name>.alloc_count`

### Step 2: Backend — Traffic/RPS Collection

**File:** `internal/platform/asty/metrics_store.go`

Gateway reports valid_rps per node via NATS (`asty.v1.metrics.gateway.<node_id>`). Server already subscribes to this for autoscaling.

Extend `collectMetrics()`:
- Subscribe to `asty.v1.metrics.gateway.*` and store RPS per node
- Store keys: `node.<id>.rps`, `cluster.rps`, `service.<name>.rps`

**File:** `internal/platform/asty/api.go`

Extend `/api/v1/metrics/cluster` response with `rps` field.
Extend `/api/v1/metrics/nodes/:id` response with `rps` field.

### Step 3: Backend — Autoscaler Events API

**File:** `internal/platform/asty/autoscaler.go`

Add event recording when scaling decisions are made:
```go
type ScalingEvent struct {
    Timestamp   int64  `json:"timestamp"`
    Service     string `json:"service"`
    Action      string `json:"action"`      // "scale_up" | "scale_down"
    Reason      string `json:"reason"`      // "traffic_rps" | "cpu_threshold" | "memory_threshold"
    FromCount   int    `json:"from_count"`
    ToCount     int    `json:"to_count"`
    NodeID      string `json:"node_id,omitempty"`
}
```

Store in ring buffer (last 1000 events).

**File:** `internal/platform/asty/api.go`

Add endpoint:
```
GET /api/v1/autoscaler/events?service=<name>&limit=100
GET /api/v1/autoscaler/status
```

`/autoscaler/status` response:
```json
{
  "services": {
    "xhttp": {
      "current_copies": 4,
      "min_copies": 3,
      "target_cpu": 75,
      "target_memory": 75,
      "traffic_threshold": 5,
      "cooldown_active": false,
      "last_action": "scale_up",
      "last_action_at": 1234567890
    }
  }
}
```

### Step 4: Backend — Allocation Timeseries Metrics

**File:** `internal/platform/asty/metrics_store.go`

Extend `collectMetrics()`:
- When agent reports allocation metrics via NATS, store per-allocation timeseries
- Keys: `alloc.<id>.cpu`, `alloc.<id>.memory`

**File:** `internal/platform/asty/api.go`

Add endpoint:
```
GET /api/v1/metrics/allocations/:id?period=1h
```

### Step 5: Backend — Deployments Storage

**File:** `internal/platform/asty/deployer.go`

Store deployment history in NATS KV bucket `asty-deployments`:
```go
type DeploymentRecord struct {
    ID          string    `json:"id"`
    Service     string    `json:"service"`
    Version     string    `json:"version"`
    Strategy    string    `json:"strategy"`    // "rolling" | "canary"
    Status      string    `json:"status"`      // "running" | "completed" | "failed" | "reverted"
    StartedAt   time.Time `json:"started_at"`
    CompletedAt time.Time `json:"completed_at,omitempty"`
    Progress    int       `json:"progress"`    // 0-100%
    Nodes       []string  `json:"nodes"`
}
```

Update `handleDeployments()` to return actual data.

### Step 6: UI — Dashboard Overhaul

**File:** `ui/src/pages/dashboard.tsx`

Add to dashboard:
1. **Cluster status cards** (top row): Total Nodes, Healthy Nodes, Total Services, Cluster CPU%, Cluster Memory%, Total RPS
2. **CPU/Memory area charts** using `/api/v1/metrics/cluster`
3. **Services tab** in addition to Nodes tab
4. Integrate SSE `/api/v1/stream` for real-time status updates instead of polling

### Step 7: UI — Services List Page

**File:** `ui/src/pages/services.tsx` (new)

Route: `/services`

Content:
- Table of all services from `/api/v1/services`
- Columns: Name, Type, Copies Running, CPU (aggregate), Memory (aggregate), Health, Actions
- Per-service aggregate data from `/api/v1/metrics/services/:name`
- Click row → navigate to `/services/:name`

**File:** `ui/src/pages/services-detail.tsx` (new)

Route: `/services/:name`

Content:
- Service info card (type, resources, health config)
- Allocations table (all instances across nodes)
- CPU/Memory/RPS charts from `/api/v1/metrics/services/:name`
- Autoscaler status from `/api/v1/autoscaler/status`
- Scaling events timeline from `/api/v1/autoscaler/events?service=<name>`
- Scale action button → `POST /api/v1/services/:name/scale`

### Step 8: UI — Node Detail Metrics Charts

**File:** `ui/src/pages/node-detail.tsx`

Add to Overview tab:
- CPU/Memory area charts from `/api/v1/metrics/nodes/:id`
- RPS chart (when available)

### Step 9: UI — Allocation Detail Metrics Charts

**File:** `ui/src/pages/service-detail.tsx`

Add to Overview tab:
- CPU/Memory area charts from `/api/v1/metrics/allocations/:id`

### Step 10: UI — Deploy Page

**File:** `ui/src/pages/deploy.tsx` (new)

Route: `/deploy`

Content:
- Deploy form: select service, enter version → `POST /api/v1/deploy`
- Deployments history table from `/api/v1/deployments`
- Active deployment progress (if any)

### Step 11: UI — SSE Integration

**File:** `ui/src/hooks/useStream.ts` (new)

Replace polling in dashboard with EventSource to `/api/v1/stream`:
- Receive cluster status updates every 5s
- Update nodes/services state in real-time
- Fallback to polling if SSE disconnects

### Step 12: UI — API Client Extension

**File:** `ui/src/api/client.ts`

Add missing methods:
```typescript
export const api = {
  // ... existing ...

  // Metrics
  getClusterMetrics: (period?: string) =>
    fetchJSON(`${API_BASE}/metrics/cluster?period=${period || '1h'}`),
  getNodeMetrics: (id: string, period?: string) =>
    fetchJSON(`${API_BASE}/metrics/nodes/${id}?period=${period || '1h'}`),
  getServiceMetrics: (name: string, period?: string) =>
    fetchJSON(`${API_BASE}/metrics/services/${name}?period=${period || '1h'}`),
  getAllocationMetrics: (id: string, period?: string) =>
    fetchJSON(`${API_BASE}/metrics/allocations/${id}?period=${period || '1h'}`),

  // Services
  getService: (name: string) =>
    fetchJSON(`${API_BASE}/services/${name}`),
  scaleService: (name: string, count: number) =>
    fetchJSON(`${API_BASE}/services/${name}/scale`, {
      method: 'POST',
      body: JSON.stringify({ count }),
    }),

  // Autoscaler
  getAutoscalerStatus: () =>
    fetchJSON(`${API_BASE}/autoscaler/status`),
  getAutoscalerEvents: (service?: string, limit?: number) =>
    fetchJSON(`${API_BASE}/autoscaler/events?service=${service || ''}&limit=${limit || 100}`),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(`${API_BASE}/deploy`, {
      method: 'POST',
      body: JSON.stringify({ service, version }),
    }),
  getDeployments: () =>
    fetchJSON(`${API_BASE}/deployments`),
}
```

### Step 13: UI — Navigation Update

**File:** `ui/src/App.tsx`

Add routes:
```
/services           → Services List
/services/:name     → Service Detail
/deploy             → Deploy Page
```

**File:** `ui/src/components/header.tsx`

Add nav links: Dashboard | Services | Deploy

## Implementation Order

Backend first (steps 1-5), then UI (steps 6-13). Each step is independently testable.

**Priority:**
1. Steps 1-2 (metrics collection) — foundation for everything
2. Steps 6-8 (dashboard + charts) — immediate visual value
3. Steps 3-4 (autoscaler + allocation metrics) — deeper observability
4. Steps 7, 9 (services page, allocation charts) — complete picture
5. Steps 5, 10 (deploy) — operational tooling
6. Steps 11-13 (SSE, nav) — polish

## Testing

Backend:
```bash
go test ./internal/platform/asty -v -run TestMetricsStore
go test ./internal/platform/asty -v -run TestAPI
```

UI:
```bash
cd ui && pnpm dev
# Verify: charts render with mock data
# Verify: services page loads
# Verify: deploy form works
```

Integration:
```bash
# Start cluster, generate traffic, verify metrics flow end-to-end
./start.sh
curl http://localhost:4646/api/v1/metrics/cluster
curl http://localhost:4646/api/v1/metrics/services/gateway
curl http://localhost:4646/api/v1/autoscaler/status
```
