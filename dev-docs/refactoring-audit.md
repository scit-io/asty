# Asty Refactoring Audit: Phase 6 — Чистота, читаемость, реактивность

**Цель**: Довести пакет `internal/platform/asty/` до состояния, в котором каждый файл читается за один присест (150–200 строк), отсутствует дублирование, polling заменён на event-driven, а имена и переходы понятны человеку без бэкенд-опыта.

**Статус**: Запланирован
**Ожидаемое время**: 28 часов (3.5 рабочих дня)
**Предусловие**: Phase 1–5 (см. `asty-refactoring.md`) — Feature-Based архитектура завершена, но 17 файлов всё ещё превышают 200 строк, есть дублирование и polling-таймеры.

## Текущее состояние

`go build ./... && go test ./... -race` — всё проходит. Файловая структура корректна. Однако:

| Метрика | Значение | Цель |
|---|---|---|
| Файлов > 400 строк | 2 | 0 |
| Файлов > 200 строк | 17 | 0 |
| Дубликатов утилит | 12 пар (см. Appendix A) | 0 |
| `time.Ticker` на polling KV/процессов | 7 (см. Appendix B) | ≤ 2 (только TTL refresh) |
| Stub-эндпоинтов («not implemented») | 4 (см. Appendix C) | 0 |
| Утилит, переписанных вручную вместо stdlib | 5 (см. Appendix D) | 0 |

### Файлы, превышающие 200 строк

| Файл | Строк | Стратегия декомпозиции |
|---|---:|---|
| `features/deployment/deployer.go` | 419 | вынести в подпакет `deployment/deployer/` |
| `features/draining/manager.go` | 416 | вынести в подпакет `draining/manager/` |
| `agent/agent.go` | 378 | разбить внутри `agent/` (services.go, nodeinfo.go, agent.go) |
| `server/lifecycle.go` | 372 | разбить внутри `server/` по ответственности |
| `features/clustering/controller/controller.go` | 358 | разбить внутри `controller/` |
| `features/autoscaling/autoscaler.go` | 344 | вынести в подпакет `autoscaling/autoscaler/` |
| `features/execution/process/process.go` | 321 | разбить внутри `process/` |
| `features/scheduling/scheduler.go` | 320 | вынести в подпакет `scheduling/scheduler/` |
| `features/api/stream.go` | 295 | вынести в подпакет `api/stream/` |
| `features/clustering/leader/election.go` | 290 | разбить внутри `leader/` |
| `features/api/logs.go` | 289 | вынести в подпакет `api/logs/` |
| `server/streamhub.go` | 283 | разбить внутри `server/` (hub.go, fanout.go, subscribe.go) |
| `server/server.go` | 276 | разбить внутри `server/` (server.go + boot.go) |
| `features/scheduling/proximity/matrix.go` | 263 | разбить внутри `proximity/` |
| `agent/lifecycle.go` | 247 | разбить внутри `agent/` (heartbeat.go, restart.go, logstream.go) |
| `server/snapshot.go` | 225 | разбить внутри `server/` (allocindex.go + snapshot.go) |
| `features/execution/health/checker.go` | 224 | разбить внутри `health/` |

## Целевая структура (после Phase 6)

