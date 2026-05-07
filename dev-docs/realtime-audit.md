# Аудит реального времени: UI ↔ Backend

**Дата:** 2026-05-07  
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

### 2.1 SSE-потоки (текущие)

| Endpoint | Кто открывает | Частота событий | Что несёт |
|---|---|---|---|
| `GET /api/v1/stream` | `App.tsx` → `initSSE()` | каждые 5с (hub tick) | status, nodes, services (с runtime-метриками) |
| `GET /api/v1/stream/node/:id` | `subscribeNode()` | каждые 5с | allocations этой ноды + полная история метрик (1 час) |
| `GET /api/v1/stream/service/:name` | `subscribeService()` | каждые 5с | detail + allocations + полная история метрик (1 час) |
| `GET /api/v1/stream/allocation/:id` | `subscribeAllocation()` | каждые 5с | detail + полная история метрик (1 час) |
| `GET /api/v1/stream/metrics/cluster` | `cluster.tsx` useEffect | каждые 5с | полная история метрик кластера (1 час) |
| `GET /api/v1/logs/cluster?follow=true` | `cluster.tsx` useEffect | push (NATS sub) | live логи сервера |
| `GET /api/v1/logs/node/:id?follow=true` | `node-detail.tsx` | push (NATS sub) | live логи агента |
| `GET /api/v1/logs/allocation/:id?follow=true` | `service-detail.tsx` | push (NATS sub) | live логи процесса |

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

### 2.3 Проблема: Cluster страница открывает 3 SSE-соединения

```
App.tsx:         /api/v1/stream            ← nodes, services, status (уже есть)
cluster.tsx:     /api/v1/stream/metrics/cluster  ← дублирует, отдельный поток
cluster.tsx:     /api/v1/logs/cluster?follow=true ← OK, этого нет в глобальном потоке
```

Метрики кластера можно включить в глобальный `/api/v1/stream` как отдельный event-тип.

---

## 3. Архитектурные проблемы

### 3.1 Два независимых цикла сбора данных

**Проблема:** `metricsStore.StartCollection()` и `streamHub.refresh()` оба читают NATS KV независимо.

```
streamHub.refresh()           → каждые 5с → ListNodes() + ListAllocations() × сервисы
metricsStore.collectMetrics() → каждые 10с → ListNodes() + ListAllocations() × сервисы
```

При 1000 сервисах:
- streamHub: 1000 KV-читов каждые 5с = **200 читов/сек**
- metricsStore: 1000 KV-читов каждые 10с = **100 читов/сек**
- Итого: **~300 KV-читов/сек** только на фоновые петли, без пользовательских запросов

streamHub уже строит полный snapshot со всеми allocations. metricsStore должен получать данные из этого snapshot, а не ходить в KV сам.

### 3.2 SSE-потоки шлют всю историю метрик на каждый тик

**Файл:** `api.go`, хендлеры `handleStreamNode`, `handleStreamService`, `handleStreamAllocation`

```go
since := time.Now().Add(-1 * time.Hour)
sseEvent(w, "metrics", mustJSON(map[string]interface{}{
    "cpu": ms.Get("node."+nodeID+".cpu", since), // 360 точек каждые 5с
```

На каждый тик клиент получает 360 точек (час при интервале 10с). При нескольких открытых вкладках:
- 1 нода-detail = 2 массива × 360 точек × каждые 5с  
- 5 вкладок = 10 массивов × 360 точек = **~14 KB JSON каждые 5 секунд**

Нужна delta-модель: отсылать только новые точки с момента последнего события.

### 3.3 Масштаб in-memory MetricsStore

При текущих настройках (`maxAge: 24h`, интервал 10с):

```
Ключи метрик:
  Ноды:           2 ключа × 1000 нод = 2 000
  Сервисы:        3 ключа × 1000 сервисов = 3 000
  Аллокации:      2 ключа × 1000 сервисов × avg 3 аллок = 6 000
  Итого:          ~11 000 ключей

Точек на ключ:    24h × 3600/10 = 8 640

Размер одной точки: ~50 байт (JSON)
Итого RAM:        11 000 × 8 640 × 50 = ~4.7 GB
```

Это неприемлемо для продакшена. Хранить 24h истории in-memory нельзя.

Решение:
- in-memory: последние 1-2 часа (720-1440 точек)
- долгосрочная история: NATS JetStream с TTL или внешнее хранилище

Также: per-allocation метрики не нужны исторически — аллокации эфемерны. Достаточно текущего значения в snapshot.

### 3.4 Нет истории логов

Все log-эндпоинты без `follow=true` возвращают заглушки:

```go
// handleLogsCluster, follow=false:
"logs": []string{
    "[...] Cluster log stream available via SSE (follow=true)",
    "[asty] Use follow=true for real-time cluster events",
}
```

Реальная история отсутствует. NATS pub/sub — fire-and-forget, без буферизации.

**Нужно:** кольцевой буфер на сервере (последние N строк) + JetStream для долгосрочного хранения.

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

### Приоритет 1 — Критические баги (делать сейчас)

#### P1.1 Убрать WriteTimeout для SSE

**Файл:** `internal/platform/asty/api.go`

```go
// Вариант A: убрать глобально (проще, SSE — основной режим)
api.httpServer = &http.Server{
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      0, // SSE-совместимо
}

// Вариант B: per-connection (элегантнее, REST-эндпоинты сохраняют timeout)
// В начале каждого SSE-хендлера:
rc := http.NewResponseController(w)
rc.SetWriteDeadline(time.Time{}) // отключить deadline для этого соединения
```

