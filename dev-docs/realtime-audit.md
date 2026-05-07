# Аудит реального времени: UI ↔ Backend

**Дата аудита:** 2026-05-07  
**Дата реализации P1–P3:** 2026-05-07  
**Контекст:** Данные в UI не обновляются в реальном времени. Требуется аудит SSE/Polling, план для event-driven архитектуры, учёт графиков с историей и масштаб 1000 сервисов × 1000 нод.

---

## 0. Состояние процессов на машине

```
Load Avg: 2.50, 5.24, 5.82
CPU: 34% user, 29% sys — нагрузка есть, но она от Termius renderer (~10% CPU) и VS Code
Asty-процессы (9 серверов + 4 агента): по 0.0–0.2% CPU — норма
```

Asty тут ни при чём. Нагрузка — от внешних приложений.

---

## 1. КРИТИЧЕСКИЙ БАГ: WriteTimeout убивает SSE через 10 секунд

**Файл:** `internal/platform/asty/api.go:62-63`

```go
api.httpServer = &http.Server{
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      10 * time.Second,  // ← убивает SSE!
}
```

`WriteTimeout` применяется ко всем соединениям, включая SSE. SSE — это long-lived HTTP-соединение, сервер пишет в него неопределённо долго. Через 10 секунд Go закрывает соединение, клиент получает ошибку, EventSource делает exponential backoff reconnect, получает одну порцию данных — и снова отключается.

**Визуальный эффект:** UI показывает данные примерно раз в 10-60 секунд, а не live.

**Исправление:**
```go
api.httpServer = &http.Server{
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      0,  // SSE-соединения долгоживущие
}
```

Или per-connection — вызвать `http.NewResponseController(w).SetWriteDeadline(time.Time{})` в начале каждого SSE-хендлера. Это изящнее, но оба варианта работают.

---

## 2. Карта всех UI ↔ Backend взаимодействий

### 2.1 SSE-потоки (актуальное состояние после P1–P3)

| Endpoint | Кто открывает | Частота событий | Что несёт |
|---|---|---|---|
| `GET /api/v1/stream` | `App.tsx` → `initSSE()` | каждые 5с (hub tick) | status, nodes, services, **cluster_metrics** (delta) |
| `GET /api/v1/stream/node/:id` | `subscribeNode()` | каждые 5с | allocations + metrics (delta, история на первом connect) |
| `GET /api/v1/stream/service/:name` | `subscribeService()` | каждые 5с | detail + allocations + metrics (delta) |
| `GET /api/v1/stream/allocation/:id` | `subscribeAllocation()` | каждые 5с | detail только (no per-alloc metrics) |
| ~~`GET /api/v1/stream/metrics/cluster`~~ | ~~cluster.tsx~~ | — | **удалён**, метрики теперь в `/stream` |
| `GET /api/v1/logs/cluster?follow=true` | `cluster.tsx` useEffect | push (NATS sub) | история из LogBuffer + live логи сервера |
| `GET /api/v1/logs/node/:id?follow=true` | `node-detail.tsx` | push (NATS sub) | история из LogBuffer + live логи агента |
| `GET /api/v1/logs/allocation/:id?follow=true` | `service-detail.tsx` | push (NATS sub) | история из LogBuffer + live логи процесса |

### 2.2 REST-запросы (текущие)

| Endpoint | Когда | Тип |
|---|---|---|
| `GET /api/v1/services/:name` | открытие service-overview | одноразовый |
| `GET /api/v1/allocations/:id` | открытие service-detail | одноразовый |
| `GET /api/v1/logs/allocation/:id` | открытие service-detail | одноразовый (история) |
| `GET /api/v1/autoscaler/status` | service-overview | одноразовый |
| `GET /api/v1/autoscaler/events` | service-overview | одноразовый |
| `GET /api/v1/deployments` | deploy page | одноразовый |
| `POST /api/v1/...` | мутации (drain, scale, deploy) | одноразовый |