```
internal/platform/asty/
├── core/
│   ├── config/
│   ├── types/
│   ├── errors/
│   └── netutil/                   # NEW: shared NATS connect, getNodeIP, hostname
├── features/
│   ├── api/
│   │   ├── api.go                 # router setup
│   │   ├── nodes.go, services.go, allocations.go, status.go, autoscaler.go
│   │   ├── pathparse.go           # NEW: общий path/query парсер
│   │   ├── method.go              # NEW: methodGuard helper для GET/POST guards
│   │   ├── healthy.go             # NEW: countHealthyNodes хелпер
│   │   ├── stream/                # CHANGED: бывший stream.go (295 → 4 файла)
│   │   │   ├── stream.go          # SSE skeleton + driver
│   │   │   ├── cluster.go
│   │   │   ├── node.go
│   │   │   ├── service.go
│   │   │   └── allocation.go
│   │   └── logs/                  # CHANGED: бывший logs.go (289 → 3 файла)
│   │       ├── logs.go            # общий стриминг
│   │       ├── cluster.go
│   │       ├── node.go
│   │       └── allocation.go
│   ├── autoscaling/
│   │   ├── metrics/
│   │   └── autoscaler/            # CHANGED: бывший autoscaler.go (344 → 5 файлов)
│   │       ├── autoscaler.go      # struct + EvaluateService
│   │       ├── scale_up.go
│   │       ├── scale_down.go
│   │       ├── execute.go
│   │       └── cooldown.go
│   ├── clustering/
│   │   ├── controller/
│   │   │   ├── controller.go      # struct + Run (slim)
│   │   │   ├── reconcile.go       # NEW: reconcile, dispatchPending, pruneFailed
│   │   │   ├── watch.go           # NEW: watchAllocsToQueue, watchNodesToQueue
│   │   │   ├── autoscale.go       # NEW: autoscaleOnce
│   │   │   └── workqueue.go
│   │   ├── discovery/
│   │   ├── leader/
│   │   │   ├── election.go        # struct + lifecycle (slim)
│   │   │   ├── campaign.go        # NEW: claim/refresh/stepDown
│   │   │   └── watch.go           # NEW: WaitForLeader (event-driven), WatchLeadership
│   │   └── state/
│   │       ├── state.go, nodes.go, allocations.go, services.go
│   │       └── watch.go           # CHANGED: один generic watcher
│   ├── deployment/
│   │   ├── artifacts/
│   │   ├── loader.go
│   │   └── deployer/              # CHANGED: бывший deployer.go (419 → 5 файлов)
│   │       ├── deployer.go        # struct + Deploy
│   │       ├── canary.go
│   │       ├── rolling.go
│   │       ├── history.go
│   │       └── wait.go            # event-driven через WatchAllocations
│   ├── draining/
│   │   └── manager/               # CHANGED: бывший manager.go (416 → 5 файлов)
│   │       ├── manager.go         # struct + Start/Resume/GetStatus
│   │       ├── run.go             # runDrain orchestration
│   │       ├── system.go          # dismantleAndConfirm
│   │       ├── migrate.go         # placeReplacement, finalizeMigration
│   │       └── wait.go            # waitFor* через WatchAllocation
│   ├── execution/
│   │   ├── process/
│   │   │   ├── process.go         # struct + Start/Stop/Status
│   │   │   ├── monitor.go         # NEW: process exit notifier (event-based)
│   │   │   └── logs.go            # NEW: TailLogs, GetLogs, file mgmt
│   │   └── health/
│   │       ├── checker.go         # struct + Start/Register/Unregister (slim)
│   │       └── probe.go           # NEW: performCheck, recordResult
│   ├── observability/             # без изменений (все файлы уже малы)
│   └── scheduling/
│       ├── helpers.go             # CHANGED: + datacenterCountsByOccupied (был дубль)
│       ├── proximity/
│       │   ├── matrix.go          # struct + LoadFromConfig (slim)
│       │   ├── sort.go            # NEW: SortDatacentersByProximity (sort.Slice)
│       │   └── validate.go        # NEW: ValidateLatencies, RunValidation
│       └── scheduler/             # CHANGED: бывший scheduler.go (320 → 5 файлов)
│           ├── scheduler.go       # struct + ReconcileService
│           ├── system.go          # reconcileSystem
│           ├── regular.go         # reconcileRegular
│           ├── candidates.go      # PickCandidates, SelectNearest
│           └── filter.go          # filterHealthyNodes, hasResources
├── server/
│   ├── server.go                  # struct + New (slim)
│   ├── boot.go                    # NEW: Start (initialization)
│   ├── nats.go                    # NEW: connectNATS (тонкий wrapper над netutil)
│   ├── commands.go                # NEW: SendCommandToAgent, Start/StopServiceOnNode
│   ├── leadership.go              # NEW: watchLeadership, startLeaderWork, stopLeaderWork
│   ├── logbuffer.go               # NEW: startLogBuffering
│   ├── metrics.go                 # NEW: subscribeGatewayMetrics
│   ├── deployment.go              # NEW: DeployService
│   ├── allocindex.go              # NEW: бывший struct allocIndex
│   ├── snapshot.go                # buildSnapshot + helpers (slim)
│   ├── streamhub/
│   │   ├── hub.go                 # struct + Run
│   │   ├── subscribe.go           # generic Subscribe[T] для snap/drain/event
│   │   └── fanout.go
│   └── dispatcher.go
├── agent/
│   ├── agent.go                   # struct + New + Start (slim)
│   ├── services.go                # NEW: StartService, StopService, stopAll
│   ├── nodeinfo.go                # NEW: getNodeInfo (uses core/netutil)
│   ├── commands.go                # без изменений
│   ├── heartbeat.go               # NEW: publishHeartbeat, publishProcessMetrics
│   ├── restart.go                 # NEW: monitorProcesses → реактивная подписка
│   ├── logstream.go               # NEW: streamProcessLogs (без 500ms ticker)
│   └── sysinfo_*.go
└── testutil/
```

## Phase 6.1 — Извлечение общих утилит (без изменения структуры)

**Цель**: Удалить точечные дубликаты до того, как двигать файлы. Это уменьшит размер каждого «толстого» файла на 30–80 строк, и Phase 6.2 пройдёт легче.

### 6.1.1 `core/netutil/`
- [ ] Создать `core/netutil/nats.go` с `Connect(cfg, name string) (*nats.Conn, error)` — единый код для `agent.connectNATS` и `server.connectNATS` (сейчас идентичные блоки).
- [ ] Создать `core/netutil/host.go` с `Hostname()` (был `generateNodeID` в двух местах) и `LocalIPv4(natsHost string)` (был `getNodeIP` в двух местах).
- [ ] Удалить `agent.connectNATS`, `agent.generateNodeID`, `agent.getNodeIP`, аналоги в `server`.

### 6.1.2 Замена самописных утилит на stdlib
- [ ] `agent.splitLines` → `bufio.NewScanner` или `strings.Split + tail`.
- [ ] `server.splitSubject` → `strings.Split(subject, ".")`.
- [ ] `proximity.removeFromSlice` → `slices.DeleteFunc` (Go 1.21+) или удалить вместе с рекурсией (см. 6.1.5).
- [ ] `proximity.SortDatacentersByProximity` — заменить bubble sort (строки 139–145) на `sort.Slice`.
- [ ] `state.ListNodes` / `state.ListAllocations` / `state.ListAllAllocations` — заменить ручную проверку префикса `key[:len(prefix)] != prefix` на `strings.HasPrefix`.

### 6.1.3 Удаление дубликата `datacenterCountsByOccupied`
- [ ] Сейчас функция определена дважды: `features/scheduling/helpers.go` и `features/autoscaling/autoscaler.go`. Оставить только в `helpers.go` как экспортируемую `DatacenterCountsByOccupied`, удалить вторую копию.

### 6.1.4 Удаление обёртки `FilterHealthyNodes`
- [ ] `Scheduler.FilterHealthyNodes` (экспорт) ↔ `filterHealthyNodes` (private) — оставить одну экспортируемую версию, остальные вызовы переписать.

### 6.1.5 Generic KV watcher в `state/`
- [ ] `WatchNodes`, `WatchNodesInit`, `WatchAllocations`, `WatchAllocationsInit` — четыре копии одного цикла. Свести к одному private:
  ```go
  func watchKV[T any](ctx, bucket, pattern,
      decode func(entry) *T,
      onChange func(*T),
      onReady func(), // optional
  ) error
  ```
  и сделать существующие 4 функции тонкими обёртками. Целевая длина `watch.go`: ≤ 100 строк (было 189).

