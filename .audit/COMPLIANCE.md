# Compliance — ветка `migration/tz` vs `.audit/TZ.md`

Аудит проводился на HEAD `migration/tz` (после `868869b`). Каждое
утверждение проверено грепом / чтением кода; ничего не принято «по
доке». Источники истины внизу каждой строки — путь до файла.

Шкала:
- ✓ — соответствует TZ.
- ⚠ — частично или с интерпретацией (поясняется).
- ❌ — расхождение, требует правки кода или TZ.

---

## §2 Принципы дизайна

| # | Принцип | Статус | Доказательство |
|---|---|---|---|
| §2.1 | NATS — единая истина в KV | ✓ | `asty-cluster` + `asty-leader` бакеты, `infra/kv/state.go:31`, `ops/leader/election.go:47` |
| §2.2 | NATS — единственный backbone | ✓ | Нет gRPC/Redis/иных транспортов; всё через `nc.Publish`/`Subscribe` |
| §2.3 | Locality first | ✓ | `ops/autoscaler/scale_up.go:findNodeWithTrafficWithoutService` приоритетнее resource-pressure |
| §2.4 | Event-driven, polling — исключения | ⚠ | Перечисленные исключения (heartbeat 5 с, resync 60 с, etc.) присутствуют. Полный список не верифицирован построчно |
| §2.5 | Эффекты только на лидере | ✓ | `api/dashboard/leaderguard.go` 307-redirect для POST; GET-роуты без guard'а; ops/leader/* запускается из `server/leadership.go` через `startLeaderWork` |
| §2.6 | Бинарный wire (CBOR) | ✓ | 15 точек `codec.Wire/State.Marshal` в `infra/kv`, `api/dashboard/audit.go`; JSON только в `ops/drainer/wait.go:146` (drain.progress) — это явно разрешено в TZ §6.2 |
| §2.7 | Три NATS-учётки изолированы | ✓ | `core/config/nats.go:116-118` — Validate отвергает `User == AppUser` |
| §2.8 | Файлы ≤200 строк, исключение workqueue | ❌ | См. отдельную секцию ниже. Сейчас 4 файла превышают |
| §2.9 | Конфиг — один путь, `os.Getenv` только в `core/config` | ✓ | `make layer-check` грепом верифицирует на каждом коммите; ручная проверка — пусто |
| §2.10 | API split: `/api/v1` data / `/metrics` Prom / `/health` | ⚠ | По букве TZ data plane на `/api/v1`. Реализация: `/dashboard/v1` (admin REST+SSE) + `/metrics` (Prom) + `/api/v1` (gateway user traffic) + `/health`. Это **сознательное изменение** после написания TZ (user redesign в `4d45918`); TZ.md ещё не обновлён под него |
| §2.11 | Состояние — формальные FSM | ✓ | Allocation/Node/Deployment FSM — typed enums в `core/types`, CAS через `kv.MutateAllocation` |
| §2.12 | Слои onion | ✓ | Гребы по `asty/internal/{core,domain,infra}/` — нет восходящих импортов; `.golangci.yml` depguard и `make layer-check` форсят |
| §2.13 | Нет полу-реализованных фич | ⚠ | `auto_revert` теперь реальный (`51105c5`); `RollbackFailed` гейтит autoscaler. Остаётся `RollbackSteps[]` в `DeploymentRecord` — упомянут в TZ §4.4, в коде НЕТ |
| §2.14 | Daily-update протокол | n/a | Это процедурное требование, в коде не отражается |
| §2.15 | Демо-сервисы отделены | ✓ | `grep -rE '\b(xauth\|xhttp\|xws)\b' asty/internal/ asty/cmd/` — пусто |

---

## §3 Архитектура: слои

| Проверка | Статус | Доказательство |
|---|---|---|
| L0 `core/*` не импортирует `internal/*` | ✓ | `grep '"asty/asty/internal/' asty/internal/core/` — пусто |
| L2 `domain/*` не импортирует infra/ops/api | ✓ | `grep` — пусто |
| L1 `infra/*` не импортирует domain/ops/api | ✓ | `grep` — пусто |
| L4 `api/*` не импортирует `server`/`agent` | ✓ | `grep` — пусто |
| Composition roots `server`/`agent` собирают слои | ✓ | `internal/server/boot.go`, `internal/agent/start.go` |
| Server и agent — независимые процессы | ✓ | `cmd/main.go:24` — `-mode` flag разводит |
| Server-на-не-лидере отдаёт GET, /metrics, /health | ✓ | leaderOnly middleware применяется только к POST (api/dashboard/api.go:97) |

