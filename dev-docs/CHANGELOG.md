# Changelog

## 2026-05-06 - Controller-runtime рефакторинг

### Архитектура

Заменён трёх-тикер leader (runScheduler/30s + watchAllocations/15s + Autoscaler.Run/10s) на один control-plane по образу `k8s.io/client-go/util/workqueue`. См. `controller-architecture.md` для деталей.

### Added

- `internal/platform/asty/workqueue.go` — типизированный workqueue с дедупом, processing-tracking, AddAfter (heap-based delayed scheduler), AddRateLimited (exponential backoff), Forget, ShutDown
- `internal/platform/asty/workqueue_test.go` — тесты инвариантов (FIFO, dedup, Add-during-processing, AddAfter, backoff doubling, Forget, shutdown unblocks Get)
- `internal/platform/asty/controller.go` — `ServiceController` с alloc/node KV watchers, периодическим resync, N parallel workers
- `state.go`: `MutateAllocation` (CAS-guarded read-modify-write), `WatchAllocations`, `ServiceCooldown` + `MarkScaleUp/MarkScaleDown` (cooldown в KV переживает leader flip)
- `A_CONTROLLER_WORKERS` env (default 2)

### Changed

- Все обновления allocations переведены на `MutateAllocation` — agent (StartService, publishProcessMetrics, checkAndRestartFailedProcesses), server (dispatchPending), deployer (canary, rolling update). Никаких last-write-wins гонок между leader/agent/deployer.
- Cooldown autoscaler'а перенесён из in-memory map в KV (`service.<name>.cooldown`)
- `Scheduler.ReconcileService` идемпотентен: top-up до MinCopies, не удаляет copies сверх. Стабильный tiebreak в `pickCandidates` (DC count, free memory, node ID) убрал ложные рестарты при одинаковых нодах.
- `Autoscaler.evaluateScaleDown` использует реальные `alloc.CPUUsage/MemoryUsage` (CAS-safe источник) с гистерезисом `Target/2`. Раньше `allBelowTarget := true` был хардкод-стаб.
- `Autoscaler.evaluateScaleUp` при overload теперь place'ит на **другой** свободный узел (через `Scheduler.pickCandidates`), не overwrite'ит существующую аллокацию на перегруженном узле.
- `pruneFailed` читает порог из `svc.Restart.GetAttempts()` (раньше — хардкод `>= 3`)
- `subscribeGatewayMetrics` пишет только в `node.<id>.rps`. Кластерный rollup делает `MetricsStore.collectMetrics` (сумма последних значений per-node).
- `dispatchPending`: pending → starting CAS-guarded **до** отправки команды. На ошибке dispatch — rollback CAS + `AddRateLimited` для backoff. Stuck-`starting` (>90s) flip в pending.
- Leader lifecycle: `startLeaderWork` стартует controller под sub-context. На `lose-leadership` cancel → controller дрейнит workers, watchers выходят, нет утечек goroutine'ов.

### Removed

- `runLeaderLoop`, `watchAllocsForKick`, `watchNodesForKick`, `dispatchPending`, `pruneFailed`, `autoscaleOnce` из server.go (переехали в controller)
- `Autoscaler.lastScaleUp/lastScaleDown` in-memory мапы
- Старая `scheduleSystemService/scheduleRegularService/ScheduleService` в scheduler.go
- `Autoscaler.Run` метод (контроллер сам вызывает `EvaluateService`)
- Двойной push в `cluster.rps` из gateway-subscribe

### Fixed

- Гонка между leader (writes "starting" после ACK) и agent (writes "running" с PID) — leader перезатирал агента, alloc залипал в `starting`/`pid:0`. CAS + reorder.
- Бесконечная пинг-понг между scheduler и autoscaler из-за разного clamp'а MinCopies (scheduler: min=1, autoscaler: min=3).
- Leader flap утечка scheduler/autoscaler goroutine'ов (старый `ctx` не отменялся, новый запускал второй scheduler).
- Cluster.rps содержал per-gateway точки вместо cluster-total — UI показывал лужу значений.

### Latency improvements

| Action | Before | After |
|---|---|---|
| alloc create → start command | до 30s | ~ms |
| node join → gateway placement | до 30s | ~ms |
| reconcile error → retry | 15-30s (next tick) | 500ms (exponential backoff) |
| leader flap → cooldown | reset (in-memory) | preserved (KV) |

## 2026-05-04 - Initial setup

