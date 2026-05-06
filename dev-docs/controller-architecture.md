# Controller-runtime архитектура (event-driven leader)

## Контекст

Изначальный leader был три параллельных тика — `runScheduler` 30s, `watchAllocations` 15s, `Autoscaler.Run` 10s, — гонявших независимо. Они дрались за состояние: scheduler успевал удалить alloc, который только что добавил autoscaler; watchAllocations перезаписывал агентский `running` обратно в `starting`; разное определение MinCopies в scheduler и autoscaler давало бесконечный пинг-понг.

Поэтапный рефакторинг привёл к controller-runtime подходу из Kubernetes: один control plane, который **реагирует** на события в KV, а не опрашивает.

## Слои

```
┌────────────────────────────────────────────────────────────┐
│  Server                                                     │
│   ├─ leader election (NATS KV TTL)                         │
│   ├─ on become-leader → spawn ServiceController            │
│   └─ on lose-leader   → cancel leaderCtx (controller дрейн)│
├────────────────────────────────────────────────────────────┤
│  ServiceController (controller.go)                          │
│   ├─ Workqueue (workqueue.go)                              │
│   ├─ alloc.* watcher → enqueue affected service            │
│   ├─ node.*  watcher → enqueue all services                │
│   ├─ periodic resync (60s) → enqueue all services          │
│   └─ N workers ── reconcile(svc) ── 4 phases               │
├────────────────────────────────────────────────────────────┤
│  Reconcile phases (idempotent, CAS-safe)                    │
│   1. Scheduler.ReconcileService — top up к MinCopies       │
│   2. dispatchPending — pending→starting + start command    │
│   3. pruneFailed — drop allocs с исчерпанным restart       │
│   4. autoscaleOnce — grow/shrink по метрикам (только type:service) │
└────────────────────────────────────────────────────────────┘
```

## Workqueue

Тонкая реализация семантики `k8s.io/client-go/util/workqueue` (~200 строк).

### Свойства

| Свойство | Значение |
|---|---|
| Дедупликация | `Add(k)` пока `k` уже в очереди — no-op |
| Processing-tracking | `Add(k)` пока worker держит `k` через `Get` — ставит dirty bit; `Done(k)` re-enqueue'ит |
| Backoff | `AddRateLimited(k)` — 500ms × 2^failures, capped at 60s |
| Reset failures | `Forget(k)` — после успешного reconcile |
| Delayed add | `AddAfter(k, d)` — heap-based scheduler |
| Shutdown | `ShutDown()` будит блокированные `Get`'ы |

### Инварианты

1. **Один и тот же ключ не обрабатывается двумя воркерами одновременно** — `processing` map гарантирует это.
2. **Изменения, наблюдаемые во время обработки, не теряются** — dirty bit + Done re-enqueue.
3. **Failure одного ключа не блокирует другие** — backoff применяется только к k.

### Ключевая структура данных

```go
queue      []string             // FIFO готовых
dirty      map[string]struct{}  // ждут обработки (включая marked-while-processing)
processing map[string]struct{}  // в работе сейчас
delayed    heap of (key, ready_time)  // для AddAfter / AddRateLimited
failures   map[string]int       // счётчик retries для backoff
```

## ServiceController

### Ключевание

Очередь хранит **имена сервисов**, не алло́ки. Любое событие в KV маппится на «какие сервисы могло задеть»:

| Событие | Enqueue |
|---|---|
| `alloc.<svc>.<node>` change | `svc` |
| `node.<id>` change | **все** сервисы |
| Periodic resync | **все** сервисы |
| Reconcile error | тот же `svc` через `AddRateLimited` |
| dispatch error | тот же `svc` через `AddRateLimited` |

Почему имена сервисов, а не аллокации: одна reconcile-итерация на сервис делает все 4 фазы атомарно. Если бы ключевали по аллокациям, пришлось бы координировать кросс-аллокационные решения (geo-spread, autoscaler) через ещё один уровень.

### Воркеры