---

## §4 Доменные FSM

| FSM | Все состояния из TZ присутствуют? | Доказательство |
|---|---|---|
| Allocation: Pending, Starting, Running, Restarting, Stopping, Stopped, Failed, Deleted | ✓ | `core/types/allocation.go:10-46` |
| Node: Joining, Ready, Stale, Draining, Drained, Paused, Down, Deleted | ✓ | `core/types/node.go:22-56` |
| Deployment: Running, Completed, Failed, Reverted, **RollbackFailed** | ✓ | `ops/deployer/states.go:10-30` |
| Drain: Draining, Migrating, Drained, Stuck, Ready | ⚠ | Нет явного `Stuck` — drain пишет ошибки в `op.status.Errors`, но не переходит в отдельное состояние. Возможный TODO |

Переходы Allocation между состояниями:
- `Pending→Starting` через CAS в `dispatchOne` (`ops/reconciler/reconcile.go:96`). ✓
- `Starting→Pending` (unstick `>90s`) — `ops/reconciler/reconcile.go:69`. ✓
- `Running→Restarting` в agent на падении процесса (`agent/restart.go:69` после `5ceab5c`). ✓
- `Running→Stopping` в `StopService` (`agent/services.go:128`). ✓
- `Failed` после `Restart.Attempts` исчерпан → `pruneFailed`. ✓

---

## §5 Циклы управления

### Reconciler

| TZ §5.1 | Статус | Где |
|---|---|---|
| Workqueue: FIFO + dedup + rate-limit | ✓ | `ops/reconciler/workqueue.go` |
| Backoff экспонента 500ms→60s | ✓ | `workqueue.go:35-36` |
| Источники: KV watcher (alloc, node), API trigger, resync 60s | ✓ | `ops/reconciler/watch.go`, `controller.go:42-44` |
| Per-key lock | ⚠ | TZ §5.1 описывает явный «acquire per-key lock». В коде — `dirty`/`processing`-карты, что даёт ту же гарантию (один воркер на ключ), но через дедупликацию очереди, а не отдельный мьютекс. Семантически эквивалент |
| Пайплайн: `Reconcile → Dispatch → Prune → Autoscale` | ✓ | `reconcile.go:22-39` |
| `system`-сервисы пропускают autoscale | ✓ | `reconcile.go:35` |

### Autoscaler

| TZ §5.2 | Статус | Где |
|---|---|---|
| Memory threshold в `%` от `svc.Resources.Memory` | ✓ | `scale_up.go:73-85` (после `47d84f1`) |
| `idle_hold` гистерезис | ✓ | `scale_down.go:48-56` (после `3359fe0`) |
| `MaxCopies` cap | ✓ | `scale_up.go:31-33` |
| RollbackFailed gate | ✓ | `autoscaler.go:63` (после `5ceab5c`) |
| `inCooldown` берёт `max(CooldownUp, CooldownDown)` | ✓ | `cooldown.go:19-22` |

### Deployer

| TZ §4.4 / §5.3 | Статус | Где |
|---|---|---|
| Реальный rollback на ошибке canary/rolling | ✓ | `ops/deployer/history.go:revertDeployment` |
| StateRollbackFailed терминальное | ✓ | `states.go:30` |
| `CanaryRetries` | ✓ | `canary.go:deployCanaryWithRetries` |
| `MaxParallel` default = 1 | ✓ | `core/types/service.go:159` (Resolve normalises) |
| `Canary` из `.asty` (не хардкод) | ✓ | `server/deployment.go:65` берёт `svc.Update.Canary` |
| `RollbackSteps[]` в `DeploymentRecord` | ❌ | TZ §4.4 упоминает аудит-поле, в коде НЕТ |
| Прогресс деплоя на NATS `asty.v1.deploy.progress.<service>` | ❌ | TZ §6.2 объявляет subject, в коде такого `Publish` НЕТ |

### Drainer

| TZ §5.4 | Статус | Где |
|---|---|---|
| Параллельные миграции, лимит `maxConcurrentMigrations=4` | ✓ | `ops/drainer/run.go:50` (после `5704d69`) |
| `waitForStopped` бюджет `kill_timeout + 10s` | ✓ | `wait.go:19,24` |
| `drainHealthDeadline = 2m` | ✓ | `wait.go:15` |
| Stuck-состояние | ❌ | См. §4 выше — нет |

