# Asty Refactoring: Feature-Based Architecture

**Цель**: Перейти от плоской структуры (30+ файлов в одной папке) к Feature-Based архитектуре с четкой изоляцией компонентов.

**Статус**: Не начат  
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

- Phase 1: [ ] 0/5 задач
- Phase 2: [ ] 0/5 блоков
- Phase 3: [ ] 0/8 блоков
- Phase 4: [ ] 0/4 блока
- Phase 5: [ ] 0/6 задач

**Общий прогресс**: 0% (0/28 блоков)
