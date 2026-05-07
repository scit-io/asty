# Метрики: архитектура и план задач

**Последнее обновление:** 2026-05-07  
**Статусы:** `[ ]` не начато · `[~]` в процессе · `[+]` готово

---

## Текущая архитектура (as-is)

### Как данные движутся сейчас

```
Agent                          NATS                          Server
──────                         ────                          ──────
MetricsCollector               asty.v1.metrics.gateway.*     subscribeGatewayMetrics()
  └─ reads /proc (CPU/mem)                                    └─ MetricsStore.Add(node.{id}.rps)
  └─ publishProcessMetrics()
       └─ MutateAllocation()   node.{id} KV update           streamHub.refresh()
            sets CPUUsage,                                     └─ IngestSnapshot()
            MemoryUsage,                                            └─ MetricsStore.Add(
            HealthStatus                                                 node.{id}.cpu/memory
                                                                         cluster.cpu/memory/rps
                                                                         service.{name}.cpu/mem)

                                                              SSE handlers
                                                              └─ MetricsStore.Get/GetAfter()
                                                                   └─ cluster_metrics event
                                                                   └─ node metrics event
                                                                   └─ service metrics event

                                                              Autoscaler
                                                              └─ MetricsStore.Get(node.{id}.rps)
                                                                   └─ hasGatewayTraffic()
```

### Проблемы текущей архитектуры

1. **MetricsStore живёт в памяти лидера.** При смене лидера история (2 ч) теряется целиком.
2. **Метрики записываются в KV (MutateAllocation).** Каждые EvalInterval на каждый аллок — лишняя нагрузка на JetStream KV, дополнительные тики в watchAllocsToQueue.
3. **IngestSnapshot** — метрики ноды вычисляются косвенно из `CPUTotal − CPUAvailable`, а не из реальных показаний процесса.
4. **Delta streaming (GetAfter)** — сложный механизм с `lastMetricsSent`; ломается при переподключении SSE клиента.
5. **Порядок нод в UI нестабилен.** `snapshot()` итерирует `map[string]*NodeInfo` — порядок случаен при каждом обновлении. Визуально ноды «прыгают».
6. **Prometheus `/metrics` — заглушка.** Возвращает только counts нод/сервисов; реальные метрики ресурсов отсутствуют.
7. **Нет метрик по самому оркестратору.** Нет данных о состоянии workqueue, лидерства, ошибках reconcile, решениях автоскейлера, состоянии NATS-соединения и JetStream stream-ов, состоянии кластера как целого (allocs по статусам, ёмкость).

---

## Целевая архитектура (to-be)

### Принципы

- **JetStream как шина метрик.** Агент публикует в JetStream, сервер подписывается. MaxMsgsPerSubject=1 — хранится только последнее значение; история не нужна (SSE клиент сам строит историю в браузере).
- **MetricsCache на сервере.** Замена MetricsStore: `map[subject → последнее значение]` + fan-out каналы для SSE. Не теряется при смене лидера — пересоздаётся из JetStream за несколько секунд.
- **Агент не пишет метрики в KV.** CPUUsage/MemoryUsage/HealthStatus убираются из ServiceAllocation. Актуальные значения берутся из MetricsCache при построении snapshot.
- **Стабильный порядок нод.** Ноды сортируются по `CreatedAt` (время первой регистрации в кластере) на сервере при построении snapshot.
- **Три группы системных метрик.** `metrics.asty.*` — состояние оркестратора (workqueue, reconcile, autoscaler). `metrics.nats.*` — NATS-соединение и JetStream (RTT, throughput, stream stats). `metrics.cluster.*` — операционное состояние кластера (allocs по статусам, ёмкость ресурсов). Публикуются сервером раз в EvalInterval, доступны в Prometheus и SSE.

### Потоки данных (to-be)