### 6.1.6 KV bucket startup retry
- [ ] Цикл `for attempt := 0; attempt < 30; attempt++ { ... time.Sleep(1 * time.Second) }` присутствует и в `state.New`, и в `leader.NewElection`. Вынести в `core/netutil/kv.go`:
  ```go
  func WaitBucketReady(js, name string, opts) (nats.KeyValue, error)
  ```
  Использовать обоих местах.

### 6.1.7 Унификация HTTP-handler boilerplate
- [ ] Каждый handler начинается с `if r.Method != http.MethodGet { 405 }`. Создать `api/method.go`:
  ```go
  func methodGuard(w, r, methods ...string) bool
  // или использовать method-aware mux: mux.Handle("GET /api/...", h)  — Go 1.22+
  ```
  Применить ко всем handler'ам (примерно 15 мест).

### 6.1.8 Парсер path-сегментов
- [ ] Сейчас `nodes.go`, `services.go`, `allocations.go` имеют идентичные блоки:
  ```go
  for i, ch := range path {
      if ch == '/' { nodeID = path[:i]; action = path[i+1:]; break }
  }
  ```
  Вынести в `api/pathparse.go`:
  ```go
  func splitIDAndAction(path string) (id, action string)  // strings.Cut
  ```

### 6.1.9 Хелпер «здоровая нода»
- [ ] `node.Status == "ready" && time.Since(node.LastSeen) < 2*time.Minute` повторяется в `server/snapshot.go`, `api/status.go`, `api/metrics.go`. Вынести в `core/types/node.go`:
  ```go
  func (n *NodeInfo) IsHealthy(at time.Time) bool
  ```

### 6.1.10 Хелпер «cooldown активен»
- [ ] Логика «не нулевой `LastScaleUp/Down` + не истёк cooldown» повторяется в `server/snapshot.go` и `api/autoscaler.go`. Вынести метод в `core/types`:
  ```go
  func (c ServiceCooldown) IsActive(now time.Time, up, down time.Duration) (upActive, downActive bool, lastAction string, lastAt int64)
  ```

### 6.1.11 Хелпер «подсчёт аллокаций по нодам»
- [ ] Цикл «для каждой ноды посчитать planned/running по всем сервисам» в `api/nodes.go` встречается дважды (в `handleNodes` и `handleNodesWithID`). И уже есть `Scheduler.ComputeNodeAllocCounts` в `scheduling/helpers.go`. Объединить.

### 6.1.12 Унификация `mustJSON`
- [ ] Определена в `server/snapshot.go` и в `api/api.go`. Оставить одну — например, в `core/types/json.go`.

### 6.1.13 Объединение Start/StopServiceOnNode и sendStartCommand
- [ ] `server.sendStartCommand` (lifecycle.go:119), `server.StartServiceOnNode` (lifecycle.go:155) — почти полные дубликаты. `deployer.sendUpdateCommand` тоже делает то же самое. Свести к одной функции в новом `server/commands.go`.

