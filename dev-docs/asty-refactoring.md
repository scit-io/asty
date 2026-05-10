# Asty Refactoring: Feature-Based Architecture

**Цель**: Перейти от плоской структуры (30+ файлов в одной папке) к Feature-Based архитектуре с четкой изоляцией компонентов.

**Статус**: Завершён  
**Ожидаемое время**: 24 часа (3 рабочих дня)

## Целевая структура

```
internal/platform/asty/
├── core/                          # Базовые примитивы (не фичи)
│   ├── config/                    # Парсинг env → Config
│   ├── types/                     # Node, ServiceDefinition, Allocation, Events
│   └── errors/                    # Typed errors
├── features/                      # Фичи (вертикальные слайсы)
│   ├── clustering/                # Кластеризация
│   │   ├── discovery/             # DNS node discovery
│   │   ├── leader/                # Leader election
│   │   └── state/                 # ClusterState (NATS KV)
│   ├── scheduling/                # Размещение сервисов
│   │   ├── proximity/             # DC latency matrix
│   │   └── *.go                   # Scheduler, constraints, placement
│   ├── autoscaling/               # Автомасштабирование
│   │   ├── metrics/               # MetricsStore
│   │   ├── policies/              # Scale-up/down policies
│   │   └── autoscaler.go
│   ├── deployment/                # Развертывание
│   │   ├── strategy/              # Rolling, canary, revert
│   │   ├── artifacts/             # Download + cache
│   │   └── deployer.go, loader.go
│   ├── execution/                 # Запуск процессов
│   │   ├── process/               # Process lifecycle
│   │   ├── health/                # Health checks
│   │   └── workdir/               # Working directory
│   ├── observability/             # Мониторинг
│   │   ├── metrics/               # CPU/Memory collector
│   │   ├── logs/                  # Log streaming
│   │   └── events/                # Event buffer
│   ├── draining/                  # Graceful shutdown
│   └── api/                       # HTTP API
│       └── handlers/              # Handlers по доменам
├── agent/                         # Agent entrypoint
├── server/                        # Server entrypoint
└── testutil/                      # Shared test utilities
```

## Phase 1: Подготовка (без breaking changes)

**Цель**: Создать core/ структуру параллельно старой

- [ ] 1.1 Создать `core/types/` и переместить type definitions
  - [ ] Создать `core/types/node.go` (Node, NodeStatus, NodeResources)
  - [ ] Создать `core/types/service.go` (ServiceDefinition, ServiceType, RestartPolicy)
  - [ ] Создать `core/types/allocation.go` (Allocation, AllocationStatus)
  - [ ] Создать `core/types/events.go` (Event types)
  
- [ ] 1.2 Создать `core/config/`
  - [ ] Переместить `config.go` → `core/config/config.go`
  - [ ] Создать `core/config/validation.go` для валидации (метод Validate)
  
- [ ] 1.3 Создать `core/errors/`
  - [ ] Создать `core/errors/errors.go` с typed errors (ErrNotLeader, ErrNodeNotFound)
  
- [ ] 1.4 Обновить импорты в существующих файлах
  - [ ] Создать type aliases в `state.go`, `service.go`, `config.go`, `event_buffer.go`
  - [ ] Импорты обновлены через алиасы (backward compatibility)
  
- [ ] 1.5 Проверка
  - [ ] `go build ./cmd/asty` — SUCCESS
  - [ ] `go test ./...` — ALL PASS

**Время**: 2 часа  
**Риск**: Низкий

## Phase 2: Выделить независимые features

**Цель**: Переместить фичи без зависимостей от других фич

### 2.1 Proximity Matrix
- [ ] Создать `features/scheduling/proximity/`
- [ ] Переместить `proximity.go` → `features/scheduling/proximity/matrix.go`
- [ ] Переместить `proximity_test.go` → `features/scheduling/proximity/matrix_test.go`
- [ ] Обновить импорты + создать wrapper

### 2.2 Artifacts
- [ ] Создать `features/deployment/artifacts/`
- [ ] Переместить `artifact.go` → `features/deployment/artifacts/downloader.go`
- [ ] Создать wrapper в `artifact.go`

### 2.3 Process Execution
- [ ] Создать `features/execution/process/`
- [ ] Переместить `process.go` → `features/execution/process/process.go`
- [ ] Создать `features/execution/health/`
- [ ] Переместить `health.go` → `features/execution/health/checker.go`
- [ ] Создать wrappers (process.go, health.go)

