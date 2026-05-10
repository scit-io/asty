# Asty Refactoring: Feature-Based Architecture

**Цель**: Перейти от плоской структуры (30+ файлов в одной папке) к Feature-Based архитектуре с четкой изоляцией компонентов.

**Статус**: Завершён  
**Ожидаемое время**: 24 часа (3 рабочих дня)

## Целевая структура (реализовано)

```
internal/platform/asty/
├── core/                          # Базовые примитивы
│   ├── config/                    # Config struct + Load() + Validate()
│   ├── types/                     # Node, ServiceDefinition, Allocation, Events, Commands, Snapshot
│   └── errors/                    # Typed errors (ErrNotLeader, ErrNodeNotFound)
├── features/                      # Фичи (вертикальные слайсы)
│   ├── api/                       # HTTP API (ServerContext interface + handlers)
│   ├── clustering/                # Кластеризация
│   │   ├── controller/            # ServiceController + Workqueue
│   │   ├── discovery/             # DNS node discovery
│   │   ├── leader/                # Leader election (TTL-based)
│   │   └── state/                 # ClusterState (NATS KV)
│   ├── scheduling/                # Размещение сервисов
│   │   ├── proximity/             # DC latency matrix
│   │   └── *.go                   # Scheduler, helpers
│   ├── autoscaling/               # Автомасштабирование
│   │   ├── metrics/               # MetricsStore (RPS timeseries)
│   │   └── autoscaler.go         # Scale-up/down decisions
│   ├── deployment/                # Развертывание
│   │   ├── artifacts/             # tar.gz download + SHA256
│   │   ├── deployer.go           # Rolling updates, canary, auto-revert
│   │   └── loader.go             # .asty file loading
│   ├── execution/                 # Запуск процессов
│   │   ├── process/               # Process lifecycle (start/stop/signals)
│   │   └── health/                # HTTP probe scheduler
│   ├── observability/             # Мониторинг
│   │   ├── metrics/               # CPU/Memory collector (platform-specific)
│   │   ├── logs/                  # LogBuffer + NATSWriter
│   │   └── events/                # EventBuffer (ring buffer)
│   └── draining/                  # DrainManager + DrainDeps interface
├── server/                        # Server sub-package (implements api.ServerContext)
└── agent/                         # Agent sub-package (process management)
```

## Phase 1: Подготовка (без breaking changes)

**Цель**: Создать core/ структуру параллельно старой

- [x] 1.1 Создать `core/types/` и переместить type definitions
  - [x] `core/types/node.go` (Node, NodeStatus, NodeResources)
  - [x] `core/types/service.go` (ServiceDefinition, ServiceType, RestartPolicy)
  - [x] `core/types/allocation.go` (Allocation, AllocationStatus)
  - [x] `core/types/events.go` (Event types)
  
- [x] 1.2 Создать `core/config/`
  - [x] `core/config/config.go` (Config struct + Load() + Validate())
  
- [x] 1.3 Создать `core/errors/`
  - [x] `core/errors/errors.go` с typed errors
  
- [x] 1.4 Обновить импорты
  - [x] All packages import from core/ directly (no aliases needed after Phase 4)
  
- [x] 1.5 Проверка
  - [x] `go build ./cmd/asty` — SUCCESS
  - [x] `go test ./...` — ALL PASS

**Время**: 2 часа  
**Риск**: Низкий

## Phase 2: Выделить независимые features

**Цель**: Переместить фичи без зависимостей от других фич

### 2.1 Proximity Matrix
- [x] `features/scheduling/proximity/matrix.go` + `matrix_test.go`

### 2.2 Artifacts
- [x] `features/deployment/artifacts/downloader.go`

### 2.3 Process Execution
- [x] `features/execution/process/process.go` + `logs_test.go`
- [x] `features/execution/health/checker.go`

### 2.4 Metrics & Observability
- [x] `features/observability/metrics/collector.go` + `_darwin.go` + `_linux.go`
- [x] `features/observability/logs/buffer.go` + `cluster_logger.go`
- [x] `features/observability/events/buffer.go`

### 2.5 Проверка Phase 2
- [x] `go build ./cmd/asty` — SUCCESS
- [x] `go test ./...` — ALL PASS

**Время**: 4 часа  
**Риск**: Средний

## Phase 3: Выделить features с зависимостями

**Цель**: Разбить сложные компоненты через интерфейсы

### 3.1 ClusterState → features/clustering/state/
- [x] `state.go` (базовая структура + NATS KV setup)
- [x] `nodes.go` (Node CRUD + WatchNodes)
- [x] `allocations.go` (Allocation CRUD)
- [x] `watch.go` (Watch methods)
- [x] `services.go` (ServiceCooldown tracking)

### 3.2 Leader Election → features/clustering/leader/
- [x] `election.go`

### 3.3 Discovery → features/clustering/discovery/
- [x] `discovery.go`

### 3.4 Scheduler → features/scheduling/
- [x] `scheduler.go` + `scheduler_test.go`
- [x] `helpers.go` (ComputeNodeAllocCounts, PickCandidates for autoscaler)

### 3.5 Autoscaler → features/autoscaling/
- [x] `autoscaler.go`
- [x] `metrics/store.go`

### 3.6 Deployer → features/deployment/
- [x] `deployer.go` + `deployer_test.go`
- [x] `loader.go` + `loader_test.go`

### 3.7 DrainManager → features/draining/
- [x] `manager.go` with `DrainDeps` interface (breaks Server circular dep)