### 2.3 ~~Проблема: Cluster страница открывает 3 SSE-соединения~~ ✅ Исправлено

~~`cluster.tsx` открывал `/api/v1/stream/metrics/cluster` отдельно.~~  
Метрики кластера перенесены в глобальный `/api/v1/stream` как event `cluster_metrics`.  
Cluster страница теперь открывает 2 SSE-соединения: `/stream` и `/logs/cluster`.

---

## 3. Архитектурные проблемы

### 3.1 ~~Два независимых цикла сбора данных~~ ✅ Исправлено

~~`metricsStore.StartCollection()` и `streamHub.refresh()` оба читали NATS KV независимо.~~

Реализован `metricsStore.IngestSnapshot(snap)` — метрики теперь выводятся из snapshot, который hub уже строит.  
`StartCollection()` и `collectMetrics()` удалены. KV-нагрузка от MetricsStore: **0 читов/сек**.

### 3.2 ~~SSE-потоки шлют всю историю метрик на каждый тик~~ ✅ Исправлено

~~На каждый тик клиент получал 360 точек (час при интервале 10с).~~

Реализована delta-модель: `lastMetricsSent time.Time` в каждом SSE-хендлере. На первом connect — полная история (2ч), на каждом следующем тике — только новые точки (обычно 1).  
Добавлен `GetAfter(key, after time.Time)` в MetricsStore.

Frontend: `appendMetrics()` в store накапливает дельты с ограничением `MAX_CHART_POINTS = 1440`.

### 3.3 ~~Масштаб in-memory MetricsStore~~ ✅ Исправлено

~~При `maxAge: 24h`: ~4.7 GB RAM — неприемлемо.~~

- `maxAge` снижен до 2 часов → ~0.4 GB RAM
- Per-allocation метрики убраны из MetricsStore (аллокации эфемерны, UUID накапливался без ограничений)
- `AllocationData` в store содержит только текущий snapshot (no time series)

### 3.4 ~~Нет истории логов~~ ✅ Исправлено

~~Все log-эндпоинты без `follow=true` возвращали заглушки.~~

Реализован `LogBuffer` — per-source кольцевой буфер (1000 строк).  
Sources: `"cluster"`, `"node.{id}"`, `"node.{id}.svc.{name}"`.  
Буфер пополняется NATS-подписками в `startLogBuffering()` (запускается при старте сервера).  
`follow=true` — сначала реплей из буфера, затем live NATS.

### 3.5 handleEvents возвращает пустой массив

```go
func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
    // TODO: implement events storage
    api.writeJSON(w, http.StatusOK, map[string]interface{}{
        "events": []interface{}{},
```

События кластера (scale up/down, restart, deploy) не накапливаются и не доступны через UI. Есть только `metricsStore.events` (scaling events) и это единственный доступный журнал.

### 3.6 Нет event-driven обновлений в streamHub

`streamHub.buildSnapshot()` — это polling: каждые 5 секунд читает всё из KV. Правильная архитектура — подписаться на изменения KV через NATS JetStream Watch и обновлять in-memory индекс. Тогда snapshot строится из памяти за O(1) без сетевых вызовов.

---

## 4. Что работает правильно

- **Архитектура SSE с hub** — правильная. Единый hub, fan-out к подписчикам — хорошо.
- **Drain events через NATS** — event-driven, работает корректно.
- **Drain subscribe** в hub — правильно: один NATS sub на всех SSE-клиентов.
- **Exponential backoff reconnect** в UI — правильно.
- **Non-blocking fan-out** (drop if full) — правильно, не блокирует hub.
- **Keepalive pings** каждые 30с — правильно для прокси с idle timeout.
- **SSE headers** — правильные.
- **Log streaming через NATS** (follow=true) — работает, event-driven.

---

## 5. План исправлений и улучшений