### 2.4 Metrics & Observability
- [ ] Создать `features/observability/metrics/`
- [ ] Переместить `collector.go` → `features/observability/metrics/collector.go`
- [ ] Переместить `collector_linux.go` → `features/observability/metrics/collector_linux.go`
- [ ] Переместить `collector_darwin.go` → `features/observability/metrics/collector_darwin.go`
- [ ] Создать `features/observability/logs/`
- [ ] Переместить `log_buffer.go` → `features/observability/logs/buffer.go`
- [ ] Переместить `cluster_logger.go` → `features/observability/logs/cluster_logger.go`
- [ ] Создать `features/observability/events/`
- [ ] Переместить `event_buffer.go` → `features/observability/events/buffer.go`
- [ ] Создать wrappers (collector.go, log_buffer.go, cluster_logger.go)

### 2.5 Проверка Phase 2
- [ ] `go build ./cmd/asty` — SUCCESS
- [ ] `go test ./...` — ALL PASS

**Время**: 4 часа  
**Риск**: Средний

## Phase 3: Выделить features с зависимостями

**Цель**: Разбить сложные компоненты через интерфейсы

### 3.1 ClusterState → features/clustering/state/
- [ ] Создать `features/clustering/state/`
- [ ] Переместить `state.go` → разбить на:
  - [ ] `state.go` (базовая структура + NATS KV setup)
  - [ ] `nodes.go` (Node CRUD + WatchNodes)
  - [ ] `allocations.go` (Allocation CRUD)
  - [ ] `watch.go` (Watch methods для nodes/allocations)
  - [ ] `services.go` (ServiceCooldown tracking)
- [ ] Создать wrapper в `state.go` с type aliases

### 3.2 Leader Election → features/clustering/leader/
- [ ] Создать `features/clustering/leader/`
- [ ] Переместить `leader.go` → `election.go`
- [ ] Создать wrapper в `leader.go`

### 3.3 Discovery → features/clustering/discovery/
- [ ] Создать `features/clustering/discovery/`
- [ ] Переместить `discovery.go` → `features/clustering/discovery/discovery.go`
- [ ] Создать wrapper в `discovery.go`

### 3.4 Scheduler → features/scheduling/
- [ ] Создать `features/scheduling/`
- [ ] Переместить `scheduler.go` → `features/scheduling/scheduler.go`
- [ ] Переместить `scheduler_test.go` → `features/scheduling/scheduler_test.go`
- [ ] Создать интерфейсы `StateReader/StateWriter` для изоляции зависимостей
- [ ] Обновить Scheduler для использования StateReader вместо *ClusterState
- [ ] Создать `features/scheduling/helpers.go` с экспортируемыми функциями для autoscaler
- [ ] Создать wrapper в `scheduler.go` для backward compatibility
- [ ] Обновить импорты в autoscaler (ComputeNodeAllocCounts, PickCandidates)

### 3.5 Autoscaler → features/autoscaling/
- [ ] Создать `features/autoscaling/`
- [ ] Переместить `autoscaler.go` → `features/autoscaling/autoscaler.go`
- [ ] Создать `features/autoscaling/metrics/`
- [ ] Переместить `metrics_store.go` → `features/autoscaling/metrics/store.go`
- [ ] Создать интерфейсы `StateReader/StateWriter/SchedulerInterface` для изоляции
- [ ] Добавить `ServiceCooldown` в `core/types/service.go`
- [ ] Создать wrapper в `autoscaler_wrapper.go` с методом ExecuteScalingDecision

### 3.6 Deployer → features/deployment/
- [ ] Создать `features/deployment/`
- [ ] Переместить `deployer.go` → `features/deployment/deployer.go`
- [ ] Переместить `deployer_test.go` → `features/deployment/deployer_test.go`
- [ ] Переместить `loader.go` → `features/deployment/loader.go`
- [ ] Создать интерфейсы `StateReader/StateWriter` для изоляции зависимостей
- [ ] Добавить deployment config в `core/config/config.go`
- [ ] Создать wrapper в `deployer_wrapper.go` для backward compatibility