### 3.8 Controller → features/clustering/controller/
- [x] `controller.go` + `workqueue.go` + `workqueue_test.go`

### 3.9 Проверка Phase 3
- [x] `go build ./cmd/asty` — SUCCESS
- [x] `go test ./...` — ALL PASS

**Время**: 6 часов  
**Риск**: Высокий

## Phase 4: Рефакторинг Server и Agent

**Цель**: Сделать Server/Agent тонкими оркестраторами — реальные Go sub-packages

### 4.1 Server → server/
- [x] Создать `server/server.go` (Server struct, New(), Start())
- [x] Создать `server/lifecycle.go` (connectNATS, command dispatch, ServerContext methods)
- [x] Создать `server/streamhub.go` (implements api.StreamHub interface)
- [x] Создать `server/snapshot.go` (buildSnapshot, allocIndex)
- [x] Создать `server/dispatcher.go` (CommandDispatcher for controller)
- [x] Server implements `api.ServerContext` interface (breaks circular dep)
- [x] Обновить `cmd/asty/main.go` для использования `server.New(cfg)`

### 4.2 Agent → agent/
- [x] Создать `agent/agent.go` (Agent struct, New(), Start(), StartService, StopService)
- [x] Создать `agent/commands.go` (NATS command handlers)
- [x] Создать `agent/lifecycle.go` (heartbeat, process metrics, monitor)
- [x] Создать `agent/sysinfo_darwin.go` (detectCPUMHz, detectMemoryMB)
- [x] Создать `agent/sysinfo_linux.go` (detectCPUMHz, detectMemoryMB)
- [x] Обновить `cmd/asty/main.go` для использования `agent.New(cfg)`

### 4.3 API → features/api/
- [x] Создать `features/api/context.go` (ServerContext, StreamHub, EventBufferReader interfaces)
- [x] Создать `features/api/api.go` (router setup, New(ctx, uiAddr))
- [x] Создать `features/api/nodes.go`
- [x] Создать `features/api/services.go`
- [x] Создать `features/api/allocations.go`
- [x] Создать `features/api/status.go`
- [x] Создать `features/api/autoscaler.go`
- [x] Создать `features/api/logs.go`
- [x] Создать `features/api/stream.go`
- [x] No compat wrappers — cmd/asty imports sub-packages directly

### 4.4 Проверка Phase 4
- [x] `go build ./...` — SUCCESS
- [x] `go test ./...` — ALL PASS
- [x] No .go files remain in root `internal/platform/asty/` package

**Время**: 4 часа  
**Риск**: Средний

## Phase 5: Очистка и документация

**Цель**: Удалить старый код, добавить документацию

- [x] 5.1 Удалить старые файлы из `internal/platform/asty/`
  - [x] Все .go файлы удалены (compat.go, compat_funcs.go, 20 wrapper files)
  - [x] No code remains in root asty package
  
- [x] 5.2 Создать testutil/
  - [x] `testutil/fixtures.go` (NewTestConfig, NewTestNode, NewTestService, NewTestAllocation)
  - [x] `testutil/assertions.go` (AssertNoError, AssertEqual, AssertLen, etc.)
  
- [x] 5.3 Добавить doc.go в каждую feature
  
- [x] 5.4 Обновить CLAUDE.md
  - [x] Обновить "Key Implementation Files" to reflect sub-packages
  - [x] Remove references to compat.go/compat_funcs.go
  
- [x] 5.5 Финальная проверка
  - [x] `go build ./...` — SUCCESS
  - [x] `go test ./...` — ALL PASS
  - [x] `go test -race ./...` — PASS
  
- [x] 5.6 Git commits
  - [x] `243f0e4` — phase 5 cleanup
  - [x] `6239a11` — phase 4 sub-package extraction

**Время**: 2 часа  
**Риск**: Низкий

## Критические правила

1. **Не меняй логику, только структуру** — копируй код as-is
2. **Проверяй после каждой фазы** — `go build && go test ./...`
3. **Коммиты по фазам** — отдельный коммит для каждой Phase
4. **Интерфейсы вводим в Phase 3** — до этого прямые зависимости OK
5. **Старые файлы удаляем только в Phase 5** — можем откатиться

## Метрики успеха

- [x] Найти код для фичи: `cd features/scheduling` вместо grep
- [x] Изолированное тестирование: `go test ./features/scheduling/...`
- [x] Максимальный размер файла: 419 строк (было 1381) — 2 файла чуть >400, остальные <400
- [x] Зависимости через интерфейсы: ServerContext, StreamHub, DrainDeps, EventBufferReader

## Прогресс

- Phase 1: [x] — core/types, core/config, core/errors
- Phase 2: [x] — proximity, artifacts, process, health, observability
- Phase 3: [x] — clustering (state/leader/discovery/controller), scheduling, autoscaling, deployment, draining
- Phase 4: [x] — real sub-packages: server/, agent/, features/api/ with ServerContext interface
- Phase 5: [x] — all old code deleted, CLAUDE.md updated, build + test + race pass

**Общий прогресс**: 100% — all phases complete

### Ключевые решения
- server↔api circular dep → `api.ServerContext` interface (defined in api, implemented by server)
- server↔draining circular dep → `draining.DrainDeps` interface
- Shared DTOs (ClusterSnapshot) → `core/types/snapshot.go`
- cmd/asty imports sub-packages directly (no root asty package, no compat layer)

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
