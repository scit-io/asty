# Рабочий план: realtime-audit исправления

**Последнее обновление:** 2026-05-07  
**Основан на:** реальном состоянии кода, не на документации

---

## Статус: что реально сделано (проверено в коде)

| Пункт | Статус | Где в коде |
|---|---|---|
| P1.1 WriteTimeout=0 | ✅ | api.go:63 |
| P1.2 cluster_metrics в глобальном /stream | ✅ | api.go:787 |
| P1.2 Убрать handleStreamMetricsCluster | ✅ | удалено |
| P2.1 GetAfter + lastMetricsSent | ✅ | api.go:762,841,910; metrics_store.go:72 |
| P2.2 IngestSnapshot, StartCollection удалён | ✅ | streamhub.go:262, metrics_store.go:106 |
| P2.3 maxAge=2h, per-alloc убраны | ✅ | server.go:97; metrics_store.go:108 |
| P3.1 LogBuffer 1000 строк | ✅ | log_buffer.go |
| P3.2 follow=true replay + live | ✅ | api.go:1147,1265,1323 |
| P4.1 allocIndex + WatchNodesInit/AllocationsInit | ✅ | streamhub.go, state.go |
| P4.2 notify channel + event-triggered refresh | ✅ | streamhub.go:159 |
| alloc.* → alloc.> bug fix | ✅ | state.go:451 |
| P5 ClusterEvent ring buffer | ✅ | event_buffer.go, api.go, streamhub.go |
| Секция 10: JetStream метрики (MetricsCache) | ❌ | не начато |
| Секция 11: Health checks сквозно | ✅ | service.go, agent.go, health.go |

---

## Оставшиеся задачи

### Задача 1 — Удалить мёртвый endpoint handleStreamMetricsCluster

**Размер:** малый (удаление)  
**Файлы:** `api.go`

Что сделать:
- Удалить регистрацию `mux.HandleFunc("/api/v1/stream/metrics/cluster", ...)` (строка 49)
- Удалить `handleStreamMetricsCluster` функцию (строки 1015–1060)
- Удалить `"stream_metrics"` из ответа discovery handler (строка 113)

Убедиться что фронтенд не использует этот endpoint (проверено: `cluster.tsx` и `cluster.ts` не обращаются к нему).

---

### Задача 2 — P5: История кластерных событий

**Размер:** средний  
**Файлы:** `server.go`, `api.go`, `streamhub.go`

#### 2a. Тип и буфер

В `server.go` добавить:

```go
type ClusterEvent struct {
    Timestamp int64       `json:"ts"`
    Type      string      `json:"type"` // "scale_up", "scale_down", "restart", "deploy", "node_join", "node_leave", "alloc_failed"
    Service   string      `json:"service,omitempty"`
    NodeID    string      `json:"node_id,omitempty"`
    AllocID   string      `json:"alloc_id,omitempty"`
    Details   string      `json:"details,omitempty"`
}
```

Кольцевой буфер в `Server`:
```go
eventBuf   []ClusterEvent
eventMu    sync.RWMutex
```
Максимум 10 000 событий. Метод `addEvent(e ClusterEvent)`.

#### 2b. Где писать события

| Место в коде | Событие |
|---|---|
| `autoscaler.go` — после scale up | `"scale_up"` |
| `autoscaler.go` — после scale down | `"scale_down"` |
| `scheduler.go` — CreateAllocation | `"alloc_created"` (опционально) |
| `controller.go` — после удаления failed alloc | `"alloc_failed"` |
| `server.go` — WatchNodes delete | `"node_leave"` |
| `server.go` — WatchNodes new node | `"node_join"` |

#### 2c. SSE

В глобальном `/stream` хендлере добавить event `cluster_event` — пушить при каждом новом событии (через fan-out или отдельный subscribeEvents канал).

#### 2d. REST

`handleEvents` — убрать заглушку, возвращать последние N событий из кольцевого буфера.

---

### Задача 3 — Секция 11: Health checks сквозно

**Размер:** средний  
**Файлы:** `agent.go`, `health.go`, `service.go` (`.asty` формат)

**Проблема:** `healthChecker.Register` закомментирован в `agent.go:183` — агент не знает порт процесса.