### Проверка 6.1
- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./...` — ALL PASS
- [ ] Все «толстые» файлы потеряли по 20–80 строк за счёт удалённого дублирования.

**Время**: 6 часов
**Риск**: Низкий — изменения чисто механические, тесты в `state/`, `scheduler/`, `proximity/` ловят регрессии.

## Phase 6.2 — Декомпозиция файлов > 200 строк

**Принцип**: после Phase 6.1 многие файлы уже подсохнут. Оставшиеся > 200 строк делятся по двум сценариям:

- **Сценарий A**: файл — единственный в feature-папке (`features/draining/manager.go`, `features/deployment/deployer.go`, и т.п.). Создаётся подпапка с именем файла → файл становится sub-package.
- **Сценарий B**: файл уже внутри sub-package (`features/clustering/controller/controller.go`, `agent/agent.go`). Просто добавляются файлы в ту же папку.

### 6.2.1 `features/deployment/deployer.go` (419 → ≤ 200, сценарий A)

Разбить на `features/deployment/deployer/`:
- [ ] `deployer.go` — `Deployer struct`, `NewDeployer`, `Deploy(plan)` (orchestration: canary → rolling → finalize). Цель: ≤ 150 строк.
- [ ] `canary.go` — `deployCanary` + перевод на `WatchAllocations` вместо 5-секундного ticker (см. Phase 6.3).
- [ ] `rolling.go` — `rollingUpdate` + батчинг.
- [ ] `wait.go` — `waitForBatchHealth`, `checkAllocationsHealth` (event-driven).
- [ ] `history.go` — `addRecord`, `updateLastRecord`, `GetHistory` + типы `DeploymentRecord`, `DeploymentPlan`, `UpdateStrategy`, `DeploymentStatus` (если стоит вынести в `core/types`).
- [ ] Удалить `GetDeploymentStatus` (возвращает «not implemented», см. Appendix C) или реализовать в `history.go`.

### 6.2.2 `features/draining/manager.go` (416 → ≤ 200, сценарий A)

Разбить на `features/draining/manager/`:
- [ ] `manager.go` — `DrainManager`, `DrainDeps`, `Start/Resume/GetStatus`. Цель: ≤ 150 строк.
- [ ] `run.go` — `runDrain` (главный orchestrator); разделение system/regular allocs.
- [ ] `system.go` — `dismantleAndConfirm`.
- [ ] `migrate.go` — `placeReplacement`, `finalizeMigration`.
- [ ] `wait.go` — `waitForStopped`, `waitForHealthyOnNode`, `waitForHealthyReplacement`, `publishDrainEvent`. Здесь же — приведение `waitForHealthyReplacement` к event-driven (см. Phase 6.3).

### 6.2.3 `features/autoscaling/autoscaler.go` (344 → ≤ 200, сценарий A)

Разбить на `features/autoscaling/autoscaler/`:
- [ ] `autoscaler.go` — `Autoscaler struct`, `NewAutoscaler`, `EvaluateService`, `ExecuteScalingDecision`. Цель: ≤ 120 строк.
- [ ] `scale_up.go` — `evaluateScaleUp`, `findNodeWithTrafficWithoutService`, `findOverloadedAlloc`, `pickFreeNode`.
- [ ] `scale_down.go` — `evaluateScaleDown`, `pickAllocationToRemove`.
- [ ] `execute.go` — `executeScaleUp`, `executeScaleDown` (запись cooldown, событий).
- [ ] `cooldown.go` — `inCooldown`, `lastActionAt`, `hasGatewayTraffic`.

### 6.2.4 `features/scheduling/scheduler.go` (320 → ≤ 200, сценарий A)

Разбить на `features/scheduling/scheduler/`:
- [ ] `scheduler.go` — `Scheduler struct`, `NewScheduler`, `ReconcileService`. Цель: ≤ 100 строк.
- [ ] `system.go` — `reconcileSystem`.
- [ ] `regular.go` — `reconcileRegular`, `targetCopies`.
- [ ] `candidates.go` — `PickCandidates`, `SelectNearestForReplacement`, `SelectNodeForTrafficBasedPlacement`.
- [ ] `filter.go` — `FilterHealthyNodes` (после удаления дубликата), `hasResources`, `createAllocation`, `ComputeNodeAllocCounts`.

### 6.2.5 `features/api/stream.go` (295 → ≤ 200, сценарий A)

Разбить на `features/api/stream/`:
- [ ] `stream.go` — общий SSE-driver:
  ```go
  func runSSE[T any](w, r, source func() (<-chan T, func()), emit func(T))
  ```
  + helpers: `sseEvent`, `sseSetup`, `keepalive`. Цель: ≤ 120 строк.
- [ ] `cluster.go` — `handleStream` (cluster-wide).
- [ ] `node.go` — `handleStreamNode`.
- [ ] `service.go` — `handleStreamService`.
- [ ] `allocation.go` — `handleStreamAllocation`.

После выноса каждый handler укоротится с 50–80 до 20–30 строк.

### 6.2.6 `features/api/logs.go` (289 → ≤ 200, сценарий A)

Разбить на `features/api/logs/`:
- [ ] `logs.go` — общий стриминг (история из буфера + NATS-подписка), `formatLogEntry`. Цель: ≤ 120 строк.
- [ ] `cluster.go` — `handleLogsCluster`.
- [ ] `node.go` — `handleLogsNode`.
- [ ] `allocation.go` — `handleLogsAllocation`.

Все три handler'а сейчас почти идентичны — после общего helper'а каждый сократится до ~40 строк.

### 6.2.7 `features/clustering/leader/election.go` (290 → ≤ 200, сценарий B)

Разбить внутри `features/clustering/leader/`:
- [ ] `election.go` — `Election struct`, `NewElection`, `IsLeader`, `GetLeader`. Цель: ≤ 100 строк.
- [ ] `campaign.go` — `CampaignForLeader`, `tryBecomeLeader`, `claimLeadership`, `refreshLeadership`, `stepDown`.
- [ ] `watch.go` — `WatchLeadership`, `WaitForLeader` (после event-driven фикса в Phase 6.3).

### 6.2.8 `features/clustering/controller/controller.go` (358 → ≤ 200, сценарий B)

Разбить внутри `features/clustering/controller/`:
- [ ] `controller.go` — `ServiceController struct`, `NewServiceController`, `Run`. Цель: ≤ 120 строк.
- [ ] `reconcile.go` — `reconcile`, `dispatchPending`, `pruneFailed`, `findService`, `enqueueAllServices`.
- [ ] `watch.go` — `watchAllocsToQueue`, `watchNodesToQueue`, `periodicResync`, `runWorker`.
- [ ] `autoscale.go` — `autoscaleOnce`.

### 6.2.9 `features/scheduling/proximity/matrix.go` (263 → ≤ 200, сценарий B)

Разбить внутри `features/scheduling/proximity/`:
- [ ] `matrix.go` — `Matrix struct`, `NewMatrix`, `SetLatency`, `GetLatency`, `LoadFromConfig`. Цель: ≤ 100 строк.
- [ ] `sort.go` — `SortDatacentersByProximity` (с `sort.Slice` вместо bubble sort), `GetNearestDatacenter`.
- [ ] `validate.go` — `ValidateLatencies`, `measureLatency`, `pingNode`, `RunValidation`.

### 6.2.10 `features/execution/process/process.go` (321 → ≤ 200, сценарий B)

Разбить внутри `features/execution/process/`:
- [ ] `process.go` — `Process struct`, `New`, `Start`, `Stop`, `Status`, `PID`, `ServiceDefinition`. Цель: ≤ 150 строк.
- [ ] `monitor.go` — `monitor` + новый callback `OnExit(func(err error))` (см. Phase 6.3).
- [ ] `logs.go` — `setupLogs`, `closeLogs`, `GetLogs`, `GetLogPath`, `TailLogs`.
- [ ] Удалить мёртвый `Process.Context()` (lines 193-201) — возвращает `context.Background()` без отмены.

### 6.2.11 `features/execution/health/checker.go` (224 → ≤ 200, сценарий B)

Разбить внутри `features/execution/health/`:
- [ ] `checker.go` — `Checker struct`, `NewChecker`, `Register`, `Unregister`, `Start`, `IsHealthy`, `HealthStatusStr`. Цель: ≤ 130 строк.
- [ ] `probe.go` — `runChecks`, `performCheck`, `recordResult`, `GetStatus`, `GetAllStatuses` (последние два — после общего хелпера копирования `Check`).

### 6.2.12 `agent/agent.go` (378 → ≤ 200, сценарий B)

Разбить внутри `agent/`:
- [ ] `agent.go` — `Agent struct`, `New`, `Start`. Цель: ≤ 130 строк.
- [ ] `services.go` — `StartService`, `StopService`, `stopAllProcesses`.
- [ ] `nodeinfo.go` — `getNodeInfo` (использует `core/netutil` после Phase 6.1).

### 6.2.13 `agent/lifecycle.go` (247 → ≤ 200, сценарий B)

Разбить внутри `agent/`:
- [ ] `heartbeat.go` — `publishHeartbeat`, `publishProcessMetrics`.
- [ ] `restart.go` — `monitorProcesses`, `checkAndRestartFailedProcesses` → переписать через callback от `Process.OnExit` (см. Phase 6.3).
- [ ] `logstream.go` — `streamProcessLogs` без 500ms ticker (использовать `Process` ctx).

### 6.2.14 `server/server.go` (276 → ≤ 200, сценарий B)

Разбить внутри `server/`:
- [ ] `server.go` — `Server struct`, `New`, поля. Цель: ≤ 80 строк.
- [ ] `boot.go` — `Start` (инициализация всех зависимостей; ~150 строк, но это линейная подпрограмма с понятными секциями).
- [ ] `leadership.go` — `startLeaderWork`, `stopLeaderWork`, `addClusterEvent`.

### 6.2.15 `server/lifecycle.go` (372 → ≤ 200, сценарий B)

Разделить по ответственностям:
- [ ] `nats.go` — `connectNATS` (тонкий wrapper над `core/netutil.Connect`).
- [ ] `commands.go` — `SendCommandToAgent`, `StartServiceOnNode`, `StopServiceOnNode` + удалить дубликат `sendStartCommand`.
- [ ] `deployment.go` — `DeployService` + helper `parseUpdateDuration` (можно перенести в `core/types`).
- [ ] `leadership.go` — `watchLeadership`, `watchClusterNodes`.
- [ ] `logbuffer.go` — `startLogBuffering`.
- [ ] `metrics.go` — `subscribeGatewayMetrics`.
- [ ] `context.go` — все `ServerContext`-getter'ы (`ClusterState`, `Services`, и т.п.) + interface check (`var _ apiPkg.ServerContext = …`).

### 6.2.16 `server/streamhub.go` (283 → ≤ 200, сценарий B)

Разбить внутри `server/streamhub/`:
- [ ] `hub.go` — `streamHub struct`, `newStreamHub`, `Run`, `refresh`, `Snapshot`. Цель: ≤ 150 строк.
- [ ] `subscribe.go` — `Subscribe`, `SubscribeDrain`, `SubscribeEvents` через generic helper:
  ```go
  type subscribers[T any] struct { ... }
  func (s *subscribers[T]) Subscribe(buf int) (<-chan T, func())
  func (s *subscribers[T]) Fanout(v T)
  ```
  Это убирает 3 копии mutex+map+nextID.
- [ ] `fanout.go` — `fanout`, `fanoutDrain`, `FanoutEvent` поверх `subscribers[T]`.

### 6.2.17 `server/snapshot.go` (225 → ≤ 200, сценарий B)

Разбить внутри `server/`:
- [ ] `allocindex.go` — `allocIndex` struct + методы (`onNode`, `onAlloc`, `hasNode`, `snapshot`).
- [ ] `snapshot.go` — только `buildSnapshot` + helper'ы (`buildClusterStatus`, `buildServicesUsage`). Цель: ≤ 130 строк.

### Проверка 6.2
- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./...` — ALL PASS
- [ ] `go test -race ./...` — PASS
- [ ] `find … -name '*.go' | xargs wc -l | awk '$1 > 200'` — пусто (кроме сгенерированного и тест-фикстур)