Рекомендую **Вариант A** — проще, нет риска забыть в новых хендлерах.

#### P1.2 Убрать дублирующийся SSE-поток на странице Cluster

**Файл:** `ui/src/pages/cluster.tsx`

Сейчас открывает `/api/v1/stream/metrics/cluster` отдельно. Решение: добавить `cluster_metrics` event в глобальный `/api/v1/stream` и читать оттуда. Убрать отдельный `useEffect` с EventSource в cluster.tsx.

**Backend:** в `handleStream` добавить `cluster_metrics` event с историей метрик кластера.  
**Frontend:** в `initSSE()` добавить обработчик `cluster_metrics`, хранить в store.

---

### Приоритет 2 — Масштаб и производительность

#### P2.1 Delta streaming для исторических метрик

Вместо "посылать всю историю на каждый тик" — посылать только дельту.

**Backend:** в каждом SSE-stream-хендлере (node/service/allocation/cluster) хранить `lastSentTimestamp`. На каждый тик отсылать только точки `> lastSentTimestamp`.

```go
// Вместо:
since := time.Now().Add(-1 * time.Hour)
ms.Get(key, since)  // 360 точек

// Стать:
ms.GetAfter(key, lastSentTimestamp)  // только новые точки
```

При первом соединении — посылать историю целиком (initial burst), потом только дельты.

**Frontend:** в store вместо `cpuMetrics = data.cpu` делать append:
```ts
cpuMetrics: [...existing.cpuMetrics, ...data.cpu].slice(-MAX_POINTS)
```

#### P2.2 Убрать дублирование сбора метрик

Убрать `metricsStore.StartCollection()`. Кормить MetricsStore из streamHub-снимков:

```go
func (h *streamHub) refresh() {
    snap := h.buildSnapshot()
    h.server.metricsStore.IngestSnapshot(snap)  // новый метод
    h.mu.Lock()
    h.snapshot = snap
    h.mu.Unlock()
    h.fanout(snap)
}
```

`IngestSnapshot` извлекает CPU/memory/alloc_count из snap и записывает в store без лишних KV-читов.

#### P2.3 Ограничить in-memory историю

Уменьшить `maxAge` до 2 часов:
```go
s.metricsStore = NewMetricsStore(2 * time.Hour)
```

Убрать per-allocation метрики из MetricsStore (они эфемерны, достаточно текущего значения в snapshot).

---

### Приоритет 3 — История логов

#### P3.1 Кольцевой буфер логов на сервере

Добавить `LogBuffer` — per-source ring buffer последних N строк:

```go
type LogBuffer struct {
    mu    sync.RWMutex
    lines map[string][]LogLine  // source → ring buffer
    maxN  int                   // default: 1000
}

type LogLine struct {
    Timestamp int64  `json:"ts"`
    Level     string `json:"level"`
    Message   string `json:"message"`
    Fields    map[string]interface{} `json:"fields,omitempty"`
}
```

Sources: `"cluster"`, `"node.{id}"`, `"alloc.{service}.{id}"`

Буфер пополняется NATS-подписчиком (тем же, что сейчас используется для SSE).

#### P3.2 REST-эндпоинты возвращают историю из буфера

```
GET /api/v1/logs/cluster?lines=100    → последние 100 строк из LogBuffer["cluster"]
GET /api/v1/logs/node/:id?lines=100   → из LogBuffer["node.{id}"]
```

SSE `follow=true` продолжает работать через NATS-подписку.

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

### Сейчас (делать немедленно)

- [ ] **P1.1** Убрать `WriteTimeout` в `api.go` → это исправит реальное время на UI
- [ ] **P1.2** Убрать дублирующий SSE-поток `/stream/metrics/cluster` со страницы Cluster, перенести в глобальный stream

### Следующий спринт

- [ ] **P2.1** Delta streaming для метрик (не полная история на каждый тик)
- [ ] **P2.2** `metricsStore.IngestSnapshot()` — убрать дублирующий KV-polling
- [ ] **P2.3** Уменьшить `maxAge` до 2h, убрать per-allocation time series
- [ ] **P3.1** Кольцевой `LogBuffer` — история логов
- [ ] **P3.2** REST-эндпоинты логов возвращают историю из буфера

### Производительность / масштаб

- [ ] **P4.1** JetStream KV Watch → `allocIndex` (убрать polling в buildSnapshot)
- [ ] **P4.2** Debounced event-triggered hub refresh
- [ ] **P5** История кластерных событий (ClusterEvent ring buffer + SSE event)

---

## 8. Оценка влияния по сценарию 1000×1000

| Метрика | Сейчас | После P1 | После P2 | После P4 |
|---|---|---|---|---|
| KV-читов/сек (фон) | ~300 | ~300 | ~150 | ~0 |
| Размер SSE payload (node detail, /тик) | ~14 KB | ~14 KB | ~0.1 KB | ~0.1 KB |
| RAM (MetricsStore) | ~5 GB | ~5 GB | ~0.4 GB | ~0.4 GB |
| Задержка обновления UI | 10–60с* | 5с | 5с | <1с |

*из-за бага WriteTimeout — соединение рвётся и reconnect с backoff

---

## 9. Что не стоит менять

- **Architektura SSE hub** — правильная основа, не менять
- **Drain через NATS pub/sub** — event-driven, уже работает правильно
- **Log streaming NATS → SSE** — механизм правильный, нужно только добавить буфер истории
- **REST для мутаций** — правильно (drain, scale, deploy как POST)
- **Exponential backoff reconnect в UI** — правильно
- **Non-blocking fan-out** — правильно