```
Agent (каждая нода)            JetStream METRICS             Server — MetricsCache
───────────────────            ─────────────────             ─────────────────────
MetricsCollector                                             map[subject]MetricMsg
  └─ reads /proc                metrics.node.{id}.cpu  ───▶  └─ обновляется при каждом
  └─ publishMetrics()           metrics.node.{id}.mem  ───▶       входящем сообщении
       └─ js.Publish()          metrics.node.{id}.rps  ───▶       из JetStream
                                metrics.node.{id}.
HealthChecker                    service.{name}.cpu    ───▶
  └─ publishMetrics()            service.{name}.mem    ───▶
                                 service.{name}.health ───▶

Gateway                        asty.v1.metrics.gateway.*
  └─ valid_rps          ───▶   └─ subscribeGatewayMetrics()
                                    └─ js.Publish(metrics.node.{id}.rps)

Server (каждая нода)
  └─ publishNATSMetrics()       metrics.nats.{id}.*    ───▶
       └─ nc.Stats(), nc.RTT()
       └─ js.StreamInfo()

  └─ publishAstyMetrics()       metrics.asty.{id}.*    ───▶
       └─ workqueue, counters
       └─ is_leader

Server (только лидер)
  └─ publishClusterMetrics()    metrics.cluster.*      ───▶
       └─ ClusterState snapshot


MetricsCache → при каждом snapshot-тике → SSE /api/v1/stream
                                           ├─ cluster_metrics    (node cpu/mem/rps агрегаты)
                                           ├─ asty_metrics       (workqueue, dispatch, autoscaler)
                                           ├─ nats_metrics       (rtt, throughput, js stream stats)
                                           ├─ cluster_state_metrics (nodes/allocs/services/capacity)
                                           ├─ nodes              (snapshot)
                                           └─ services           (snapshot)

MetricsCache → Autoscaler
               └─ GetLatest(metrics.node.{id}.rps) → hasGatewayTraffic()

MetricsCache → buildSnapshot()
               └─ merge cpu/mem/rps → NodeInfo; sort by CreatedAt

MetricsCache → Prometheus /metrics
               └─ все группы: node / asty / nats / cluster
```

---

## Задачи

---

### 1. Стабильный порядок нод в UI

**Файлы:** `state.go`, `streamhub.go`, `ui/src/store/cluster.ts`

- [ ] Убедиться что `NodeInfo.CreatedAt` заполняется при первом `Put` ноды в KV и не перезаписывается при обновлениях heartbeat (сейчас `CreatedAt` есть в структуре, нужно проверить, что `PutNode` не затирает его при update).
- [ ] В `buildSnapshot()` (streamhub.go) сортировать `nodes` по `CreatedAt` (возрастание) перед установкой в `clusterSnapshot.Nodes`. Порядок фиксируется раз и не меняется пока нода в кластере.
- [ ] В UI store (`cluster.ts`) при получении события `nodes` сохранять порядок как пришёл с сервера (не пересортировывать на клиенте). Убрать любую клиентскую сортировку если она есть.

---

### 2. JetStream stream METRICS

**Файлы:** `server.go`, `agent.go`

- [ ] При старте сервера создавать JetStream stream `METRICS`:
  - subjects: `metrics.>`
  - хранение: `MaxMsgsPerSubject=1` (только последнее значение на каждый subject)
  - MaxAge: 5 минут (TTL, чтобы мёртвые ноды не остаивались)
  - Retention: Limits
- [ ] При старте агента подключаться к JetStream (проверить что stream существует перед публикацией; если нет — retry с backoff).
- [ ] Формат сообщения MetricMsg: `{ node_id, service, timestamp_ms, value float64 }` — единый для всех subjects.

---

### 3. Публикация метрик агентом

**Файлы:** `agent.go`, `collector.go`

- [ ] Создать `publishMetrics(ctx)` в агенте — заменяет `publishProcessMetrics()`. Запускается раз в EvalInterval.