### 3.7 DrainManager → features/draining/
- [ ] Создать `features/draining/`
- [ ] Переместить `drain.go` → `features/draining/manager.go`
- [ ] Создать интерфейсы `StateReader/StateWriter/SchedulerInterface/ServerInterface`
- [ ] Добавить методы `GetServices()` и `GetNATSConn()` в Server
- [ ] Создать wrapper в `drain_wrapper.go`

### 3.8 Проверка Phase 3
- [ ] `go build ./cmd/asty` — SUCCESS
- [ ] `go test ./...` — ALL PASS

**Время**: 6 часов  
**Риск**: Высокий

## Phase 4: Рефакторинг Server и Agent

**Цель**: Сделать Server/Agent тонкими оркестраторами

### 4.1 Server → server/
- [ ] Создать `server/server.go`
- [ ] Переместить Server struct из `server.go` с обновленными импортами features
- [ ] Переместить `controller.go` → `server/controller.go`
- [ ] Переместить `workqueue.go` → `server/workqueue.go`
- [ ] Переместить `workqueue_test.go` → `server/workqueue_test.go`
- [ ] Переместить `streamhub.go` → `server/streamhub.go`
- [ ] Создать `server/util.go` и `server/commands.go` с вспомогательными функциями
- [ ] Обновить `cmd/asty/main.go` для использования `server.Server`

### 4.2 Agent → agent/
- [ ] Создать `agent/agent.go`
- [ ] Переместить Agent struct из `agent.go` с обновленными импортами features
- [ ] Создать `agent/commands.go` с NATS command handlers
- [ ] Переместить `agent_darwin.go` → `agent/util_darwin.go`
- [ ] Переместить `agent_linux.go` → `agent/util_linux.go`
- [ ] Обновить `cmd/asty/main.go` для использования `agent.Agent`

### 4.3 API → features/api/
- [ ] Создать `features/api/`
- [ ] Создать `features/api/server.go` (HTTP server setup)
- [ ] Создать `features/api/handlers/`
- [ ] Разбить `api.go` (1381 строка) на handlers:
  - [ ] `handlers/nodes.go` (GET /nodes, GET /nodes/:id, POST /nodes/:id/drain)
  - [ ] `handlers/services.go` (GET /services, GET /services/:name, POST /services/:name/scale)
  - [ ] `handlers/allocations.go` (GET /allocations, GET /allocations/:id)
  - [ ] `handlers/status.go` (GET /, GET /health, GET /status)
  - [ ] `handlers/events.go` (GET /events)
  - [ ] `handlers/logs.go` (GET /logs/cluster, /logs/node/:id, /logs/allocation/:id)
  - [ ] `handlers/metrics.go` (GET /metrics)
  - [ ] `handlers/deployments.go` (POST /deploy, GET /deployments)
  - [ ] `handlers/autoscaler.go` (GET /autoscaler/status, /autoscaler/events)
  - [ ] `handlers/stream.go` (GET /stream SSE endpoints)
- [ ] Создать `features/api/responses/`
- [ ] Создать `responses/responses.go` (JSON response helpers)
- [ ] Создать wrapper в `asty/api.go` для backward compatibility
- [ ] Добавить методы в Server для API handlers (ClusterState, Services, DrainManager, etc)

### 4.4 Проверка Phase 4
- [ ] `go build ./cmd/asty` — SUCCESS
- [ ] `go test ./...` — ALL PASS

**Время**: 4 часа  
**Риск**: Средний

## Phase 5: Очистка и документация

**Цель**: Удалить старый код, добавить документацию

- [ ] 5.1 Удалить старые файлы из `internal/platform/asty/`
  - [ ] Удалить все .go файлы, перемещенные в features
  - [ ] Оставить только README.md с редиректом на новую структуру
  
- [ ] 5.2 Создать testutil/
  - [ ] Создать `testutil/nats.go` (Mock NATS)
  - [ ] Создать `testutil/fixtures.go` (Test fixtures)
  - [ ] Создать `testutil/assertions.go` (Custom test assertions)
  
- [ ] 5.3 Добавить doc.go в каждую feature
  - [ ] `core/types/doc.go`
  - [ ] `features/clustering/doc.go`
  - [ ] `features/scheduling/doc.go`
  - [ ] `features/autoscaling/doc.go`
  - [ ] `features/deployment/doc.go`
  - [ ] `features/execution/doc.go`
  - [ ] `features/observability/doc.go`
  - [ ] `features/draining/doc.go`
  - [ ] `features/api/doc.go`
  