### Приоритет 1 — Критические баги ✅ Выполнено (2026-05-07)

#### P1.1 ✅ Убрать WriteTimeout для SSE

**Файл:** `internal/platform/asty/api.go`

```go
api.httpServer = &http.Server{
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      0, // SSE-совместимо
}
```

Выбран Вариант A (глобально). SSE больше не рвётся через 10 секунд.

#### P1.2 ✅ Убрать дублирующийся SSE-поток на странице Cluster

`cluster_metrics` event добавлен в глобальный `/api/v1/stream` с delta streaming.  
`cluster.tsx` убрал отдельный `useEffect` с `/api/v1/stream/metrics/cluster`.  
`initSSE()` в store обрабатывает `cluster_metrics` и накапливает через `appendMetrics()`.

---

### Приоритет 2 — Масштаб и производительность ✅ Выполнено (2026-05-07)

#### P2.1 ✅ Delta streaming для исторических метрик

`lastMetricsSent time.Time` в каждом SSE-хендлере. Первый connect — полная история (2ч), каждый тик — только новые точки.  
Добавлен `GetAfter(key string, after time.Time) []MetricPoint` в MetricsStore.  
Frontend: `appendMetrics()` helper в store, `MAX_CHART_POINTS = 1440`.

#### P2.2 ✅ Убрать дублирование сбора метрик

`metricsStore.StartCollection()` и `collectMetrics()` удалены.  
`streamHub.refresh()` вызывает `metricsStore.IngestSnapshot(snap)` перед fanout.  
`IngestSnapshot` выводит CPU/memory/alloc_count из hub snapshot без KV-читов.

#### P2.3 ✅ Ограничить in-memory историю

`maxAge` снижен до 2 часов. Per-allocation метрики убраны.  
`AllocationData` в Zustand store не содержит time series — только текущий snapshot.

---

### Приоритет 3 — История логов ✅ Выполнено (2026-05-07)

#### P3.1 ✅ Кольцевой буфер логов на сервере

`log_buffer.go` — `LogBuffer` с per-source ring buffer (1000 строк).  
`startLogBuffering()` в Server подписывается на `asty.v1.server.logs` и `asty.v1.agent.*.logs.*`.

#### P3.2 ✅ REST-эндпоинты возвращают историю из буфера

`follow=false` — возвращает последние N строк из LogBuffer.  
`follow=true` — реплей из буфера + live NATS-подписка.

---

### Приоритет 4 — Event-driven streamHub (масштаб 1000×1000)

#### P4.1 JetStream Watch вместо KV polling

Текущий `buildSnapshot()` делает `ListAllocations(svc.Name)` для каждого сервиса — 1000 KV-читов на тик.

Правильная модель:
1. При старте сервера: загрузить все текущие ноды и аллокации в in-memory индекс.
2. Подписаться на KV Watch для bucket nodes и alloc-bucket каждого сервиса.
3. При изменении KV-записи → обновить in-memory индекс.
4. `buildSnapshot()` → просто итерирует in-memory структуры, без KV-читов.

Это снижает нагрузку с 300 KV-читов/сек до 0 (только push-события при реальных изменениях).

```go
type allocIndex struct {
    mu            sync.RWMutex
    nodeMap       map[string]*NodeInfo
    allocsByNode  map[string][]*ServiceAllocation
    allocsBySvc   map[string][]*ServiceAllocation
    allocByID     map[string]*ServiceAllocation
}

// Пополняется через KV Watch callback
func (idx *allocIndex) onNodeUpdate(node *NodeInfo) { ... }
func (idx *allocIndex) onAllocUpdate(alloc *ServiceAllocation) { ... }
```

#### P4.2 Event-triggered hub refresh

Вместо тиккера по 5 секунд — инициировать refresh при получении события от KV Watch.

Но: burst protection нужна — 1000 изменений за 1 секунду не должны вызвать 1000 snapshot-rebuild. Решение: debounce с минимальным интервалом 500ms.