**CPU и RAM ноды:**
- [ ] `metrics.node.{id}.cpu_percent` — суммарный CPU% всех процессов на ноде относительно `CPUTotal`. Источник: `MetricsCollector.GetAllMetrics()`.
- [ ] `metrics.node.{id}.memory_used_mb` — суммарный RSS всех процессов (MB). Источник: `MetricsCollector`.
- [ ] `metrics.node.{id}.memory_total_mb` — общий RAM ноды. Источник: `NodeInfo.MemoryTotal` (уже есть в KV).

**Диск ноды (новое):**
- [ ] `metrics.node.{id}.disk_used_mb` — занято на разделе, где находится `workDir`. Источник: `syscall.Statfs(workDir)` → `(Blocks - Bfree) * Bsize`.
- [ ] `metrics.node.{id}.disk_total_mb` — полный объём раздела. Источник: `syscall.Statfs` → `Blocks * Bsize`.
- [ ] `metrics.node.{id}.disk_available_mb` — доступно (с учётом reserved блоков). Источник: `syscall.Statfs` → `Bavail * Bsize`.
- [ ] `metrics.node.{id}.disk_type` — тип диска: `1.0` = SSD, `0.0` = HDD. Определять по `/sys/block/{dev}/queue/rotational` (только Linux; на других платформах публиковать `1.0` как default). Нужно определить устройство по `mountpoint` из `Statfs`.

**Метрики сервисов:**
- [ ] `metrics.node.{id}.service.{name}.cpu_percent` — CPU% процесса сервиса
- [ ] `metrics.node.{id}.service.{name}.memory_mb` — RSS процесса (MB)
- [ ] `metrics.node.{id}.service.{name}.health` — `1.0` healthy, `0.0` unhealthy

**Зачистка KV:**
- [ ] **Убрать** запись `CPUUsage`, `MemoryUsage`, `HealthStatus` в `ServiceAllocation` через `MutateAllocation`.
- [ ] Убрать поля `CPUUsage`, `MemoryUsage`, `HealthStatus` из `ServiceAllocation` (state.go).

---

### 4. MetricsCache на сервере

**Файлы:** `server.go` (новый тип), `streamhub.go`

- [ ] Создать тип `MetricsCache`:
  - Внутри: `map[subject]MetricMsg` + `sync.RWMutex`
  - Метод `Put(subject string, msg MetricMsg)` — обновляет последнее значение и рассылает в fan-out
  - Метод `GetLatest(subject string) (MetricMsg, bool)` — последнее значение
  - Метод `Subscribe() (<-chan MetricUpdate, func())` — подписка на любое изменение (для SSE fan-out)
- [ ] В `Server.Start()` заменить `metricsStore *MetricsStore` на `metricsCache *MetricsCache`.
- [ ] Подписаться на JetStream `metrics.>` при старте сервера: каждое входящее сообщение → `metricsCache.Put(subject, msg)`.
- [ ] Сохранить `subscribeGatewayMetrics()` но переделать: вместо `metricsStore.Add(...)` → `js.Publish("metrics.node.{id}.rps", ...)`. Так RPS от Gateway попадает в JetStream и затем в MetricsCache через общую подписку.

---

### 5. buildSnapshot — слияние с MetricsCache

**Файлы:** `streamhub.go`

- [ ] В `buildSnapshot()` для каждой ноды читать из MetricsCache:
  - `metrics.node.{id}.cpu` → поле в NodeInfo (новое: `CPUPercent float64`)
  - `metrics.node.{id}.memory` → `MemoryUsedMB int64`
  - `metrics.node.{id}.rps` → `RPS float64`
- [ ] Убрать `IngestSnapshot()` вызов из `refresh()`. MetricsStore больше не используется.
- [ ] Добавить в `NodeInfo` вычисляемые поля для UI (не хранятся в KV, только в snapshot): `CPUPercent`, `MemoryUsedMB`, `RPS`.
- [ ] Кластерные агрегаты (cluster cpu/mem/rps) вычислять в `buildSnapshot()` суммированием из MetricsCache, а не в IngestSnapshot.
- [ ] Для `ServiceWithUsage`: `AvgCPUPercent`, `AvgMemoryMB` вычислять из MetricsCache по всем аллокам сервиса.