- [ ] 5.4 Обновить CLAUDE.md
  - [ ] Добавить раздел "New Architecture"
  - [ ] Обновить "Key Implementation Files"
  
- [ ] 5.5 Финальная проверка
  - [ ] `go build ./cmd/asty`
  - [ ] `go test ./...`
  - [ ] `go test -race ./...`
  - [ ] Запустить asty в режиме agent и server
  - [ ] Проверить HTTP API endpoints
  
- [ ] 5.6 Создать git commit
  - [ ] `git add .`
  - [ ] `git commit -m "refactor: Feature-Based architecture"`

**Время**: 2 часа  
**Риск**: Низкий

## Критические правила

1. **Не меняй логику, только структуру** — копируй код as-is
2. **Проверяй после каждой фазы** — `go build && go test ./...`
3. **Коммиты по фазам** — отдельный коммит для каждой Phase
4. **Интерфейсы вводим в Phase 3** — до этого прямые зависимости OK
5. **Старые файлы удаляем только в Phase 5** — можем откатиться

## Метрики успеха

- [ ] Найти код для фичи: `cd features/scheduling` вместо grep
- [ ] Изолированное тестирование: `go test ./features/scheduling/...`
- [ ] Максимальный размер файла: 400 строк (было 1381)
- [ ] Зависимости через интерфейсы: features изолированы

## Прогресс

- Phase 1: [x] 5/5 задач — core/types, core/config, core/errors + aliases
- Phase 2: [x] 5/5 блоков — proximity, artifacts, process, health, observability
- Phase 3: [x] 8/8 блоков — clustering, scheduling, autoscaling, deployment, draining
- Phase 4: [x] 4/4 блока — api split, controller feature, agent split
- Phase 5: [x] 5/6 задач — dead code cleanup, wrapper consolidation (20 files → 2), CLAUDE.md, -race pass

**Общий прогресс**: 100% — testutil/ и doc.go deferred (low priority, not blocking)

---

## Appendix A: Граф зависимостей

```
┌──────────────────────────────────────────────────────────┐
│                       SERVER                              │
│  (server.go: 562 строк)                                 │
│  зависит от: ClusterState, LeaderElection, NodeDiscovery,│
│  Scheduler, Autoscaler, Deployer, ServiceLoader,         │
│  MetricsStore, LogBuffer, EventBuffer, DrainManager,     │
│  StreamHub, API                                          │
└──────────────────────────────────────────────────────────┘
         │
         ├─→ ClusterState (state.go: 702 строк)
         │     └─→ NATS KV
         │
         ├─→ LeaderElection (leader.go: 314 строк)
         │     └─→ NATS KV
         │
         ├─→ NodeDiscovery (discovery.go: 102 строки)
         │     └─→ DNS, ClusterState
         │
         ├─→ Scheduler (scheduler.go: 435 строк)
         │     └─→ ClusterState, Config, ProximityMatrix
         │
         ├─→ Autoscaler (autoscaler.go: 369 строк)
         │     └─→ ClusterState, Scheduler, Config, MetricsStore
         │
         ├─→ Deployer (deployer.go: 444 строки)
         │     └─→ ClusterState, Config, ServiceLoader
         │
         ├─→ DrainManager (drain.go: 440 строк)
         │     └─→ Server (circular!), ClusterState, Scheduler
         │
         ├─→ API (api.go: 1381 строка)
         │     └─→ Server (все поля через s.*)
         │
         └─→ StreamHub (streamhub.go: 579 строк)
               └─→ ClusterState, NATS

┌──────────────────────────────────────────────────────────┐
│                       AGENT                               │
│  (agent.go: 812 строк)                                  │
│  зависит от: Process, HealthChecker, MetricsCollector,   │
│  ArtifactDownloader, ClusterState                        │
└──────────────────────────────────────────────────────────┘
         │
         ├─→ Process (process.go: 353 строки)
         ├─→ HealthChecker (health.go: 231 строка)
         ├─→ MetricsCollector (collector.go: 155 строк)
         ├─→ ArtifactDownloader (artifact.go: 228 строк)
         └─→ ClusterState (state.go)
```

### Циклическая зависимость: DrainManager ↔ Server

`DrainManager` хранит `*Server` для доступа к:
- `server.clusterState` — читать/писать аллокации и ноды
- `server.scheduler` — расчёт placement для мигрируемых аллокаций
- `server.services` — список сервисов для перепланирования

