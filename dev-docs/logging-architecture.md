# Logging Architecture

## Overview

Asty имеет **три уровня логирования**, каждый со своим назначением, источником данных и способом доступа.

```
┌─────────────────────────────────────────────────────────────┐
│                    1. CLUSTER LEVEL                         │
│  Asty Server Events (lifecycle, scheduling, deployments)    │
│  API: GET /api/v1/logs/cluster?follow=true                 │
│  UI: Dashboard → Logs tab                                   │
└─────────────────────────────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     2. NODE LEVEL                           │
│  Agent Events (heartbeat, process management, health)       │
│  API: GET /api/v1/logs/node/{node_id}?follow=true         │
│  UI: Node Detail → Logs tab                                 │
└─────────────────────────────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    3. SERVICE LEVEL                         │
│  Process stdout/stderr (application logs)                   │
│  API: GET /api/v1/logs/allocation/{id}?follow=true        │
│  UI: Service Detail → Logs tab                              │
└─────────────────────────────────────────────────────────────┘
```

---

## 1. Cluster Level (Asty Server)

### Назначение
Логи работы **оркестратора как системы**: события сервера, решения планировщика, деплойменты, состояние кластера.

### Источник данных
- **ClusterLogger** (`cluster_logger.go`) — event bus для важных событий
- Публикует в NATS subject: `asty.v1.server.logs`
- События из модулей:
  - `autoscaler.go` — scaling up/down decisions
  - `deployer.go` — deployment started/completed/failed
  - `leader.go` — leader election changes
  - `server.go` — node discovery, cluster state changes

### Примеры логов
```json
{"level":"info","time":"2026-05-05T00:00:00Z","message":"leader election: acquired leadership"}
{"level":"info","service":"xhttp","desired":3,"time":"2026-05-05T00:01:00Z","message":"scaling service"}
{"level":"info","service":"gateway","version":"v1.2.0","time":"2026-05-05T00:02:00Z","message":"deployment started"}
{"level":"warn","node":"node-1","time":"2026-05-05T00:03:00Z","message":"node heartbeat missed"}
```

### API Endpoint
```bash
# JSON snapshot
GET /api/v1/logs/cluster?lines=100

# SSE streaming
GET /api/v1/logs/cluster?follow=true&lines=100
```

### UI Access
**Dashboard → "Logs" tab**

- Панель вкладок на главной странице: "Nodes" | "Logs"
- Live streaming с "Live" badge
- Показывает события всего кластера

### Implementation Status
✅ **Implemented**

- ✅ API endpoint реализован (`/api/v1/logs/cluster`)
- ✅ UI tab добавлен (Dashboard → Logs)
- ✅ ClusterLogger публикует события в NATS (`asty.v1.server.logs`)
- ✅ События логируются из: autoscaler, deployer, leader election, node discovery
- ✅ SSE streaming реальных событий кластера

---

## 2. Node Level (Asty Agent)

### Назначение
Логи работы **агента на конкретной ноде**: lifecycle процессов, health checks, metrics collection, взаимодействие с NATS.

### Источник данных
- **AgentLogger** (использует `ClusterLogger`) — event bus для важных событий агента
- Публикует в NATS subject: `asty.v1.agent.{node_id}.logs.agent`
- События из модулей:
  - `agent.go` — agent started, service started/stopped
  - `process.go` — process lifecycle events
  - Полные stdout/stderr логи агента доступны через systemd/docker

### Примеры логов
```json
{"level":"info","node_id":"node-1","time":"2026-05-05T00:00:00Z","message":"agent starting"}
{"level":"info","service":"xhttp","pid":12345,"time":"2026-05-05T00:01:00Z","message":"process started"}
{"level":"warn","service":"gateway","time":"2026-05-05T00:02:00Z","message":"health check failed"}
{"level":"error","service":"xauth","pid":12346,"time":"2026-05-05T00:03:00Z","message":"process exited unexpectedly"}
```

### API Endpoint
```bash
# JSON snapshot
GET /api/v1/logs/node/{node_id}?lines=100

# SSE streaming (TODO)
GET /api/v1/logs/node/{node_id}?follow=true&lines=100
```

### UI Access
**Node Detail → "Logs" tab**

- Доступно после клика на ноду в таблице
- Показывает события конкретного агента
- Полезно для debugging node-specific issues

### Implementation Status
✅ **Implemented**

- ✅ API endpoint реализован (`/api/v1/logs/node/{id}`)
- ✅ UI tab добавлен (Node Detail → Logs)
- ✅ Agent публикует важные события в NATS (`asty.v1.agent.{node_id}.logs.agent`)
- ✅ События: agent started, service started/stopped
- ✅ SSE streaming для real-time мониторинга агента

**Note**: Полные stdout/stderr логи агента доступны через systemd (`journalctl -u asty-agent`) или docker logs. UI показывает только важные события.

---

## 3. Service Level (Application Logs)

### Назначение
Логи **запущенных сервисов** (Gateway, xhttp, xauth, xws): application stdout/stderr, request logs, business logic.