---

### 6. SSE handlers — переключение на MetricsCache + новые события

**Файлы:** `api.go`

#### 6a. Существующие события — переключение с MetricsStore на MetricsCache

- [ ] Глобальный `/api/v1/stream`: убрать `lastMetricsSent` и delta-логику (GetAfter). Метрики встраиваются в snapshot — `cluster_metrics` идёт с каждым тиком как актуальное значение.
- [ ] `/api/v1/stream/node/{id}`: убрать delta `lastMetricsSent`. `metrics` event — последнее значение из MetricsCache для этой ноды.
- [ ] `/api/v1/stream/service/{name}`: аналогично.
- [ ] **Убрать ссылки** на `metricsStore` из всех SSE handlers.

#### 6b. Новые SSE события — metrics.asty, metrics.nats, metrics.cluster

MetricsCache получает все subjects `metrics.>`, в том числе новые группы. SSE должен их доставлять клиенту.

**Новые SSE events в глобальном `/api/v1/stream`:**

- [ ] `asty_metrics` — ресурсы и состояние оркестратора на каждой ноде.
  Структура: `{ nodes: { [node_id]: { process: { cpu_percent, memory_mb, disk_mb }, is_leader, workqueue_depth, workqueue_delayed, reconcile_errors, placements, dispatch_sent, dispatch_failed, scale_up, scale_down, prune_failed } } }`.
  Источник: MetricsCache, subjects `metrics.asty.*`.

- [ ] `nats_metrics` — ресурсы и состояние NATS-сервера на каждой ноде.
  Структура: `{ nodes: { [node_id]: { server: { cpu_percent, memory_mb, storage_used_mb, storage_total_mb }, connected, rtt_ms, reconnects, in_msgs, out_msgs, in_bytes, out_bytes, js_stream_messages, js_stream_bytes, js_kv_messages } } }`.
  Источник: MetricsCache, subjects `metrics.nats.*`.

- [ ] `cluster_state_metrics` — операционное состояние и ёмкость кластера.
  Структура: `{ health_percent, nodes: { total, healthy, draining, down }, allocs: { running, pending, starting, failed, stopped }, services: { total, healthy, degraded }, capacity: { cpu_total_mhz, cpu_used_mhz, memory_total_mb, memory_used_mb, disk_total_mb, disk_used_mb, disk_available_mb } }`.
  Источник: MetricsCache, subjects `metrics.cluster.*`.

**Механизм доставки:**
- [ ] Все три новых события отправляются **при каждом тике snapshot** — MetricsCache всегда хранит последнее значение, поэтому читать при каждом `emit()` безопасно и дёшево.
- [ ] Отдельная подписка на `MetricsCache.Subscribe()` не нужна — достаточно читать из cache при уже существующем snapshot-тике.
- [ ] Если данных в MetricsCache нет (сервер только запустился) — не отправлять событие, а не отправлять пустой объект.

---

### 7. Автоскейлер — переключение на MetricsCache

**Файлы:** `autoscaler.go`

- [ ] `hasGatewayTraffic()`: вместо `metricsStore.Get(node.{id}.rps, -60s)` → `metricsCache.GetLatest("metrics.node.{id}.rps")`. Проверять что timestamp не старше 60 секунд (TTL живого значения).
- [ ] `evaluateResourcePressure()` (CPU/memory scale-up): читать из MetricsCache вместо KV-полей `CPUAvailable/MemoryAvailable` из NodeInfo. MetricsCache даёт реальные показания процессов, а не расчётный остаток.
- [ ] Убрать зависимость `Autoscaler` от `*MetricsStore`, заменить на `*MetricsCache`.

---