**Время**: 12 часов
**Риск**: Средний — пути импортов меняются для подпакетов (`autoscaling/autoscaler`, `deployment/deployer`, и т.п.). Модулю обновить будет нужно только `server/` и `cmd/asty/main.go`.

## Phase 6.3 — Event-driven вместо polling

**Цель**: убрать polling-таймеры там, где у нас уже есть события (`KV.Watch`, callback от `Process`). Оставить таймеры только где они оправданы (TTL refresh, периодический safety-net resync).

### 6.3.1 `Deployer.deployCanary` и `waitForBatchHealth` (deployer.go)
Сейчас обе функции тикают каждые 5 секунд и читают `clusterState.GetAllocation` для каждой канарейки/батча.

- [ ] Заменить на `WatchAllocations` (или per-key `WatchAllocation`) с фильтром `status == "running"`. Считать суммарный «healthy time» по фактическим переходам, а не по тиксам.
- [ ] Сократит время реакции с 5 с до < 100 мс и снимет N запросов/тик.

### 6.3.2 `DrainManager.waitForHealthyReplacement` (manager.go:382)
Использует 200ms ticker для `ListAllocations`. Соседние `waitForStopped` и `waitForHealthyOnNode` уже event-driven через `WatchAllocation`.

- [ ] Привести к тому же паттерну: подписаться на `WatchAllocations(svc)` и в callback'е выйти как только появится running на любой ноде ≠ drainedNode.

### 6.3.3 `Agent.monitorProcesses` (agent/lifecycle.go:76)
Сейчас 5s ticker сканирует `processes` и ищет `Status == StatusFailed`. `Process.monitor` (process.go:203) уже знает момент выхода.

- [ ] Добавить в `Process` callback `OnExit func(err error)` и регистрировать его при `New`.
- [ ] `Agent.StartService` вешает callback, который кладёт сервис в канал `failedProcesses chan string`.
- [ ] `Agent.checkAndRestartFailedProcesses` слушает канал вместо тикера.
- [ ] Удаляется goroutine `monitorProcesses` целиком.

### 6.3.4 `Agent.streamProcessLogs` 500ms-проверка (agent/lifecycle.go:201)
Сейчас тикер каждые 500мс проверяет «жив ли процесс».

- [ ] Передать в `streamProcessLogs` уже отменяемый ctx (производный от ctx процесса) — `Process` его кэнсельнёт сам.
- [ ] Удалить ticker.

### 6.3.5 `Election.WaitForLeader` (election.go:228)
Сейчас 200ms ticker дёргает `GetLeader`.

- [ ] Использовать существующий `bucket.Watch("current-leader")` и завершиться при первом не-nil entry. Падает до < 1 мс реакции.