### Источник данных
- **Process stdout/stderr** (`process.go:setupLogs`)
- Файлы: `{workdir}/logs/{service_name}.log`
- Tail + NATS streaming: `asty.v1.agent.{node_id}.logs.{service_name}`

### Примеры логов
```json
{"level":"info","service":"gateway","method":"GET","path":"/health","status":200,"duration":1,"time":"2026-05-05T00:00:00Z","message":"request completed"}
{"level":"warn","service":"xhttp","user_id":"123","time":"2026-05-05T00:01:00Z","message":"rate limit exceeded"}
{"level":"error","service":"xauth","error":"connection refused","time":"2026-05-05T00:02:00Z","message":"failed to connect to database"}
```

### API Endpoint
```bash
# JSON snapshot
GET /api/v1/logs/allocation/{alloc_id}?lines=100

# SSE streaming
GET /api/v1/logs/allocation/{alloc_id}?follow=true&lines=100
```

### UI Access
**Service Detail → "Logs" tab**

- Доступно после клика на allocation в таблице ноды
- Real-time streaming с auto-scroll
- Live badge индикатор + Clear button

### Implementation Status
✅ **Fully Implemented**

- ✅ Process пишет stdout/stderr в файл
- ✅ `Process.TailLogs()` читает новые строки
- ✅ Agent публикует в NATS real-time
- ✅ Server forwarding через SSE
- ✅ UI с live streaming

---

## Comparison Table

| Level    | Source              | API Endpoint                 | UI Location            | Status          |
|----------|---------------------|------------------------------|------------------------|-----------------|
| Cluster  | Server events       | `/api/v1/logs/cluster`      | Dashboard → Logs       | ✅ Implemented  |
| Node     | Agent events        | `/api/v1/logs/node/{id}`    | Node Detail → Logs     | ✅ Implemented  |
| Service  | Process stdout/stderr| `/api/v1/logs/allocation/{id}`| Service Detail → Logs | ✅ Implemented  |

---

## Use Cases

### 1. Cluster-Level Debugging
**Scenario**: "Почему сервис не масштабируется?"

- Смотрим **Cluster Logs**
- Ищем решения autoscaler: `"scaling service"`, `"insufficient resources"`
- Видим, какие ноды были рассмотрены для placement

### 2. Node-Level Debugging
**Scenario**: "Почему на node-2 падают все сервисы?"

- Смотрим **Node Logs** для node-2
- Видим: `"out of memory"`, `"process killed by OOM"`
- Диагностируем проблему с ресурсами на конкретной ноде

### 3. Service-Level Debugging
**Scenario**: "Почему xhttp возвращает 500?"

- Смотрим **Service Logs** для allocation xhttp
- Видим application errors: `"database connection failed"`
- Находим root cause в application logic

---

## Implementation Roadmap

### Phase 1: Service Logs ✅ (Done)
- [x] Process log files
- [x] Tail + NATS streaming
- [x] SSE API endpoint
- [x] UI with live streaming

### Phase 2: Cluster Logs 🚧 (Current)
- [x] API endpoint with placeholder
- [x] UI tab on Dashboard
- [ ] Wire server zerolog to NATS stream
- [ ] Structured events (scheduling, deployments)

### Phase 3: Node Logs (Future)
- [ ] Agent log collection mechanism
- [ ] NATS streaming or file-based tail
- [ ] API endpoint implementation
- [ ] UI tab in Node Detail

### Phase 4: Enhancements (Future)
- [ ] Log search & filtering
- [ ] Parse JSON logs (zerolog) → structured UI
- [ ] Log retention & rotation
- [ ] Export to external systems (Loki, S3)

---

## Technical Notes

### NATS Subjects Convention

```
asty.v1.logs.cluster              → Cluster-level events
asty.v1.agent.{node_id}.logs.agent → Node-level agent logs
asty.v1.agent.{node_id}.logs.{svc} → Service-level application logs
```

### Zerolog Output Format

All Asty components use zerolog with JSON output:

```go
log.Info().
    Str("service", "xhttp").
    Int("pid", 12345).
    Msg("process started")
```

Output:
```json
{"level":"info","service":"xhttp","pid":12345,"time":"2026-05-05T00:00:00Z","message":"process started"}
```

### SSE Protocol

All log endpoints support SSE streaming via `?follow=true`:

```
data: {"line": "log message", "timestamp": 1234567890}

data: {"line": "another message", "timestamp": 1234567891}
```

UI uses `EventSource` API to consume streams.

### Performance Considerations

- **Service logs**: High volume (1000s lines/sec) → consider batching
- **Node logs**: Medium volume (100s lines/sec) → direct streaming OK
- **Cluster logs**: Low volume (10s lines/sec) → direct streaming OK

---

## Conclusion

Три уровня логирования покрывают весь lifecycle приложения:

1. **Cluster** — что делает оркестратор
2. **Node** — что делает агент на каждой ноде
3. **Service** — что делает само приложение

Это позволяет эффективно debugging на любом уровне абстракции.