### 8. metrics.asty — метрики оркестратора

**Файлы:** `server.go`, `controller.go`, `autoscaler.go`

Публикуются каждым сервером (не только лидером) раз в EvalInterval в JetStream `metrics.asty.{node_id}.*`.

**Потребление ресурсов процессом Asty (ВАЖНО):**
- [ ] `metrics.asty.{id}.process.cpu_percent` — CPU% процесса `asty server` (самого оркестратора). Источник: `/proc/self/stat` (Linux) / `getrusage` (Darwin). Реализовать как отдельный вызов в `collector.go` для PID = `os.Getpid()`.
- [ ] `metrics.asty.{id}.process.memory_mb` — RSS процесса `asty server` в MB. Источник: `/proc/self/status` → `VmRSS` (Linux) / `task_info` (Darwin). Тот же механизм что и для управляемых процессов, но для себя.
- [ ] `metrics.asty.{id}.process.disk_mb` — занято на диске рабочей директорией Asty (`workDir` + бинарники + логи). Источник: рекурсивный `du` или `filepath.Walk` с суммированием размеров. Запускать не чаще раза в минуту (дорогая операция).

**Состояние оркестратора:**
- [ ] `metrics.asty.{id}.is_leader` — `1.0` лидер, `0.0` нет. Источник: `leaderElection.IsLeader()`.
- [ ] `metrics.asty.{id}.workqueue.depth` — глубина очереди (ожидание + обработка). Источник: `workqueue.Len()`.
- [ ] `metrics.asty.{id}.workqueue.delayed` — ключи в backoff. Источник: новый метод `Workqueue.DelayedLen()`.
- [ ] `metrics.asty.{id}.reconcile.errors` — ошибки reconcile с момента старта лидерства (счётчик). Источник: счётчик в `ServiceController`.
- [ ] `metrics.asty.{id}.schedule.placements` — созданных аллокаций (счётчик). Источник: `Scheduler.createAllocation()`.
- [ ] `metrics.asty.{id}.dispatch.sent` — отправленных start-команд (счётчик).
- [ ] `metrics.asty.{id}.dispatch.failed` — упавших start-команд (счётчик).
- [ ] `metrics.asty.{id}.autoscaler.scale_up` — решений scale_up (счётчик).
- [ ] `metrics.asty.{id}.autoscaler.scale_down` — решений scale_down (счётчик).
- [ ] `metrics.asty.{id}.prune.failed_allocs` — удалённых permanently-failed аллокаций (счётчик).

**Реализация:**
- [ ] Добавить метод в `collector.go` для сбора метрик произвольного PID, включая `os.Getpid()`.
- [ ] Добавить атомарные счётчики в `ServiceController`.
- [ ] Создать `publishAstyMetrics(ctx)` в `Server` — горутина раз в EvalInterval. `process.*` и `is_leader` публикуются всегда; остальное только в `leaderCtx`.

---

### 9. metrics.nats — метрики NATS-сервера и соединения

**Файлы:** `server.go`, `agent.go`

Публикуются каждым узлом раз в EvalInterval. Два источника:
1. **`nc.Stats()`** — клиентская статистика соединения (доступна везде).
2. **NATS HTTP monitoring** (`http://127.0.0.1:8222/varz`, `/jsz`) — статистика самого nats-server процесса (потребление ресурсов). Порт мониторинга должен быть настроен в NATS config; при недоступности — пропускать тик и логировать debug.

**Потребление ресурсов процессом NATS (ВАЖНО):**
- [ ] `metrics.nats.{id}.server.cpu_percent` — CPU% процесса nats-server. Источник: `GET /varz` → поле `cpu` (float, 0–100).
- [ ] `metrics.nats.{id}.server.memory_mb` — RAM процесса nats-server (MB). Источник: `GET /varz` → `mem` (bytes) / 1024 / 1024.
- [ ] `metrics.nats.{id}.server.storage_used_mb` — занято на диске JetStream хранилищем. Источник: `GET /jsz` → `storage` (bytes). Это именно то, что NATS пишет на диск для stream-ов и KV.
- [ ] `metrics.nats.{id}.server.storage_total_mb` — максимальный лимит JetStream storage (из конфига). Источник: `GET /jsz` → `max_storage`. Если `0` — лимит не задан (unlimited).