### Scheduler

| TZ §5.5 | Статус | Где |
|---|---|---|
| `PickCandidates` использует DC-диверсити + proximity | ✓ | `ops/scheduler/candidates.go` |
| Учёт `EffectiveStatus` (Stale/Joining skip) | ✓ | `scheduler.go:90-93` после `701909a` |

### Leader

| TZ §5.6 | Статус | Где |
|---|---|---|
| KV TTL, refresh каждые ttl/2 | ✓ | `ops/leader/campaign.go` |
| `startLeaderWork`/`stopLeaderWork` идемпотентны | ✓ | `server/leadership.go` |

---

## §6 Контракты данных

### KV-бакеты

| Бакет | TZ объявляет | В коде |
|---|---|---|
| `asty-cluster` (nodes, allocs, services, cooldowns, scale, deployments, drains) | ✓ | `infra/kv/state.go:31`. **Но** записи `deployments/<service>` и `drains/<nodeID>` в коде НЕТ — deployment history в RAM ring buffer (`historyCap=100`), drain status — в `dm.drains` map. Это деградация vs TZ §6.1 |
| `asty-leader` (leader, TTL 5s) | ✓ | `ops/leader/election.go:47` |

### NATS subjects

| TZ §6.2 | Реальный subject в коде | Статус |
|---|---|---|
| `asty.v1.cmd.<nodeID>.<verb>` | `asty.v1.agent.<nodeID>.cmd.<verb>` | ❌ Порядок сегментов отличается |
| `asty.v1.event.<nodeID>.<event>` | Нет такого subject в коде; события идут через `streamHub` SSE | ❌ |
| `asty.v1.log.<role>.<nodeID>.<source>` | `asty.v1.agent.<nodeID>.logs.<source>` и `asty.v1.server.logs` | ❌ Другая структура |
| `asty.v1.metrics.gateway.<nodeID>` | `asty.v1.metrics.gateway.%s` | ✓ |
| `asty.v1.drain.progress.<nodeID>` | `asty.v1.drain.progress` (без nodeID) | ❌ |
| `asty.v1.deploy.progress.<service>` | НЕТ в коде | ❌ |
| `asty.v1.ping.<nodeID>` | `asty.v1.agent.<nodeID>.ping` и `.ping-peer` | ❌ Другая структура |
| `asty.v1.audit.<resource>.<action>` | `asty.v1.audit.<resource>.<action>` | ✓ |
| `$SYS.REQ.SERVER.<id>.STATSZ/JSZ` | ✓ в `agent/natsstats.go` | ✓ |

**Сводно:** subject-схема в коде сложилась исторически и не соответствует §6.2. Это **либо TZ устарел** (нужно обновить TZ под фактическую конвенцию `asty.v1.<role>.<nodeID>.<topic>`), **либо требуется миграция кода**. И то, и другое — отдельная работа.

### nats.conf

| TZ §6.3 | Статус | Где |
|---|---|---|
| `cluster{}` рендерится только при `len(peers)>0` | ✓ | `core/natsconf/render.go:54` |
| `system_account: SYS` | ✓ | `render.go:83` |
| Permissions для observer (STATSZ/JSZ only) | ✓ | `render.go:107-116` |
| Нет `http_port` директивы | ✓ | grep `http_port` пусто |

---

## §7 HTTP-поверхность

| TZ §7 | Статус | Где |
|---|---|---|
| Три поверхности: dashboard, prometheus, gateway | ✓ | `core/config/{config.go, gateway.go}` |
| Default ports: 7060 (dashboard+prom shared), 80 (gateway) | ✓ | `config.go:defaults()` |
| Default prefixes: `/dashboard/v1`, `/metrics`, `/api/v1` | ⚠ | Реальные совпадают; TZ §7 в md говорит `/api/v1` для дашборда — устарел |
| Shared listener при совпадении портов | ✓ | `api/dashboard/api.go:Start` mounts both |
| Standalone Prometheus при разных портах | ✓ | `server/boot.go:runStandalonePrometheus` |
| POST → 307 на лидера | ✓ | `leaderguard.go` |
| Write chain: tokenAuth → leaderOnly → audit → handler | ✓ | `api/dashboard/api.go:97-99` |
| Gateway path validation (regex `^[A-Za-z0-9_-]+$`) | ✓ | `api/gateway/routing.go:13` |