**Решение: фиксированный `health.addr` в `.asty` формате**

Самое простое — добавить необязательное поле `health.addr` в ServiceDefinition:

```yaml
health:
  type: http
  addr: ":8080"   # новое поле, опционально; если не задан — health probe отключён
  path: /health
  interval: 10s
```

В Go-структуре `HealthCheck`:
```go
type HealthCheck struct {
    ...
    Addr string `yaml:"addr"` // например ":8080"
}
```

Раскомментировать и исправить в `agent.go:183`:
```go
if svc.Health.Type == "http" && svc.Health.Addr != "" {
    a.healthChecker.Register(svc.Name, svc.Health.Addr, svc.Health.Path,
        svc.Health.GetInterval(), svc.Health.GetTimeout())
}
```

**Связать результат с KV**: в `publishProcessMetrics` добавить `HealthStatus`:
```go
healthy := a.healthChecker.IsHealthy(serviceName)
healthStatus := "healthy"
if !healthy { healthStatus = "unhealthy" }

err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *ServiceAllocation) bool {
    alloc.CPUUsage = cpu
    alloc.MemoryUsage = mem
    alloc.HealthStatus = healthStatus
    return true
})
```

Так `HealthStatus` будет доходить до UI через allocIndex → snapshot.

---

### Задача 4 — Стабильный порядок нод в UI

**Размер:** малый  
**Файлы:** `ui/` (фронтенд)

**Проблема:** ноды в UI постоянно меняются местами — визуально выглядит как «сервисы прыгают из ноды в ноду», хотя на самом деле это проблема сортировки на клиенте.

**Решение:** сортировать ноды по времени первой регистрации в кластере (поле `RegisteredAt` / `FirstSeen` в `NodeInfo`), а не по текущему состоянию или алфавиту. Порядок фиксируется однажды при первом появлении ноды и не меняется при обновлениях метрик/статусов.

Если поля времени регистрации нет в `NodeInfo` — добавить `RegisteredAt int64` (unix timestamp) в `state.go` при создании ноды (`CreateNode` / первый `Put`).

---

### Задача 5 — Секция 10: JetStream метрики (крупный рефактор)

**Размер:** большой — отдельная сессия  
**Файлы:** `agent.go`, `server.go`, `api.go`, `metrics_store.go`, `streamhub.go`

Суть: удалить MetricsStore, заменить на:
- JetStream stream `METRICS` (subjects: `metrics.>`, MaxMsgsPerSubject=1)
- `MetricsCache` в сервере — `map[subject]MetricMsg` + fan-out channels
- Агент публикует `metrics.node.{id}` и `metrics.node.{id}.service.{name}` в JetStream
- Сервер подписан на `metrics.>` → обновляет MetricsCache → fan-out в SSE
- `/metrics` Prometheus endpoint из MetricsCache

**Порядок реализации секции 10:**
1. Добавить `MetricsCache` struct (map + fan-out + Subscribe/Unsubscribe)
2. Создать JetStream stream `METRICS` при старте (server + agent)
3. Агент: `publishMetrics()` — читает /proc, публикует node + node.service в JetStream
4. Сервер: подписка на `metrics.>` → `MetricsCache.put()`
5. `PublishServiceAggregates` на сервере (агрегаты по сервису → JetStream)
6. SSE handlers — подписаться на MetricsCache fan-out вместо MetricsStore
7. `/metrics` Prometheus endpoint
8. Удалить MetricsStore, IngestSnapshot, subscribeGatewayMetrics, collector на сервере

---

## Рекомендуемый порядок работ

```
Задача 1 (15 мин) → Задача 2 (1-2 ч) → Задача 3 (1 ч) → Задача 4 (30 мин) → Задача 5 (4-6 ч)
```

Задачи 1–4 независимы. Задача 5 — отдельная большая сессия, после 1–4.

---

## Что НЕ трогать

- Архитектура SSE hub (allocIndex + snapshot) — работает корректно
- Drain через NATS pub/sub — event-driven, не трогать
- LogBuffer и log streaming — работает
- Delta streaming (lastMetricsSent + GetAfter) — работает, уберётся при выполнении задачи 4