```go
func (h *streamHub) Run(ctx context.Context) {
    notify := make(chan struct{}, 1)
    // KV Watch → notify (non-blocking send, drop if pending)
    
    minInterval := time.NewTicker(500 * time.Millisecond)
    for {
        select {
        case <-notify:
            <-minInterval.C  // wait for minimum interval
            h.refresh()
        case <-minInterval.C:
            // periodic refresh если notify не приходил долго
        }
    }
}
```

---

### Приоритет 5 — История кластерных событий

Реализовать `handleEvents` — сейчас возвращает пустой массив.

```go
type ClusterEvent struct {
    Timestamp int64       `json:"ts"`
    Type      string      `json:"type"` // "scale_up", "restart", "deploy", "node_join", "node_leave"
    Service   string      `json:"service,omitempty"`
    NodeID    string      `json:"node_id,omitempty"`
    AllocID   string      `json:"alloc_id,omitempty"`
    Details   interface{} `json:"details,omitempty"`
}
```

Хранить в кольцевом буфере (последние 10 000 событий).  
Пушить через SSE в глобальный поток как event `"cluster_event"`.  
UI показывает ленту событий на главной странице.

---

## 6. Полная карта event-driven архитектуры (цель)

```
NATS KV Watch (nodes, allocs)
    ↓ (push при изменениях)
allocIndex (in-memory, thread-safe)
    ↓ (debounced, max 2/сек)
streamHub.refresh()
    ↓
clusterSnapshot (immutable)
    ↓ fan-out
┌─────────────────────────────────────────────────────────┐
│ SSE subscribers:                                        │
│                                                         │
│  /stream              → status, nodes, services,        │
│                         cluster_metrics (delta),        │
│                         cluster_events                  │
│                                                         │
│  /stream/node/:id     → allocations (delta),           │
│                         node_metrics (delta)            │
│                                                         │
│  /stream/service/:name → detail, allocations (delta),  │
│                          service_metrics (delta)        │
│                                                         │
│  /stream/allocation/:id → detail,                      │
│                           alloc_metrics (delta)        │
└─────────────────────────────────────────────────────────┘

NATS pub/sub (logs):
    asty.v1.server.logs       → LogBuffer["cluster"]
    asty.v1.agent.{id}.logs.* → LogBuffer["node.{id}"]
    
    ↓
    /logs/cluster?follow=true    → SSE fan-out из LogBuffer + live
    /logs/node/:id?follow=true   → SSE fan-out из LogBuffer + live
    /logs/allocation/:id?follow=true → SSE fan-out + live
```

---

## 7. Приоритизированный план задач

### Выполнено ✅

- [x] **P1.1** Убрать `WriteTimeout` в `api.go` → исправлено реальное время на UI
- [x] **P1.2** Убрать дублирующий SSE-поток `/stream/metrics/cluster`, перенести `cluster_metrics` в глобальный stream
- [x] **P2.1** Delta streaming для метрик (`GetAfter` + `lastMetricsSent`)
- [x] **P2.2** `metricsStore.IngestSnapshot()` — убран дублирующий KV-polling, `StartCollection` удалён
- [x] **P2.3** `maxAge` снижен до 2h, per-allocation time series убраны
- [x] **P3.1** Кольцевой `LogBuffer` — история логов (1000 строк, per-source)
- [x] **P3.2** REST-эндпоинты логов возвращают историю из буфера + live через NATS

### Производительность / масштаб (следующий этап)

- [ ] **P4.1** JetStream KV Watch → `allocIndex` (убрать KV polling в buildSnapshot)
- [ ] **P4.2** Debounced event-triggered hub refresh
- [ ] **P5** История кластерных событий (ClusterEvent ring buffer + SSE event)

---

## 8. Оценка влияния по сценарию 1000×1000