---

## §8 Конфигурация

| TZ §8.4 правило | Статус | Где |
|---|---|---|
| Validate отвергает `Domain == ""` вне dev_mode | ✓ | `config.go:76` |
| Validate отвергает `Token == ""` вне dev_mode | ✓ | `config.go:79` |
| Validate отвергает `User == AppUser` | ✓ | `nats.go:116` |
| Validate отвергает `MinCopies < 1` | ❌ | В Validate нет такого правила; default 3, но негативное допускается |
| Validate отвергает `MaxCopies < MinCopies` (если MaxCopies > 0) | ❌ | Не реализовано |
| Validate отвергает `TargetCPU/TargetMemory` вне `(0,100)` | ❌ | Не реализовано |
| Validate отвергает `gateway.rate_limit.*<=0` при Enabled | ✓ | `gateway.go:Validate` |

---

## §9 Наблюдаемость

| TZ §9.2 prefix | Реально emits | Статус |
|---|---|---|
| `asty_cluster_*` | `prom_cluster.go` | ✓ |
| `asty_node_*` | `prom_nodes.go` | ✓ |
| `asty_service_*` | `prom_services.go` | ✓ |
| `asty_alloc_*` | `prom_allocs.go` | ✓ |
| `asty_deploy_*` | `prom_deploy.go` | ✓ |
| `asty_leader` | `prom_cluster.go:55` | ✓ |
| `asty_node_nats_*`, `asty_cluster_nats_*` | `prom_nats.go` | ✓ |

**Mirror rule** (UI ↔ Prom) — текстуально соблюдается; точечная проверка каждого UI-тайла vs прометей-метрики не делалась.

**Gateway не имеет своего `/metrics`** — `grep prometheus asty/internal/api/gateway/` пусто. ✓

---

## §10 Безопасность

| TZ §10 | Статус | Где |
|---|---|---|
| Три NATS-учётки изолированы | ✓ | См. §2.7 |
| Artifact: HTTPS + sha256 | ✓ | `infra/artifact/downloader.go:71-75` (sha256). HTTPS-only НЕ форсится — `Download` принимает любой URL. ⚠ |
| Token middleware на write-эндпоинтах | ✓ | `dashboard/tokenauth.go` |
| Audit-log в `asty.v1.audit.*` | ✓ | `dashboard/audit.go` |
| Drop-root агента | ✓ | `agent/privileges.go` (после `868869b`); жёстко drop'ает в `asty` |
| Per-operator accounts / RBAC | ❌ | Out of scope (TZ §15) |
| TLS termination | ❌ | Out of scope (front-proxy) |

---

## §11 Операционные сценарии

| TZ §11 | Статус | Где |
|---|---|---|
| §11.1 systemd unit ordering | ✓ | `deploy/prod/systemd/asty-{agent,server}.service` (после `ce7db92`) |
| §11.2 Расширение кластера на лету | ✓ | `agent/natswatch.go` SIGHUP / cold restart + `server/streamreplicas.go` |
| §11.3 Локальный scale-up под нагрузкой | ✓ | locality-aware autoscaler |
| §11.4 Деплой с откатом | ✓ | После `5ceab5c` |
| §11.5 Drain узла | ✓ | После `5704d69` |

---

## §12 Файловая структура

| TZ tree | Статус | Заметка |
|---|---|---|
| `core/{identity,types,errors,codec,config,natsconf,netutil}` | ⚠ | `identity/` отсутствует. NodeID/AllocID генерируются в местах создания (фактически нет AllocID — alloc-key это `(serviceName, nodeID)`) |
| `infra/{kv,natsd,process,artifact,probe,events,logs,metrics}` | ⚠ | `natsd/` отсутствует — NATS-supervisor живёт в `agent/natssup.go` + `natswatch.go`. Слой L1 описан, но имплементация в composition root. **Технически нарушение слоя, потому что супервизор пишет файлы и exec'ит процесс — это L1 работа** |
| `domain/{allocation,service,node,deployment,drain,proximity}` | ⚠ | Только `domain/proximity/` есть. Остальные типы живут в `core/types/*.go` (allocation, service, node, deployment), что плоско вместо подпакетов. Прагматично, но не по букве §12 |
| `ops/{leader,reconciler,scheduler,autoscaler,deployer,drainer,discovery}` | ✓ | Все на месте |
| `api/{rest,prometheus,stream,gateway,health}` | ⚠ | `rest` переименован в `dashboard`. Структурно совпадает |
| `server/`, `agent/`, `testutil/` | ✓ | На месте |