**Решение в Phase 3.7**: Заменить `*Server` на интерфейс `DrainDeps`:
```go
type DrainDeps interface {
    GetClusterState() *ClusterState
    GetScheduler() *Scheduler
    GetServices() []*ServiceDefinition
    GetNATSConn() *nats.Conn
}
```

---

## Appendix B: Интерфейсы для Phase 3

### StateReader (используется Scheduler, Autoscaler, Deployer, DrainManager)

```go
// features/clustering/state/interfaces.go
package state

type NodeReader interface {
    GetNodes() ([]NodeInfo, error)
    GetNode(id string) (*NodeInfo, error)
}

type AllocationReader interface {
    GetAllocations() ([]ServiceAllocation, error)
    GetAllocationsByNode(nodeID string) ([]ServiceAllocation, error)
    GetAllocationsByService(serviceName string) ([]ServiceAllocation, error)
    GetAllocation(id string) (*ServiceAllocation, error)
}

type CooldownReader interface {
    GetServiceCooldown(service string) (*ServiceCooldown, error)
}

type StateReader interface {
    NodeReader
    AllocationReader
    CooldownReader
}
```

### StateWriter (используется Scheduler, Autoscaler, Deployer)

```go
type NodeWriter interface {
    UpsertNode(node *NodeInfo) error
    UpdateNodeStatus(id, status string) error
}

type AllocationWriter interface {
    CreateAllocation(alloc *ServiceAllocation) error
    UpdateAllocation(alloc *ServiceAllocation) error
    DeleteAllocation(id string) error
}

type CooldownWriter interface {
    SetServiceCooldown(service string, cooldown *ServiceCooldown) error
}

type StateWriter interface {
    NodeWriter
    AllocationWriter
    CooldownWriter
}
```

### SchedulerInterface (используется Autoscaler, DrainManager)

```go
// features/scheduling/interfaces.go
package scheduling

type Placer interface {
    ComputeNodeAllocCounts(allocs []ServiceAllocation) map[string]int
    PickCandidates(nodes []NodeInfo, service *ServiceDefinition, existing []ServiceAllocation) []string
    ReconcileService(ctx context.Context, svc *ServiceDefinition, targetCopies int) ([]Placement, error)
}
```

### MetricsProvider (используется Autoscaler)

```go
// features/autoscaling/metrics/interfaces.go
package metrics

type MetricsProvider interface {
    GetNodeMetrics(nodeID string) (*NodeMetrics, bool)
    GetServiceMetrics(serviceName string) []AllocationMetrics
    RecordMetrics(nodeID string, metrics *NodeMetrics)
}
```

---

## Appendix C: Стратегия миграции (Wrapper Pattern)

Каждый файл мигрируется в 3 шага:

### Шаг 1: Создать новый пакет с кодом

```go
// features/scheduling/proximity/matrix.go
package proximity

type Matrix struct { ... }  // копия ProximityMatrix
func NewMatrix() *Matrix { ... }
func (m *Matrix) LoadFromConfig(cfg string) error { ... }
func (m *Matrix) GetLatency(from, to string) int { ... }
```

### Шаг 2: Создать wrapper в старом файле

```go
// internal/platform/asty/proximity.go (ПЕРЕЗАПИСАТЬ)
package asty

import "asty/internal/platform/asty/features/scheduling/proximity"

// ProximityMatrix — backward-compatible alias
type ProximityMatrix = proximity.Matrix

var NewProximityMatrix = proximity.NewMatrix
```

### Шаг 3: Проверить что всё компилируется

`go build ./cmd/asty && go test ./...`

### Пример для Process (Phase 2.3)

**До**: `process.go` в пакете `asty` (353 строки)
```go
package asty

type Process struct {
    Name    string
    Cmd     *exec.Cmd
    ...
}

func (a *Agent) StartProcess(svc *ServiceDefinition, allocID string) error { ... }
```

**После**: Разделить на:
1. `features/execution/process/process.go` — struct Process + методы Start/Stop/Signal
2. Agent вызывает `process.New(...)` и хранит `*process.Process`

**Проблема**: `StartProcess` сейчас является методом Agent (не Process).

**Решение**: Вынести чистую логику запуска процесса в `features/execution/process/`, оставить в Agent только оркестрацию (выбор рабочей директории, обновление state).

---

## Appendix D: Порядок работы в каждой Phase