| Метрика | До (баг) | После P1–P3 ✅ | После P4 |
|---|---|---|---|
| KV-читов/сек (фон) | ~300 | ~150 (только hub) | ~0 |
| Размер SSE payload (node detail, /тик) | ~14 KB | ~0.1 KB (delta) | ~0.1 KB |
| RAM (MetricsStore) | ~5 GB | ~0.4 GB | ~0.4 GB |
| Задержка обновления UI | 10–60с* | 5с | <1с |

*WriteTimeout рвал SSE через 10с, exponential backoff растягивал reconnect до 60с

---

## 9. Что не стоит менять

- **Архитектура SSE hub** — правильная основа, snapshot остаётся для control-plane state
- **Drain через NATS pub/sub** — event-driven, уже работает правильно
- **Log streaming NATS → SSE** — механизм правильный
- **REST для мутаций** — правильно (drain, scale, deploy как POST)
- **Exponential backoff reconnect в UI** — правильно
- **Non-blocking fan-out** — правильно

---

## 10. Архитектура метрик (следующий этап)

**Дата фиксации:** 2026-05-07

### Концепция

JetStream — центральная шина метрик. Asty не хранит метрики. `MetricsStore` удаляется полностью.

Все источники публикуют в JetStream. Все потребители читают из JetStream через Asty.  
UI и Prometheus не знают друг о друге и не взаимодействуют между собой.

### Потоки данных

```
Agent (на каждой ноде):
  /proc → js.Publish("metrics.node.{id}", payload)
  /proc → js.Publish("metrics.node.{id}.service.{name}", payload)

Сервисы (напрямую через NATS):
  app code → js.Publish("metrics.node.{id}.service.{name}.{key}", payload)

Asty Server (урезанный snapshot):
  KV state → агрегаты по сервису → js.Publish("metrics.service.{name}", payload)

─────────────────────────────────────────────────────
JetStream STREAM: METRICS
  subjects:           metrics.>
  storage:            Memory
  MaxMsgsPerSubject:  1   ← не хранилище, только последнее значение
─────────────────────────────────────────────────────

Asty: одна подписка на metrics.>
  → MetricsCache: map[subject]MetricMsg  ← last value only, не time series
  → fan-out channels для SSE подписчиков

  ├── SSE /api/v1/stream      → UI        (push на каждый входящий msg)
  └── GET /metrics            → Prometheus (итерирует MetricsCache при scrape)
```

### Иерархия subjects

```
metrics.node.{nodeId}                            ← ресурсы ноды
metrics.node.{nodeId}.service.{serviceName}      ← сервис на конкретной ноде
metrics.node.{nodeId}.service.{serviceName}.{key} ← кастомные метрики сервиса
metrics.service.{serviceName}                    ← агрегат по всем нодам
```

**Wildcard запросы:**
- Всё по ноде N: `metrics.node.N.>`
- Сервис S на всех нодах: `metrics.node.*.service.S`
- Только ноды (без сервисов): `metrics.node.*`
- Все агрегаты: `metrics.service.>`

### Формат сообщений

Поля зависят от уровня subject:

```json
// metrics.node.{id}
{"ts": 1746617400, "node": "n1", "cpu_percent": 42.3, "memory_mb": 4096, "memory_percent": 65.0, "rps": 8.5}

// metrics.node.{id}.service.{name}
{"ts": 1746617400, "node": "n1", "service": "gateway", "cpu_percent": 12.1, "memory_mb": 128, "status": "running"}

// metrics.service.{name}  (агрегат, сервер)
{"ts": 1746617400, "service": "gateway", "avg_cpu_percent": 11.8, "avg_memory_mb": 124, "copies_running": 3}

// metrics.node.{id}.service.{name}.{key}  (кастомные, любые поля)
{"ts": 1746617400, "node": "n1", "service": "myapp", "value": 88, "queue_depth": 88}
```

### Prometheus `/metrics`