`A_CONTROLLER_WORKERS` параллельных goroutine (default 2). Параллелизм безопасен:

- Per-service alloc keys (`alloc.<svc>.<node>`) не пересекаются между сервисами.
- `processing` map в workqueue гарантирует, что один сервис обрабатывается одним воркером в любой момент.
- Все мутации KV — под CAS (revision check), agent'ские update'ы метрик не теряются.

### Watchers

Watchers фильтруют шум **до** workqueue:

- **alloc.* watcher** держит локальный `map[svc/node]status`, kick'ает только на смену статуса (`pending`→`starting`, `starting`→`running`, и т.д.). Метрические update'ы (`alloc.CPUUsage`, `MemoryUsage`) не приводят к enqueue.
- **node.* watcher** держит `map[node]status`, kick'ает на join / leave / status flip. Heartbeat (`LastSeen` tick) фильтруется.

Без этого фильтра: 8 нод × 4 сервиса × 1 metric write/10s = ~3 события/с впустую.

### Resync

Periodic 60s — **safety net**. Покрывает:
- Watcher reconnect (NATS разрыв → пересоединение → возможные пропущенные события).
- Autoscaler eval — он принимает решение по метрикам (push'атся через NATS subject), а не по KV-state, так что watcher на alloc.* его не разбудит. Resync даёт регулярную каденцию.

Resync = `EvalInterval × 6`, capped at 5min. При `EvalInterval=10s` → 60s.

### Lifecycle

```
on become-leader (KV watcher на current-leader):
  leaderCtx = WithCancel(serverCtx)
  controller := NewServiceController(...)
  go controller.Run(leaderCtx)

controller.Run:
  enqueueAllServices()         // initial, чтобы не ждать первый watcher event
  go watchAllocsToQueue(ctx)
  go watchNodesToQueue(ctx)
  go periodicResync(ctx)
  for i := 0; i < workers; i++ go runWorker(ctx, i)
  <-ctx.Done()
  queue.ShutDown()
  wg.Wait()                    // ждём дрейн воркеров

on lose-leader:
  leaderCancel()  // отменяет leaderCtx
  → controller's ctx.Done() → ShutDown → workers выходят на Get()=false → Run возвращается
```

Нет утечек goroutine'ов: каждый flap leadership даёт чистый цикл.

### CommandDispatcher

Воркер должен отправлять start-command'ы агентам. Это NATS request/reply, который умеет `Server`. Чтобы избежать циркулярной зависимости (`ServiceController` ← `Server`), есть интерфейс:

```go
type CommandDispatcher interface {
    SendStartCommand(nodeID string, svc *ServiceDefinition) error
}
```

`Server` имплементит через адаптер `serverDispatcher`. Можно подменить в тестах.

## Reconcile фазы

### 1. Scheduler.ReconcileService

Идемпотентный baseline. Считает «живые» аллокации (pending/starting/running), сравнивает с `MinCopies`, добивает недостаток через `pickCandidates`. **Не удаляет** аллокации сверх `MinCopies` — это работа autoscaler'а.

`pickCandidates` использует стабильный tiebreak (DC count, free memory, node ID), что устранило главную причину ложных рестартов: на одинаковых нодах reconcile-цикл больше не выбирает разные комбинации.

### 2. dispatchPending

Обрабатывает `pending` allocations:

```
1. (unstick pass) Если alloc в "starting" > 90s — flip в "pending"
   (агент видимо упал между приёмом команды и записью running)

2. (dispatch pass) Для каждой "pending":
   a. CAS: Status pending → starting
   b. SendStartCommand(node, svc)
   c. На ошибке: CAS rollback Status starting → pending,
      AddRateLimited(svc.Name) для backoff
```

CAS-предикаты гарантируют:
- Не дублируем dispatch (если другой воркер уже flip'нул в starting → predicate=false → skip)
- Не оживляем устаревшее `pending` (если агент успел записать `running` через CAS — наш rollback predicate `Status != "starting"` → no-op)

### 3. pruneFailed

Удаляет аллокации с `Status=failed` и `ConsecutiveFailures >= svc.Restart.GetAttempts()`. После удаления следующий reconcile place'ит копию на другой узел.

### 4. autoscaleOnce (только service-type)

Cooldown читается из KV (`service.<name>.cooldown`). Если в cooldown — skip. Иначе:

- **Scale-up**: 
  - traffic case — gateway-rps на ноде без копии этого сервиса → place там
  - overload case — `alloc.CPUUsage > TargetCPU` или `MemoryUsage > TargetMemory` → place на **другом** свободном узле (через `pickCandidates`)
- **Scale-down**:
  - копий > `MinCopies`, средние CPU/Memory < `TargetCPU/2`, `TargetMemory/2` → удалить копию из самого «густого» DC (geo-diversity preserving)

После выполнения: `MarkScaleUp/MarkScaleDown` в KV — cooldown переживает leader flip.

## Сравнение со старой архитектурой

| | До (3 ticker'а) | После (controller) |
|---|---|---|
| Latency alloc create → dispatch | до 30s | ~ms (watcher) |
| Latency node join → gateway placement | до 30s | ~ms (watcher) |
| Параллелизм | 1 (последовательно по сервисам) | N workers (default 2) |
| Гонки между фазами | да (scheduler ↔ autoscaler) | нет (одна фаза-цепочка per-service) |
| Recovery от транзиентной ошибки | до следующего тика (15-30s) | exponential backoff 500ms→60s |
| Cooldown autoscaler | in-memory, теряется на flap | в KV, переживает flap |
| Утечки goroutine на leadership flip | да | нет |
| Шум от метрик | каждое write — kick тика | фильтруется на watcher уровне |

## Конфиг

| Переменная | Default | Описание |
|---|---|---|
| `A_CONTROLLER_WORKERS` | 2 | Число параллельных воркеров |
| `A_EVAL_INTERVAL` | 10s | Множитель для resync (`×6`, capped at 5m). Реактивный путь watcher'ом не зависит. |
| `A_COOLDOWN_UP` | 30s | После scale-up — пауза до следующего scaling action |
| `A_COOLDOWN_DOWN` | 5m | После scale-down — пауза |
| `A_TRAFFIC_RPS_THRESHOLD` | 5 | RPS для traffic-based scale-up |
| `A_TARGET_CPU` | 75 | CPU% для scale-up; floor для scale-down = `TargetCPU/2` |
| `A_TARGET_MEMORY` | 75 | Аналогично |

## Файлы

| Файл | Содержание |
|---|---|
| `internal/platform/asty/workqueue.go` | Workqueue + delayed heap |
| `internal/platform/asty/workqueue_test.go` | Тесты инвариантов |
| `internal/platform/asty/controller.go` | ServiceController + reconcile phases |
| `internal/platform/asty/scheduler.go` | ReconcileService, pickCandidates |
| `internal/platform/asty/autoscaler.go` | EvaluateService, ExecuteScalingDecision |
| `internal/platform/asty/state.go` | KV bucket, MutateAllocation, WatchAllocations, WatchNodes, ServiceCooldown |
| `internal/platform/asty/server.go` | Lifecycle, leader election, NATS RPC, serverDispatcher |

## TODO / возможные улучшения

- **API SSE streams** (`api.go`) всё ещё на 5s ticker'е. Их можно подписать на `WatchAllocations`/`WatchNodes` и пушить с фильтрацией без лагов.
- **ScalingEvent ring buffer** в памяти лидера — теряется при flap. Можно поднять в KV под ключом `event.<unix_ts>.<service>.<id>`.
- **Worker-перезапуск при панике**: сейчас паника в reconcile уронит воркера; нужен `defer recover` с requeue.
- **Динамика сервисов**: `services` передаётся в controller при создании. Если service-loader подхватит новый `.asty` — controller не узнает. Решается через ServiceWatcher на отдельном KV-префиксе или file-watcher.
- **Workqueue metrics** — глубина очереди, время в queue, число retries — пригодились бы в Prometheus для тюнинга.