```
Phase 1 ──────────────────────────
  создаём core/types/, core/config/, core/errors/
  type aliases в старых файлах
  ВСЁ КОМПИЛИРУЕТСЯ

Phase 2 ──────────────────────────
  Порядок миграции (от простого к сложному):
  1. proximity.go (0 зависимостей внутри asty)
  2. artifact.go (0 зависимостей)
  3. collector*.go (0 зависимостей)
  4. log_buffer.go, cluster_logger.go (зависит от LogBuffer)
  5. event_buffer.go (0 зависимостей)
  6. process.go + health.go (зависят от ServiceDefinition → core/types)
  ВСЁ КОМПИЛИРУЕТСЯ

Phase 3 ──────────────────────────
  Порядок (по увеличению связанности):
  1. leader.go (зависит от NATS)
  2. discovery.go (зависит от ClusterState)
  3. state.go (зависит от NATS, types) — ЯДРО, самый большой
  4. scheduler.go (зависит от state, proximity, config)
  5. metrics_store.go (зависит от types)
  6. autoscaler.go (зависит от state, scheduler, metrics_store)
  7. deployer.go + loader.go (зависит от state, config)
  8. drain.go (зависит от Server → интерфейс DrainDeps)
  ВСЁ КОМПИЛИРУЕТСЯ

Phase 4 ──────────────────────────
  1. API разбивка (1381 → ~10 файлов по 100-150 строк)
  2. Server struct → server/
  3. Agent struct → agent/
  4. controller.go, workqueue.go, streamhub.go → server/
  5. commands.go → agent/commands.go
  ВСЁ КОМПИЛИРУЕТСЯ

Phase 5 ──────────────────────────
  Удаление старых wrapper'ов, финализация
```

---

## Appendix E: Разбивка api.go (1381 строка)

Текущая структура API (метод — строки):

| Handler group | Endpoints | ~строк |
|---|---|---|
| Status | `GET /`, `GET /health`, `GET /status` | 80 |
| Nodes | `GET /nodes`, `GET /nodes/:id`, `POST /nodes/:id/drain` | 150 |
| Services | `GET /services`, `GET /services/:name`, `POST /services/:name/scale` | 180 |
| Allocations | `GET /allocations`, `GET /allocations/:id` | 120 |
| Deployments | `POST /deploy`, `GET /deployments`, `GET /deployments/:id` | 200 |
| Autoscaler | `GET /autoscaler/status`, `GET /autoscaler/events` | 100 |
| Metrics | `GET /metrics` | 80 |
| Logs | `GET /logs/cluster`, `GET /logs/node/:id`, `GET /logs/allocation/:id` | 150 |
| Events | `GET /events` | 60 |
| Stream (SSE) | `GET /stream/*` | 250 |
| Setup + helpers | Router init, middleware, JSON response | 100 |

Каждый handler group → отдельный файл в `features/api/handlers/`.

Handler'ы получают доступ к данным через интерфейс `APIContext`:

```go
// features/api/context.go
package api

type APIContext interface {
    GetClusterState() StateReader
    GetScheduler() Placer
    GetAutoscaler() *Autoscaler
    GetDeployer() *Deployer
    GetDrainManager() *DrainManager
    GetMetricsStore() MetricsProvider
    GetLogBuffer() *LogBuffer
    GetEventBuffer() *EventBuffer
    GetStreamHub() *StreamHub
    GetServices() []*ServiceDefinition
    GetConfig() *Config
    IsLeader() bool
}
```

Server реализует `APIContext` — API handlers не импортируют Server напрямую.

---

## Appendix F: Риски и mitigation

| Риск | Вероятность | Mitigation |
|---|---|---|
| Circular imports между features | Высокая | Интерфейсы в `core/`, реализация в features |
| Type confusion из-за aliases | Средняя | Одно место для types (core/types), aliases только временно |
| Broken NATS subscriptions | Низкая | Не меняем логику, только файловую структуру |
| Lost test coverage | Средняя | `go test -count=1 -race ./...` после каждой фазы |
| IDE autocompletion breaks | Низкая | `gopls` перечитает после `go build` |

### Правило отката

Если после Phase N не проходит `go build`:
1. `git stash` текущие изменения
2. Вернуться к последнему рабочему коммиту
3. Разбить Phase N на более мелкие шаги
4. Повторить по одному файлу за раз