### 6.3.6 `streamHub.Run` — двойной триггер (streamhub.go:139)
Сейчас одновременно работают: 5-секундный `ticker` И debounced (500мс) trigger от watcher'ов.

- [ ] Оставить только debounce (события надёжно доставляются NATS Watch).
- [ ] Если нужен safety-net на случай пропущенных событий — поднять период до 60 с (а не 5 с) и пометить таким комментарием.

### 6.3.7 KV-bucket startup retries (`state.New`, `leader.NewElection`)
30 итераций по `time.Sleep(1 * time.Second)`.

- [ ] Заменить на ограниченный `backoff.Retry` (либо ручной backoff: 100мс, 200мс, …, до 5с total) — общий хелпер `core/netutil/kv.go::WaitBucketReady`. Не-критично, но уменьшит время старта в типовом случае с ~2с до ~100мс.

### Что оставляем как polling (намеренно)
| Где | Период | Почему оставляем |
|---|---:|---|
| `Election.CampaignForLeader` refresh | 5 с | TTL=10с, нужен периодический put (физика TTL-based leader election) |
| `Controller.periodicResync` | 60 с | Safety-net корректировки drift, не блокирующий путь |
| `Agent.publishHeartbeat` | 5 с | Подтверждение «нода жива» — обязательно периодическое |
| `Agent.publishProcessMetrics` | 10 с | Семплинг CPU/Memory — physical sampling |
| `MetricsCollector.Start` | EvalInterval | Sampling /proc для CPU% |
| `HealthChecker.Start` | 1 с | HTTP-пробы — внешние, нужны периодические запросы |
| `Process.TailLogs` | 100 мс | Файловое чтение; fsnotify усложняет ротацию — оставить, документировать |

### Проверка 6.3
- [ ] `go build ./... && go test -race ./...` — PASS
- [ ] Ручное наблюдение в DevMode: время реакции «канарейка → running → start rolling» < 1 с (раньше до 5 с).

**Время**: 5 часов
**Риск**: Средний — событийная модель чувствительнее к гонкам. Каждое изменение покрывать тестом.

## Phase 6.4 — Чистка и читаемость для не-разработчиков

**Цель**: убрать stub'ы и magic numbers, ввести типизированные статусы, сделать имена однозначными.

### 6.4.1 Удалить stub-эндпоинты
Все возвращают «not yet fully implemented» — это вводит пользователей в заблуждение. Решение: либо реализовать, либо вернуть HTTP 501.

- [ ] `api/services.go::handleServicesWithActions`: `action == "scale"` (lines 50-65) — либо реализовать через `ClusterState` + Scheduler, либо HTTP 501.
- [ ] `api/nodes.go::handleNodesWithID`: `action == "pause"` (lines 88-97) — HTTP 501 + лог.
- [ ] `api/allocations.go::handleAllocationWithID`: `action == "restart"` и `action == "stop"` (lines 88-101) — HTTP 501 + лог.
- [ ] `deployer.GetDeploymentStatus` (deployer.go:377) — реализовать поверх `history` либо удалить (никем не зовётся).
- [ ] `Process.Context()` (process.go:193) — возвращает `context.Background()`, мёртвая функция. Удалить.

### 6.4.2 Типизированные статусы
Сейчас «running», «pending», «starting», «failed», «stopped», «deleted», «ready», «draining», «drained», «down» — везде string-литералы. Опечатка в одном месте — silently broken behavior.

- [ ] В `core/types/allocation.go` ввести:
  ```go
  type AllocationStatus string
  const (
      AllocPending  AllocationStatus = "pending"
      AllocStarting                  = "starting"
      AllocRunning                   = "running"
      AllocStopped                   = "stopped"
      AllocFailed                    = "failed"
      AllocDeleted                   = "deleted"
  )
  ```
- [ ] В `core/types/node.go`:
  ```go
  type NodeStatus string
  const (
      NodeReady    NodeStatus = "ready"
      NodeDraining            = "draining"
      NodeDrained             = "drained"
      NodeDown                = "down"
  )
  ```
- [ ] Заменить все строковые сравнения. Оставить wire format = lowercase JSON для совместимости.

### 6.4.3 Magic numbers → именованные константы
- [ ] `streamHub` interval `5*time.Second` → `streamHubRefreshInterval`.
- [ ] `streamHub` debounce `500*time.Millisecond` → `streamHubDebounce`.
- [ ] Канарейка `5 * time.Second` poll → `canaryHealthCheckInterval` (если оставим polling) или удалить (после 6.3.1).
- [ ] `30 * time.Second` для `nc.Request` в start command → `agentStartCommandTimeout`.
- [ ] `5 * time.Second` для stop command → `agentStopCommandTimeout`.
- [ ] `2 * time.Minute` heartbeat staleness → `nodeStaleAfter` (уже есть в `scheduler/`, унифицировать).

### 6.4.4 Парсинг durations при загрузке конфига
Сейчас `ServiceDefinition.GetKillTimeout()`, `GetInterval()`, `GetTimeout()`, `GetAttempts()`, `GetDelay()` — все парсят строку при каждом вызове.

- [ ] При загрузке `.asty` файла (deployer/loader.go) парсить один раз, складывать в типизированные `time.Duration` поля.
- [ ] Существующие методы оставить как геттеры с дефолтами — без вызова `time.ParseDuration`.

### 6.4.5 Документировать «почему» рядом с константами
Не-разработчик должен понимать смысл цифры. Пример:
```go
// streamHubRefreshInterval — fallback period when the KV watch debouncer
// receives no events. Picked at 60s because reactive updates handle the hot
// path; this only catches missed events on watcher reconnects.
const streamHubRefreshInterval = 60 * time.Second
```
- [ ] Добавить такие комментарии для всех timing-констант (~10 мест).

### 6.4.6 Удалить мёртвую обёртку `serverDispatcher`
- [ ] `server/dispatcher.go` оборачивает `*Server` в struct, чтобы реализовать `controller.CommandDispatcher` — но это интерфейс с одним методом. Заменить на функциональный тип:
  ```go
  type CommandDispatcher func(nodeID string, svc *types.ServiceDefinition) error
  ```
  Тогда controller получает значение `s.sendStartCommand` напрямую, без оборачивающего struct.