Asty при каждом scrape итерирует `MetricsCache` и генерирует gauge-метрики.  
Имена и labels выводятся из subject иерархии автоматически:

```
asty_node_cpu_percent{node="n1", dc="dc1"}              42.3
asty_node_memory_mb{node="n1", dc="dc1"}                4096
asty_node_memory_percent{node="n1", dc="dc1"}           65.0
asty_node_rps{node="n1", dc="dc1"}                      8.5

asty_service_node_cpu_percent{node="n1", service="gw"}  12.1
asty_service_node_memory_mb{node="n1", service="gw"}    128

asty_service_avg_cpu_percent{service="gw"}              11.8
asty_service_avg_memory_mb{service="gw"}                124
asty_service_copies_running{service="gw"}               3

# Кастомные метрики сервисов — автоматически
asty_service_node_queue_depth{node="n1", service="myapp"} 88
```

### Роль snapshot после рефакторинга

Snapshot остаётся, но только для **control-plane state** (не метрики):

| Остаётся в snapshot | Уходит из snapshot |
|---|---|
| Список нод и статусы | Сбор CPU/memory из KV → MetricsStore |
| Список аллокаций (topology) | IngestSnapshot |
| Список сервисов (определения) | subscribeGatewayMetrics (агент публикует сам) |
| Cluster status (leader, healthy) | Все метрики в SSE handlers |
| Агрегаты сервисов → JetStream | |

### Что удаляется

- `MetricsStore` — полностью (ring buffer, `GetAfter`, `Add`, `AddEvent`)
- `IngestSnapshot` — заменяется на `PublishServiceAggregates` (только агрегаты в JetStream)
- `subscribeGatewayMetrics` в server — агент публикует RPS напрямую
- Delta streaming в SSE handlers — не нужен (JetStream push)
- `collector.go` на сервере — сбор метрик переходит к агенту

### Что добавляется

**Backend:**
- `MetricsCache` — `map[subject]MetricMsg` + fan-out channels (≤ 1 KB RAM на 1000 метрик)
- JetStream stream `METRICS` — создаётся при старте Asty если не существует
- `PublishMetrics(snap)` в агенте — публикует node + node.service метрики в JetStream
- `PublishServiceAggregates(snap)` на сервере — публикует metrics.service.{name}
- `/metrics` Prometheus endpoint — генерирует gauges из MetricsCache
- SSE fan-out из MetricsCache subscription

**Frontend:**
- `appendMetrics` в store остаётся — браузер накапливает из SSE пока вкладка открыта
- Убрать `MAX_CHART_POINTS` жёсткое ограничение (или увеличить — браузер ограничен RAM, не сервером)

### Порядок реализации

1. Создать JetStream stream `METRICS` при старте (server + agent)
2. `MetricsCache` — подписка `metrics.>`, map + fan-out
3. Агент: `PublishMetrics` — читает /proc, публикует node + node.service в JetStream
4. Сервер: `PublishServiceAggregates` в hub — агрегаты из KV snapshot → JetStream
5. SSE handlers — убрать MetricsStore чтение, подписаться на MetricsCache fan-out
6. `/metrics` — полный Prometheus endpoint из MetricsCache
7. Удалить MetricsStore, IngestSnapshot, subscribeGatewayMetrics, collector на сервере
8. UI — убрать жёсткое ограничение точек, накопление остаётся

---

## 11. Health checks: текущее состояние и event-driven архитектура

**Дата фиксации:** 2026-05-07

### Текущее состояние (не работает сквозно)

`health.go` — агент HTTP-опрашивает локальные процессы периодически.  
Результат хранится in-memory на агенте. Никуда не публикуется.

```go
// agent.go:183 — закомментировано, TODO про динамический порт
// a.healthChecker.Register(svc.Name, addr, svc.Health.Path, ...)
```