**Состояние соединения:**
- [ ] `metrics.nats.{id}.connected` — `1.0`/`0.0`. Источник: `nc.Status() == nats.CONNECTED`.
- [ ] `metrics.nats.{id}.reconnects` — переподключений всего. Источник: `nc.Stats().Reconnects`.
- [ ] `metrics.nats.{id}.rtt_ms` — RTT до nats-server (ms). Источник: `nc.RTT()`.
- [ ] `metrics.nats.{id}.in_msgs` — входящих сообщений. Источник: `nc.Stats().InMsgs`.
- [ ] `metrics.nats.{id}.out_msgs` — исходящих сообщений. Источник: `nc.Stats().OutMsgs`.
- [ ] `metrics.nats.{id}.in_bytes` — входящих байт. Источник: `nc.Stats().InBytes`.
- [ ] `metrics.nats.{id}.out_bytes` — исходящих байт. Источник: `nc.Stats().OutBytes`.

**JetStream состояние:**
- [ ] `metrics.nats.{id}.js.stream.metrics.messages` — сообщений в stream METRICS. Источник: `js.StreamInfo("METRICS").State.Msgs`.
- [ ] `metrics.nats.{id}.js.stream.metrics.bytes` — байт в stream METRICS.
- [ ] `metrics.nats.{id}.js.stream.metrics.subjects` — уникальных subjects в stream METRICS.
- [ ] `metrics.nats.{id}.js.kv.cluster.messages` — ключей в KV `asty-cluster`. Источник: `js.StreamInfo("KV_asty-cluster").State.Msgs`.
- [ ] `metrics.nats.{id}.js.kv.cluster.bytes` — байт в KV `asty-cluster`.

**Реализация:**
- [ ] Создать `publishNATSMetrics(ctx)` в `Server` и аналог в `Agent`.
- [ ] Для `/varz` и `/jsz`: HTTP GET на `127.0.0.1:{monitoring_port}`. Порт брать из конфига (`A_NATS_MONITORING_PORT`, default 8222). При ошибке соединения — пропускать, не падать.
- [ ] `js.StreamInfo` только на сервере; ошибки логировать debug и пропускать.

---

### 10. metrics.cluster — операционное состояние кластера

**Файлы:** `server.go`, `state.go`

Публикуются **только лидером** раз в EvalInterval. Данные берутся из `ClusterState` — реального состояния KV, не snapshot-а. Subjects не содержат node_id, так как это кластерные агрегаты.

**Subjects и источники:**

Ноды:
- [ ] `metrics.cluster.nodes.total` — всего нод в KV. Источник: `len(ListNodes())`.
- [ ] `metrics.cluster.nodes.healthy` — `status=ready` + heartbeat не старше `nodeStaleAfter`. Источник: `filterHealthyNodes(ListNodes())`.
- [ ] `metrics.cluster.nodes.draining` — `status=draining`. Источник: `ListNodes()`.
- [ ] `metrics.cluster.nodes.down` — `status=down` или stale. Источник: `ListNodes()`.

Аллокации (по всем сервисам):
- [ ] `metrics.cluster.allocs.running` — аллокаций в статусе `running`.
- [ ] `metrics.cluster.allocs.pending` — аллокаций в статусе `pending`.
- [ ] `metrics.cluster.allocs.starting` — аллокаций в статусе `starting`.
- [ ] `metrics.cluster.allocs.failed` — аллокаций в статусе `failed`.
- [ ] `metrics.cluster.allocs.stopped` — аллокаций в статусе `stopped`.