---

## §13 Тестирование

| TZ §13 | Статус | Где |
|---|---|---|
| `make ci`: `build vet race test-integration layer-check` | ✓ | `Makefile:ci` |
| `*_test.go` рядом с тестируемым кодом | ✓ | По дереву |
| Property-тесты для FSM | ❌ | Не написаны |
| `tests/integration/` отдельная директория | ❌ | Интеграционные тесты внутри пакетов под `//go:build integration` |

---

## §14 Стратегия миграции

Перечислены 6 этапов. Сделано:
- §14.1, §14.2, §14.3, §14.4, §14.5, §14.6 — все основные ✓
- §14.4 split на `rest/prometheus/stream/health` — ✓ позже (`ffd5891`)

---

## Файлы свыше 200 строк (нарушение §2.8)

Сейчас (на HEAD `migration/tz`):

| Файл | Строк | Причина роста |
|---|---|---|
| `asty/internal/ops/reconciler/workqueue.go` | 214 | Документированное исключение (когезивная структура) |
| `asty/internal/server/boot.go` | 211 | После `runStandalonePrometheus` добавки в endpoint redesign |
| `asty/internal/agent/natssup.go` | 212 | Подросло за `Credential` для nats-server |
| `asty/internal/core/config/config.go` | 208 | Новые подструктуры `DashboardConfig`, `PrometheusConfig`, `AgentCapacityConfig` |

Три файла, не считая workqueue, требуют split'а — в TZ обещано «превысил — разделил».

---

## Сводка расхождений (для следующего PR)

**Критические (поведение):**

1. **NATS subject schema не совпадает с TZ §6.2** в 5 из 9 пунктов. Решение: либо мигрировать subjects (ломает совместимость), либо обновить TZ под фактическую конвенцию `asty.v1.<role>.<nodeID>.<topic>`. Рекомендую второе.
2. **`asty.v1.deploy.progress.<service>` не публикуется** — TZ объявляет, кода нет.
3. **`DeploymentRecord.RollbackSteps[]`** — объявлен в TZ §4.4, нет в коде.
4. **Drain stuck-состояние** — нет явного, только массив ошибок.
5. **Deployments/drains в KV** — TZ §6.1 говорит хранить, реально RAM-ring.

**Средние (валидация):**

6. **`Validate()` не отвергает** негативные/слишком большие `MinCopies`, `MaxCopies < MinCopies`, проценты вне `(0,100)`.
7. **Artifact URL не форсит HTTPS** — TZ §10.2 говорит «только HTTPS», код принимает любой URL.

**Стилевые (структура):**

8. Файлы > 200 строк: `boot.go`, `natssup.go`, `config.go`. Split.
9. `core/identity/`, `infra/natsd/` отсутствуют как самостоятельные пакеты (по букве TZ §12).
10. `domain/` содержит только `proximity/` — типы аллокации/сервиса/ноды/деплоя/дрейна в `core/types/*.go` плоско.

**Концептуальные (TZ устарел):**

11. **TZ §2.10 / §7** описывают `/api/v1` как data plane. Юзер позже перенаправил на `/dashboard/v1`. **Обновить TZ.md.**
12. **Drop-root** в TZ §10.4 объявлен out-of-scope, в коде реализован (`868869b`). Обновить TZ.

---

## Что точно соответствует TZ

- Слои L0..L4 чисто, depguard + layer-check в CI.
- Все три ENV-принципа (`core/config` единственная точка чтения).
- Демо-имена не утекают в `asty/`.
- Все enum'ы Allocation/Node/Deployment расширены до TZ-набора.
- Reconciler / Autoscaler / Deployer / Drainer семантически делают то, что TZ §5 описывает (с учётом перечисленных выше дефицитов).
- Token + leaderOnly + audit middleware в правильном порядке.
- Three NATS accounts с валидацией.
- Drop-root в `asty` после bootstrap, nats-server тоже не root.
- systemd unit'ы с правильным `After=`/`Wants=`.