`ServiceAllocation.HealthStatus` никогда не обновляется из реальных проверок.  
`publishProcessMetrics` пишет CPU/memory в KV — но не health status.  
Поле `health_status` в UI всегда `""` или `"unknown"`.

### Три независимых сигнала здоровья

Здоровье сервиса — это не один вопрос, а три разных:

| Вопрос | Сигнал | Polling? |
|---|---|---|
| Процесс жив? | OS: `cmd.Wait()` уже реализован в agent | **Нет** — event при падении |
| HTTP /health отвечает? | Агент опрашивает локально | Да, но только агент |
| Агент жив? | JetStream KV TTL — heartbeat | **Нет** — KV Watch на expiry |

### Event-driven подход

**Сигнал 1 — падение процесса (уже event-driven):**

Агент уже ловит выход процесса через `cmd.Wait()` в `monitorProcesses`.  
Единственное изменение: при падении сразу публиковать в JetStream:

```
metrics.node.{id}.service.{name}  →  {healthy: false, status: "failed", ts: ...}
```

Сервер узнаёт о падении через MetricsCache fan-out мгновенно — без polling.

**Сигнал 2 — HTTP health probe (polling, но только локально):**

Агент продолжает опрашивать локальный HTTP endpoint сервиса.  
Результат идёт в то же metrics-сообщение что и CPU/memory:

```
metrics.node.{id}.service.{name}  →  {cpu_percent, memory_mb, healthy: true/false, ts: ...}
```

Сервер не участвует в polling — только получает готовый результат из JetStream.

**Сигнал 3 — liveness агента (JetStream KV TTL, event-driven):**

```
KV bucket: asty-agents
  key:   agent.{nodeId}
  value: {node_id, ip, ts}
  TTL:   30s
```

Агент обновляет ключ каждые 10с.  
Если агент умер → ключ истекает через 30с → KV Watch на сервере получает `KeyValueDelete` событие → нода помечается `down` немедленно.

Это заменяет текущую логику `NodeInfo.LastSeen` (hub читает KV каждые 5с и проверяет время).

### Итоговый event-driven поток

```
Процесс падает
  → cmd.Wait() в agent
  → js.Publish("metrics.node.{id}.service.{name}", {healthy: false})
  → MetricsCache fan-out → UI + Prometheus

HTTP probe unhealthy
  → healthChecker.performCheck() локально
  → следующий publishMetrics тик включает healthy: false
  → JetStream → MetricsCache fan-out → UI + Prometheus

Агент умирает
  → KV key "agent.{nodeId}" истекает (TTL 30s)
  → cs.WatchNodes() на сервере получает Delete event
  → нода → status: "down"
  → SSE state event → UI
```

Сервер не делает ни одного периодического health-запроса.  
Все изменения здоровья — push-события.

### Что меняется в коде

**Agent:**
- Раскомментировать и доделать `healthChecker.Register` (решить вопрос с портом через env `ASTY_HEALTH_ADDR`)
- `publishProcessMetrics` → `publishMetrics`: добавить `healthy` поле из `healthChecker.IsHealthy()`
- При падении процесса (`cmd.Wait()` → exit) — немедленно publish `{healthy: false, status: "failed"}`
- Добавить heartbeat loop: `js.KeyValue("asty-agents").Put("agent.{id}", ...)` каждые 10с

**Server:**
- Убрать проверку `LastSeen` в hub snapshot (заменена KV TTL)
- `WatchNodes` уже реализован в `state.go` — подписаться на Delete события для `agent.*` keys
- Нода переходит в `down` по KV Watch событию, а не по таймеру в hub

**Health check registration (доделать):**

Текущая проблема — агент не знает порт процесса. Решения:
- Сервис принимает порт через env `ASTY_HEALTH_PORT` (задаётся в `.asty` файле)
- Или: фиксированный порт в `.asty` конфиге (`health.addr: ":8080"`)
- Или: сервис сам публикует свой health endpoint через NATS при старте