### Created
- Project structure: `cmd/asty/main.go`, `internal/platform/asty/`
- Go module with dependencies (NATS, zerolog, yaml)
- Configuration system with A_* env variables
- Service definition parser for .asty files
- Agent and Server skeletons with NATS connectivity
- Example .asty configs: gateway, xauth
- Landing page: `docs/index.html`
- Development docs in `dev-docs/`
- Build system: Makefile, .gitignore

### Updated documentation
- README.md: commercial product description (no Nomad mentions)
- dev-docs/architecture.md: technical context, Nomad mapping, process notes
- dev-docs/autoscaling.md: bot-proof autoscaling with RPS threshold
- dev-docs/configuration.md: all A_* variables, .asty format
- dev-docs/monitoring.md: metrics, UI, testing
- dev-docs/README.md: development plan, implementation phases

### Working
- ✅ Binary builds successfully
- ✅ Config loads from A_* env variables
- ✅ .asty files parse correctly (YAML)
- ✅ Agent connects to NATS and handles commands
- ✅ Process lifecycle: start/stop with graceful shutdown (SIGTERM → SIGKILL)
- ✅ Health checks: periodic HTTP probes with state tracking
- ✅ Metrics collection: CPU/Memory from /proc filesystem
- ✅ Artifact downloads: tar.gz extraction with SHA256 verification
- ✅ Agent can start/stop services, register health checks, collect metrics

## 2026-05-04 - Phase 2: Clustering

### Added
- DNS discovery (discovery.go): node discovery via A records, change detection
- Cluster state (state.go): NATS JetStream KV for node info and allocations
- Leader election (leader.go): TTL-based leader election with automatic failover
- Agent integration: heartbeat publishing, node registration
- Server integration: leader campaign, scheduler activation, node watching

### Working
- ✅ Multi-node cluster with automatic node registration
- ✅ Leader election with failover
- ✅ DNS-based node discovery
- ✅ State persistence in NATS JetStream KV
- ✅ Agent heartbeats with 5-minute TTL

## 2026-05-04 - Phase 3: Basic Scheduler

### Added
- Scheduler (scheduler.go): system/service placement, resource checking, geo-diversity
- Command protocol (commands.go): start/stop commands with JSON encoding
- Agent command handling: receives commands, starts/stops services, sends responses
- Server scheduling integration: sends commands to agents, tracks responses

### Working
- ✅ System service placement (one per node)
- ✅ Regular service placement (min 3, geo-diverse)
- ✅ Resource availability checking
- ✅ Agent↔Server command protocol (NATS request/reply)
- ✅ All scheduler tests passing

## 2026-05-04 - Phase 4: Locality-Aware Autoscaler

### Added
- Autoscaler (autoscaler.go): scaling decision engine, cooldown tracking, min copies enforcement
- Proximity matrix (proximity.go): DC latency config, nearest DC selection, hourly validation
- Scheduler locality-aware placement: proximity-based node selection
- Server autoscaler integration: runs on leader, stops on failover

### Working
- ✅ Autoscaler evaluation loop with cooldown
- ✅ Proximity matrix with config loading
- ✅ DC sorting by latency
- ✅ Scale up/down decision logic (placeholders for metrics)
- ✅ All proximity tests passing

## 2026-05-04 - Phase 5: Deployments

### Added
- Deployer (deployer.go): rolling updates, canary, auto-revert, health checks
- Service loader (loader.go): load .asty files from directory
- Server deployment integration: DeployService() API, reconciliation with loaded services
- Deployment status tracking

### Working
- ✅ Canary deployments with health validation
- ✅ Rolling updates with max_parallel batching
- ✅ Auto-revert on health check failure
- ✅ Service definitions loaded from ./services/
- ✅ Scheduler reconciles loaded services
- ✅ All deployment tests passing

## 2026-05-04 - Phase 6: Observability

### Added
- HTTP API (api.go): REST endpoints for cluster management
- Web UI (ui.go): embedded dashboard with auto-refresh
- Prometheus metrics endpoint
- API endpoints: health, nodes, services, allocations, deploy, status
- Server API integration: starts on A_UI_ADDR (127.0.0.1:4646)

### Working
- ✅ REST API with JSON responses
- ✅ Embedded Web UI dashboard
- ✅ Real-time monitoring (10s auto-refresh)
- ✅ Prometheus metrics export
- ✅ Node and service status tables
- ✅ Deployment API endpoint

### All Phases Complete! 🎉
1. ✅ Phase 1: Process management — COMPLETE
2. ✅ Phase 2: Clustering — COMPLETE
3. ✅ Phase 3: Basic scheduler — COMPLETE
4. ✅ Phase 4: Locality-aware autoscaler — COMPLETE
5. ✅ Phase 5: Deployments — COMPLETE
6. ✅ Phase 6: Observability — COMPLETE