Источник для аллокаций: `ListAllAllocations()` (уже существует в ClusterState).

Сервисы:
- [ ] `metrics.cluster.services.total` — всего сервисов в конфиге.
- [ ] `metrics.cluster.services.healthy` — сервисов с `live >= targetCopies` (не деградированы).
- [ ] `metrics.cluster.services.degraded` — сервисов с `live < targetCopies`.

Ёмкость ресурсов:
- [ ] `metrics.cluster.capacity.cpu_total_mhz` — сумма `CPUTotal` по healthy-нодам.
- [ ] `metrics.cluster.capacity.cpu_used_mhz` — сумма `CPUTotal - CPUAvailable` по healthy-нодам.
- [ ] `metrics.cluster.capacity.memory_total_mb` — сумма `MemoryTotal` по healthy-нодам.
- [ ] `metrics.cluster.capacity.memory_used_mb` — сумма `MemoryTotal - MemoryAvailable` по healthy-нодам.

Диск кластера (агрегат по нодам):
- [ ] `metrics.cluster.capacity.disk_total_mb` — сумма `disk_total_mb` по healthy-нодам. Источник: MetricsCache, subjects `metrics.node.{id}.disk_total_mb`.
- [ ] `metrics.cluster.capacity.disk_used_mb` — сумма `disk_used_mb` по healthy-нодам.
- [ ] `metrics.cluster.capacity.disk_available_mb` — сумма `disk_available_mb` по healthy-нодам.

Здоровье кластера:
- [ ] `metrics.cluster.health_percent` — интегральная метрика здоровья (0–100). Вычисляется как взвешенная сумма:
  - 50% вес: `nodes_healthy / nodes_total`
  - 50% вес: `services_healthy / services_total`
  - Итог: `(nodes_healthy/nodes_total * 0.5 + services_healthy/services_total * 0.5) * 100`
  - Если нет нод или сервисов — `0.0`.

**Реализация:**
- [ ] Создать `publishClusterMetrics(ctx)` в `Server` — горутина в `leaderCtx`.
- [ ] Диск агрегируется из MetricsCache (читать `metrics.node.*.disk_*`) — не из KV. Убедиться что агент уже публикует disk метрики (задача 3) до запуска задачи 10.
- [ ] Вычисление degraded: `ListAllocations()` на каждый сервис, считать live vs targetCopies.

---

### 11. Prometheus `/metrics` (расширенный)

**Файлы:** `api.go`

- [ ] Реализовать `handleMetrics()` из MetricsCache (убрать TODO-заглушку).

**Метрики ресурсов нод и сервисов** (из `metrics.node.*`):
  - `asty_node_cpu_percent{node_id,datacenter}` — CPU % ноды
  - `asty_node_memory_mb{node_id,datacenter}` — Memory MB ноды
  - `asty_node_rps{node_id,datacenter}` — RPS ноды
  - `asty_service_cpu_percent{service}` — avg CPU по сервису
  - `asty_service_memory_mb{service}` — avg memory по сервису
  - `asty_service_copies{service}` — текущее количество живых копий
  - `asty_cluster_cpu_percent`, `asty_cluster_memory_mb`, `asty_cluster_rps` — кластерные агрегаты

**Метрики оркестратора** (из `metrics.asty.*`):
  - `asty_orchestrator_is_leader{node_id}` — лидерство
  - `asty_orchestrator_workqueue_depth{node_id}` — глубина очереди
  - `asty_orchestrator_workqueue_delayed{node_id}` — ключи в backoff
  - `asty_orchestrator_reconcile_errors_total{node_id}` — ошибки reconcile
  - `asty_orchestrator_placements_total{node_id}` — созданных аллокаций
  - `asty_orchestrator_dispatch_sent_total{node_id}` — отправленных start-команд
  - `asty_orchestrator_dispatch_failed_total{node_id}` — упавших start-команд
  - `asty_orchestrator_scale_up_total{node_id}`, `asty_orchestrator_scale_down_total{node_id}` — решения автоскейлера