### 6.4.7 Унификация `UpdateStrategy`/`DeploymentPlan`
Сейчас `MaxParallel`, `MinHealthyTime`, `HealthyDeadline`, `AutoRevert`, `Canary` дублируются на двух уровнях `DeploymentPlan` и его поля `UpdateStrategy`.

- [ ] Оставить только `Plan.UpdateStrategy`. Использовать `plan.UpdateStrategy.MaxParallel`. Удалить дубликаты на верхнем уровне.

### 6.4.8 Понятные имена
- [ ] `streamHub.refresh` — переименовать в `rebuild` или `recomputeSnapshot` (объясняет, что считается тяжёлый snapshot).
- [ ] `splitLines` — на удаление в Phase 6.1; все вызовы → `bufio.Scanner`.
- [ ] `Process.Context()` — на удаление в 6.4.1.
- [ ] `Process.GetLogs(lines int)` — параметр игнорируется (читает весь файл). Переименовать в `ReadLogFile()` либо реализовать `tail`.

### 6.4.9 Документация на каждый sub-package
- [ ] В каждой новой подпапке (после Phase 6.2) — `doc.go`:
  ```go
  // Package canary handles deploying N canary instances and waiting for
  // them to be healthy for MinHealthyTime before promoting to a full rollout.
  package canary
  ```
  ~ 5–10 строк русско-английского пояснения. Поможет вновь пришедшим.

### Проверка 6.4
- [ ] `go vet ./...` — без ворнингов.
- [ ] Ни в одном файле нет литералов `"running"`, `"pending"`, `"ready"` — только enum'ы.
- [ ] `git grep "not yet" internal/platform/asty/` — пусто.

**Время**: 4 часа
**Риск**: Низкий — типизированные константы ловятся компилятором.