**Метрики NATS** (из `metrics.nats.*`):
  - `asty_nats_connected{node_id}` — статус соединения
  - `asty_nats_reconnects_total{node_id}` — переподключения
  - `asty_nats_in_msgs_total{node_id}`, `asty_nats_out_msgs_total{node_id}` — трафик сообщений
  - `asty_nats_in_bytes_total{node_id}`, `asty_nats_out_bytes_total{node_id}` — трафик байт
  - `asty_nats_rtt_ms{node_id}` — RTT
  - `asty_nats_js_stream_messages{node_id,stream}` — сообщений в JetStream stream
  - `asty_nats_js_stream_bytes{node_id,stream}` — байт в JetStream stream

**Метрики кластера** (из `metrics.cluster.*`):
  - `asty_cluster_nodes_total`, `asty_cluster_nodes_healthy`, `asty_cluster_nodes_draining`, `asty_cluster_nodes_down`
  - `asty_cluster_allocs_total{status}` — аллокации по статусу (running/pending/starting/failed/stopped)
  - `asty_cluster_services_total`, `asty_cluster_services_healthy`, `asty_cluster_services_degraded`
  - `asty_cluster_capacity_cpu_total_mhz`, `asty_cluster_capacity_cpu_used_mhz`
  - `asty_cluster_capacity_memory_total_mb`, `asty_cluster_capacity_memory_used_mb`
  - `asty_nodes_total`, `asty_nodes_healthy` — уже есть, сохранить

---

### 12. Удаление старого слоя

**Файлы:** `metrics_store.go`, `streamhub.go`, `server.go`, `agent.go`

- [ ] Удалить `metrics_store.go` целиком (MetricsStore, ScalingEvent, IngestSnapshot, GetEvents, AddEvent).
- [ ] Удалить `ScalingEvent` и `AddEvent` — события теперь только в `EventBuffer` (ClusterEvent).
- [ ] Убрать поле `metricsStore *MetricsStore` из `Server`.
- [ ] Убрать передачу `metricsStore` в `Autoscaler`.
- [ ] Убрать вызов `IngestSnapshot` из `streamhub.refresh()`.
- [ ] Убрать поля `CPUUsage`, `MemoryUsage`, `HealthStatus` из `ServiceAllocation` (после проверки что нигде не используются).
- [ ] Проверить тесты: `scheduler_test.go`, `workqueue_test.go` — убрать зависимости на MetricsStore если есть.

---

## Порядок выполнения

```
Задача 1  (нода порядок)       →  независимо, можно первой
Задача 2  (JetStream stream)   →  блокирует 3, 4, 8, 9, 10
Задача 3  (агент publish)      →  после 2
Задача 4  (MetricsCache)       →  после 2
Задача 5  (snapshot merge)     →  после 4
Задача 6  (SSE handlers)       →  после 5
Задача 7  (автоскейлер)        →  после 4
Задача 8  (metrics.asty)       →  после 2; счётчики в controller/autoscaler независимы от 4
Задача 9  (metrics.nats)       →  после 2
Задача 10 (metrics.cluster)    →  после 2
Задача 11 (Prometheus)         →  после 4, 8, 9, 10
Задача 12 (удаление)           →  после 6, 7, 11 — финальная зачистка
```

Задача 1 полностью независима. Задачи 8–10 (новые системные метрики) могут идти параллельно с задачами 3–7 — они не зависят друг от друга, только от готовности JetStream stream (задача 2).

---

## Что НЕ трогать

- Архитектура SSE hub (allocIndex, snapshot, fan-out) — работает корректно
- Drain через NATS pub/sub — event-driven, не трогать
- LogBuffer и log streaming — работает
- EventBuffer (ClusterEvent, ring buffer) — работает
- Gateway → `asty.v1.metrics.gateway.*` публикация — менять только приёмник на сервере