## Phase 6.5 — Финальная проверка

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./...` — ALL PASS
- [ ] `go test -race -count=1 ./...` — PASS
- [ ] `go vet ./...` — нет варнингов
- [ ] Метрики (см. ниже) — все цели достигнуты
- [ ] Обновить `CLAUDE.md`: новые подпапки `deployer/`, `manager/`, `autoscaler/`, `scheduler/`, `stream/`, `logs/`, `streamhub/`
- [ ] Обновить `dev-docs/architecture.md` (диаграмма зависимостей с подпакетами)
- [ ] Один коммит на phase: `phase 6.1 dedup`, `phase 6.2 decompose`, `phase 6.3 event-driven`, `phase 6.4 cleanup`

**Время**: 1 час
**Риск**: Низкий

## Метрики успеха

| Метрика | До | Цель | Измерение |
|---|---:|---:|---|
| Файлов > 200 строк (без `_test.go`) | 17 | 0 | `find … -name '*.go' \! -name '*_test.go' \| xargs wc -l \| awk '$1 > 200'` |
| Файлов > 400 строк | 2 | 0 | то же, `$1 > 400` |
| Дубликатов утилит (см. Appendix A) | 12 | 0 | grep по сигнатурам |
| Polling-тикеров на бизнес-события (Appendix B) | 7 | 0 | `git grep "time.NewTicker"` минус разрешённый список |
| Stub-эндпоинтов «not yet implemented» | 4 | 0 | `git grep "not yet"` |
| Самописных утилит вместо stdlib | 5 | 0 | code review |
| Чистый `go vet ./...` | — | без ворнингов | — |
| Время реакции на «канарейка healthy» | до 5 с | < 1 с | dev-mode наблюдение |

## Стратегия отката

Каждая Phase 6.x — отдельный коммит. Если после Phase 6.x не проходит `go build` или race-тесты:

1. `git revert <commit>` (не reset) — сохраняем историю.
2. Разбить проблемную Phase на меньшие шаги (по одному файлу).
3. После каждого шага: `go build ./... && go test -race ./...`.

## Appendix A: Каталог дубликатов

| # | Что | Где (1) | Где (2) | Решение |
|---:|---|---|---|---|
| 1 | `connectNATS` | `agent/agent.go:297` | `server/lifecycle.go:95` | `core/netutil/nats.go::Connect` |
| 2 | `generateNodeID` | `agent/agent.go:318` | `server/lifecycle.go:335` | `core/netutil/host.go::Hostname` |
| 3 | `getNodeIP` | `agent/agent.go:326` | `server/lifecycle.go:344` | `core/netutil/host.go::LocalIPv4` |
| 4 | `mustJSON` | `server/snapshot.go:218` | `features/api/api.go:101` | `core/types/json.go::MustJSON` |
| 5 | `datacenterCountsByOccupied` | `features/scheduling/helpers.go:56` | `features/autoscaling/autoscaler.go:335` | один в `helpers.go` |
| 6 | `WatchNodes` vs `WatchNodesInit` | `state/watch.go:16` | `state/watch.go:84` | `state.watchKV[T]` generic |
| 7 | `WatchAllocations` vs `WatchAllocationsInit` | `state/watch.go:49` | `state/watch.go:119` | `state.watchKV[T]` generic |
| 8 | KV bucket startup retry (30×1с) | `state/state.go:29` | `leader/election.go:37` | `core/netutil/kv.go::WaitBucketReady` |
| 9 | `ListAllocations` vs `ListAllAllocations` | `state/allocations.go:56` | `state/allocations.go:92` | общий цикл с фильтром-параметром |
| 10 | `Scheduler.FilterHealthyNodes` vs `filterHealthyNodes` | `scheduler.go:229` | `scheduler.go:233` | оставить только экспортируемую |
| 11 | URL path-id-action парсинг | `api/nodes.go:60-67` | `api/services.go:32-39`, `api/allocations.go:69-76` | `api/pathparse.go::splitIDAndAction` (`strings.Cut`) |
| 12 | Node-allocations counting loop | `api/nodes.go:23-44` | `api/nodes.go:116-135`, `server/snapshot.go:111-117` | reuse `Scheduler.ComputeNodeAllocCounts` или вынести в `core/types` |
| 13 | `sendStartCommand` vs `StartServiceOnNode` | `server/lifecycle.go:119` | `server/lifecycle.go:155` | оставить один |
| 14 | `sendUpdateCommand` (deployer) vs `SendCommandToAgent` | `deployer.go:328` | `server/lifecycle.go:138` | использовать `server.SendCommandToAgent` |
| 15 | SSE handler skeleton (`sseSetup` + ping ticker + select-loop) | `api/stream.go` (×4) | `api/logs.go` (×3) | `api/stream/runSSE` generic |
| 16 | Cooldown-active mapping | `server/snapshot.go:170-187` | `api/autoscaler.go:54-71` | метод на `types.ServiceCooldown` |
| 17 | StreamHub subscribers (snap/drain/event) | `streamhub.go:189`, `213`, `233` | (сами себе ×3) | generic `subscribers[T]` |
| 18 | Metrics → Check копирование | `health/checker.go:185-203`, `205-224` | (×2) | helper `cloneCheck(*Check) *Check` |
| 19 | Duration parsing per call | `core/types/service.go:67-108` (5×) | (5 геттеров) | парсить при загрузке `.asty` |

## Appendix B: Каталог таймеров (polling)

| # | Где | Период | Заменить? | Чем |
|---:|---|---:|:---:|---|
| 1 | `Deployer.deployCanary` | 5 с | да | `WatchAllocations` |
| 2 | `Deployer.waitForBatchHealth` | 5 с | да | `WatchAllocations` |
| 3 | `DrainManager.waitForHealthyReplacement` | 200 мс | да | `WatchAllocations` (как соседи) |
| 4 | `Agent.monitorProcesses` | 5 с | да | callback `Process.OnExit` |
| 5 | `Agent.streamProcessLogs` (внутр. ticker) | 500 мс | да | производный ctx от `Process` |
| 6 | `Election.WaitForLeader` | 200 мс | да | `bucket.Watch("current-leader")` |
| 7 | `streamHub.Run` основной ticker | 5 с | да | оставить только debounce, или 60 с safety-net |
| 8 | KV bucket startup retry | 30×1 с | да | exponential backoff |
| 9 | `Election.CampaignForLeader` refresh | 5 с | **нет** | TTL physics |
| 10 | `Controller.periodicResync` | 60 с | **нет** | drift safety-net |
| 11 | `Agent.publishHeartbeat` | 5 с | **нет** | proof-of-life |
| 12 | `Agent.publishProcessMetrics` | 10 с | **нет** | sampling |
| 13 | `MetricsCollector.Start` | EvalInterval | **нет** | sampling /proc |
| 14 | `HealthChecker.Start` | 1 с | **нет** | внешние HTTP-пробы |
| 15 | `Process.TailLogs` | 100 мс | **нет** | file rotation > fsnotify complexity |
| 16 | `proximity.RunValidation` | 1 час | **нет** | heavy job |

## Appendix C: Stub-эндпоинты и мёртвый код

| Где | Что | Что делать |
|---|---|---|
| `api/services.go:50-65` | `POST /api/v1/services/:name/scale` → «scaling not yet implemented» | реализовать через увеличение targetCopies + создание pending allocation, **или** вернуть `501 Not Implemented` |
| `api/nodes.go:88-97` | `POST /api/v1/nodes/:id/pause` → «pause initiated (not yet fully implemented)» | вернуть `501` (не пишем в state, не делаем drain) |
| `api/allocations.go:88-101` | `POST /api/v1/allocations/:id/restart` и `/stop` | реализовать через NATS-команду к агенту, или `501` |
| `deployment/deployer.go:377` | `Deployer.GetDeploymentStatus` → «not implemented» | реализовать поверх `history` (last record по сервису), удалить если не зовётся |
| `execution/process/process.go:193-201` | `Process.Context()` возвращает `context.Background()` | удалить — функция нерабочая, никем не используется |
| `core/types/service.go:67-108` | 5 геттеров парсят строки на каждом вызове | парсить при загрузке `.asty` |

## Appendix D: Самописные утилиты вместо stdlib

| Где | Что | Заменить на |
|---|---|---|
| `agent/agent.go:350` | `splitLines` — char-by-char итерация | `bufio.Scanner` или `strings.Split + tail` |
| `server/lifecycle.go:321` | `splitSubject` — char-by-char итерация | `strings.Split(s, ".")` |
| `proximity/matrix.go:230` | `removeFromSlice` | `slices.DeleteFunc` (Go 1.21+) |
| `proximity/matrix.go:139-145` | bubble sort | `sort.Slice` |
| `state/state.go:74-90` | `keySuffix`, `splitAllocKey` | `strings.TrimPrefix`, `strings.Cut` |
| `state/allocations.go:69`, `state/nodes.go:76` | `key[:len(prefix)] != prefix` | `strings.HasPrefix` |

## Appendix E: Связь с предыдущим рефакторингом

`asty-refactoring.md` (Phases 1–5) реализовала Feature-Based архитектуру: вертикальные слайсы по фичам, разделение `server/` ↔ `agent/` ↔ `features/api/`, разрыв циклов через интерфейсы (`ServerContext`, `DrainDeps`, `CommandDispatcher`).

Этот документ (Phase 6) — следующий шаг. Архитектура остаётся ровно той же; меняется качество внутри:
- размер файлов 150–200 строк вместо 200–419,
- ноль дублирующих утилит,
- event-driven там, где есть события,
- понятные имена и константы для не-разработчиков.

После Phase 6 типичная задача «найти и понять код» сокращается:
- любой файл читается за один присест,
- любая константа объясняет, почему она именно такая,
- любой статус — типизирован, опечатка ловится компилятором,
- любая ожидающая операция — реактивна, не нужно гадать о таймерах.
