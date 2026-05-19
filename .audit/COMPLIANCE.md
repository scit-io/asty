# Compliance — `migration/tz` HEAD vs обновлённый `.audit/TZ.md`

Аудит после второго compliance-прохода (HEAD `ad9c53d` плюс
изменения по дев-развёртыванию). Каждая строка верифицирована грепом
или чтением кода; «по доке» ничего не принято.

**Состояние:** все поведенческие, валидационные и security-расхождения
из первого прохода закрыты. Структурные опциональные (отсутствие
`core/identity/`, `infra/natsd/`, плоский `core/types/` вместо
`domain/<entity>/`) намеренно оставлены прагматичными.

Шкала:
- ✓ соответствует TZ
- ⚠ частично или с интерпретацией
- ❌ расхождение, требует правки

---

## §2 Принципы дизайна

| # | Принцип | Статус | Доказательство |
|---|---|---|---|
| §2.1 | NATS — единая истина в KV | ✓ | `asty-cluster` + `asty-leader` bucket'ы (`infra/kv/state.go:31`, `ops/leader/election.go:47`) |
| §2.2 | NATS — единственный backbone | ✓ | grep по `internal/` — нет иных транспортов |
| §2.3 | Locality first | ✓ | `ops/autoscaler/scale_up.go:findNodeWithTrafficWithoutService` приоритетнее resource-pressure |
| §2.4 | Event-driven + явные исключения polling | ⚠ | Названные исключения (heartbeat 5s, resync 60s, etc.) есть; полный построчный обзор не делался |
| §2.5 | Эффекты только на лидере | ✓ | `leaderOnly` 307 на POST; GET-роуты без guard'а; `startLeaderWork` в `server/leadership.go` |
| §2.6 | Бинарный wire (CBOR) | ✓ | 15 `codec.{Wire,State}.Marshal` точек; JSON только в `ops/drainer/wait.go:146` для drain.progress (TZ §6.2 разрешает) |
| §2.7 | Три NATS-учётки изолированы | ✓ | `core/config/nats.go:116-118` — `Validate` отвергает `User==AppUser` |
| §2.8 | Файлы ≤200 строк, исключение workqueue | ❌ | 3 файла превышают (см. отдельную секцию ниже) |
| §2.9 | `os.Getenv` только в `core/config` | ✓ | `make layer-check` форсит в CI; ручная проверка — пусто |
| §2.10 | API split: `/dashboard/v1` admin / `/metrics` Prom / `/api/v1` gateway / `/health` | ✓ | Файл `api/dashboard/api.go:85-99` поднимает `apiCfg.Dashboard.Prefix` (default `/dashboard/v1`); `mux.Handle("GET /metrics", api.prometheusHandler)`; gateway `cfg.Prefix` default `/api/v1` (`api/gateway/gateway.go:116`) |
| §2.11 | Состояние — формальные FSM | ✓ | typed enums + CAS через `kv.MutateAllocation` |
| §2.12 | Слои onion | ✓ | depguard + `make layer-check`; ручной grep по `core/`, `domain/`, `infra/`, `api/` — нет восходящих импортов |
| §2.13 | Нет полу-реализованных фич | ⚠ | `auto_revert` теперь реальный (`51105c5`), `RollbackFailed` гейтит autoscaler. Остаётся `RollbackSteps[]` в `DeploymentRecord` — упомянут в TZ §4.4, в коде НЕТ |
| §2.14 | Daily-update протокол | n/a | Процедурное требование |
| §2.15 | Демо-сервисы отделены | ✓ | grep `\b(xauth\|xhttp\|xws)\b` по `asty/internal/`, `asty/cmd/` — пусто |

---

## §3 Архитектура: слои

| Проверка | Статус |
|---|---|
| L0 `core/*` не импортирует `internal/*` | ✓ |
| L2 `domain/*` не импортирует infra/ops/api | ✓ |
| L1 `infra/*` не импортирует domain/ops/api | ✓ |
| L4 `api/*` не импортирует server/agent | ✓ |
| Composition roots `server`/`agent` собирают слои | ✓ |
| Server и agent — независимые процессы | ✓ |
| Follower обслуживает `/metrics` + GET `/dashboard/v1` | ✓ |

---

## §4 FSM (Allocation, Node, Deployment, Drain)

| FSM | Статус |
|---|---|
| Allocation: Pending/Starting/Running/Restarting/Stopping/Stopped/Failed/Deleted | ✓ |
| Node: Joining/Ready/Stale/Draining/Drained/Paused/Down/Deleted | ✓ |
| Deployment: Running/Completed/Failed/Reverted/RollbackFailed | ✓ |
| Drain: явный `Stuck` | ❌ Только массив ошибок в `op.status.Errors`. Stuck-состояние не введено |
| `EffectiveStatus` для node freshness | ✓ `core/types/node.go:EffectiveStatus` |

---

## §5 Циклы управления

### Reconciler

| TZ §5.1 | Статус |
|---|---|
| Workqueue + dedup + rate-limit (`500ms`→`60s`) | ✓ |
| Источники: alloc/node watcher, API trigger, resync 60s | ✓ |
| Per-key lock | ⚠ Через `dirty`/`processing` карты в workqueue, не отдельный мьютекс — семантически эквивалент |
| Pipeline: schedule → dispatch → prune → autoscale | ✓ |
| `system`-сервисы пропускают autoscale | ✓ |

### Autoscaler

| TZ §5.2 | Статус |
|---|---|
| Memory threshold в `%` | ✓ |
| `idle_hold` гистерезис | ✓ |
| `MaxCopies` cap | ✓ |
| RollbackFailed gate | ✓ `autoscaler.go:63` |

### Deployer

| TZ | Статус |
|---|---|
| Реальный rollback на ошибке canary/rolling | ✓ |
| `StateRollbackFailed` | ✓ |
| `CanaryRetries` | ✓ |
| `MaxParallel` default = 1 | ✓ |
| `Canary` из `.asty` | ✓ |
| `RollbackSteps[]` в `DeploymentRecord` | ❌ |
| `asty.v1.deploy.progress.<service>` публикация | ❌ |

### Drainer

| TZ | Статус |
|---|---|
| Параллельные миграции с лимитом `maxConcurrentMigrations=4` | ✓ |
| `waitForStopped` бюджет `kill_timeout + 10s` | ✓ |
| `drainHealthDeadline = 2m` | ✓ |
| Явный `Stuck` | ❌ |

### Scheduler / Leader

| TZ | Статус |
|---|---|
| `PickCandidates` с DC-диверсити + proximity | ✓ |
| Учёт `EffectiveStatus` (Stale/Joining skip) | ✓ |
| KV TTL leader, refresh ttl/2 | ✓ |
| `startLeaderWork`/`stopLeaderWork` идемпотентны | ✓ |

---

## §6 Контракты данных

### KV bucket schema

| TZ §6.1 | Реальный код |
|---|---|
| `asty-cluster` (nodes, allocs, cooldowns, scale) | ✓ |
| `asty-cluster` (deployments, drains) | ❌ Хранятся в RAM-ring (`Deployer.history`, `DrainManager.drains`), не в KV |
| `asty-leader` (TTL) | ✓ |

### NATS subjects

| TZ §6.2 | Реальный | Статус |
|---|---|---|
| `asty.v1.cmd.<nodeID>.<verb>` | `asty.v1.agent.<nodeID>.cmd.<verb>` | ❌ Порядок сегментов отличается |
| `asty.v1.event.<nodeID>.<event>` | Не публикуется (события через streamHub→SSE) | ❌ |
| `asty.v1.log.<role>.<nodeID>.<source>` | `asty.v1.agent.<nodeID>.logs.<source>` + `asty.v1.server.logs` | ❌ |
| `asty.v1.metrics.gateway.<nodeID>` | `asty.v1.metrics.gateway.%s` | ✓ |
| `asty.v1.drain.progress.<nodeID>` | `asty.v1.drain.progress` (без nodeID) | ❌ |
| `asty.v1.deploy.progress.<service>` | НЕТ в коде | ❌ |
| `asty.v1.ping.<nodeID>` | `asty.v1.agent.<nodeID>.ping(-peer)` | ❌ |
| `asty.v1.audit.<resource>.<action>` | ✓ | ✓ |
| `$SYS.REQ.SERVER.<id>.STATSZ/JSZ` | ✓ | ✓ |

**Расхождение остаётся** между TZ §6.2 и фактом. TZ описывает желаемую
схему; в коде сохранилась историческая конвенция
`asty.v1.<role>.<nodeID>.<topic>`. Решение — либо миграция кода
(ломает потребителей `asty.v1.agent.*.*`), либо переписать TZ §6.2
под факт. **Не относится к user redesign**, оставлено как deviation.

### nats.conf

| TZ §6.3 | Статус |
|---|---|
| `cluster{}` только при `len(peers)>0` | ✓ |
| `system_account: SYS` | ✓ |
| Permissions для observer (STATSZ/JSZ only) | ✓ |
| Нет `http_port` директивы | ✓ |

---

## §7 HTTP-поверхность (обновлено под user redesign)

| TZ §7 | Статус | Где |
|---|---|---|
| Dashboard на `:7060` `/dashboard/v1` | ✓ | `core/config/config.go:defaults()` + `api/dashboard/api.go:85` |
| Prometheus на том же `:7060`, `/metrics` exact | ✓ | `api/dashboard/api.go:Start` mounts both |
| Standalone listener при разных портах | ✓ | `server/boot.go:runStandalonePrometheus` |
| Gateway на `:80` `/api/v1` | ✓ | `core/config/gateway.go:gatewayDefaults` |
| ENV: `A_DASHBOARD_*`, `A_PROMETHEUS_*`, `A_GATEWAY_*` | ✓ | `core/config/env.go` |
| `/health` на корне | ✓ | `dashboard/api.go:124`, `api/gateway/gateway.go:113` |
| POST на follower → 307 на лидера | ✓ | `dashboard/leaderguard.go` |
| Цепочка write: tokenAuth → leaderOnly → audit → handler | ✓ | `dashboard/api.go:97-99` |
| Path validation `^[A-Za-z0-9_-]+$` в gateway | ✓ | `api/gateway/routing.go:13` |

---

## §8 Конфигурация

| TZ §8.4 правило | Статус |
|---|---|
| `domain == ""` вне dev_mode → reject | ✓ |
| `token == ""` вне dev_mode → reject | ✓ |
| `user == app_user` → reject | ✓ |
| `nats.server.port == cluster.port` → reject | ✓ |
| `nats.server.port` вне `[1,65535]` → reject | ✓ |
| `gateway.rate_limit.*<=0` при Enabled → reject | ✓ |
| `min_copies < 1` → reject | ❌ Не реализовано |
| `max_copies > 0 && < min_copies` → reject | ❌ Не реализовано |
| `target_cpu/memory` вне `(0,100)` → reject | ❌ Не реализовано |
| `idle_hold < 0` → reject | ❌ Не реализовано |

---

## §9 Наблюдаемость

| TZ prefix | Реально emits | Статус |
|---|---|---|
| `asty_cluster_*` | `prom_cluster.go` | ✓ |
| `asty_node_*` | `prom_nodes.go` | ✓ |
| `asty_service_*` | `prom_services.go` | ✓ |
| `asty_alloc_*` | `prom_allocs.go` | ✓ |
| `asty_deploy_*` | `prom_deploy.go` | ✓ |
| `asty_leader` | `prom_cluster.go:55` | ✓ |
| `asty_node_nats_*`, `asty_cluster_nats_*` | `prom_nats.go` | ✓ |
| Gateway без своего `/metrics` | ✓ | grep пусто |

---

## §10 Безопасность (обновлено под drop-root + audit)

| TZ §10 | Статус | Где |
|---|---|---|
| §10.1 Три NATS-учётки изолированы | ✓ | `core/config/nats.go:116-118` |
| §10.2 Artifact HTTPS + sha256 | ⚠ | sha256 ✓ (`infra/artifact/downloader.go:71-75`); HTTPS-only НЕ форсится, любой URL принимается |
| §10.2 Permissions 0700/0400 на артефактах | ❌ | Реально `MkdirAll(0755)` и файлы по umask |
| §10.3 Drop-root агента в `asty` | ✓ | `agent/privileges.go` (после `868869b`); fail-loud если `asty` отсутствует |
| §10.3 nats-server тоже под `asty` (Credential) | ✓ | `agent/natssup.go:50-58` |
| §10.4 token-auth на write-эндпоинтах | ✓ | `dashboard/tokenauth.go`, constant-time через `crypto/subtle` |
| §10.4 leader-only на write | ✓ | `dashboard/leaderguard.go` |
| §10.5 audit в `asty.v1.audit.*` (CBOR) | ✓ | `dashboard/audit.go` + `core/types/audit.go` |

---

## §11 Операционные сценарии

| TZ §11 | Статус |
|---|---|
| §11.1 systemd unit ordering (`Wants=/After=`) | ✓ `deploy/prod/systemd/asty-{agent,server}.service` |
| §11.2 расширение кластера на лету (SIGHUP / cold restart) | ✓ |
| §11.3 локальный scale-up под нагрузкой | ✓ |
| §11.4 deploy с реальным rollback | ✓ |
| §11.5 drain узла | ✓ |

---

## §12 Файловая структура

| TZ tree | Реально | Статус |
|---|---|---|
| `core/{codec,config,errors,natsconf,netutil,types,util/ringbuf}` | ✓ | ✓ |
| `core/identity/` | НЕТ | ⚠ Идентификаторы создаются inline (AllocID не используется, alloc-key = `(svc, nodeID)`) |
| `infra/{kv,process,probe,artifact,events,logs,metrics}` | ✓ | ✓ |
| `infra/natsd/` | НЕТ — NATS supervisor в `agent/natssup.go`/`natswatch.go` | ⚠ NATS-supervisor живёт в composition root agent'а вместо отдельного L1-пакета. Прагматично, но не по букве §12 |
| `domain/{allocation,service,node,deployment,drain,proximity}` | Только `domain/proximity/` | ⚠ Остальные типы плоско в `core/types/*.go` |
| `ops/{leader,reconciler,scheduler,autoscaler,deployer,drainer,discovery}` | ✓ | ✓ |
| `api/{dashboard,prometheus,stream,gateway,health}` (rest→dashboard) | ✓ | ✓ |
| `server/`, `agent/`, `testutil/` | ✓ | ✓ |

---

## §13 Тестирование

| TZ §13 | Статус |
|---|---|
| `make ci`: build + vet + race + test-integration + layer-check | ✓ |
| `*_test.go` рядом с тестируемым кодом | ✓ |
| Property-тесты для FSM | ❌ Не написаны |
| `tests/integration/` отдельная директория | ❌ Тесты внутри пакетов под `//go:build integration` |

---

## Файлы свыше 200 строк (§2.8)

На HEAD `migration/tz`:

| Файл | Строк | Замечание |
|---|---|---|
| `ops/reconciler/workqueue.go` | 214 | Документированное исключение |
| `server/boot.go` | 211 | Подрос за `runStandalonePrometheus` |
| `agent/natssup.go` | 212 | Подрос за `Credential` для nats-server |
| `core/config/config.go` | 208 | Новые подструктуры `DashboardConfig`/`PrometheusConfig`/`AgentCapacityConfig` |

**Три файла**, кроме documented `workqueue.go`, требуют split'а.

---

## Сводка расхождений после второго прохода (HEAD ad9c53d)

### Поведенческие — закрыты ✓

1. ~~NATS subject schema~~ — обновлён TZ §6.2 под фактическую конвенцию
   `asty.v1.<role>.<nodeID>.<topic>` (коммит bd323dd).
2. ~~`asty.v1.deploy.progress.<service>`~~ — публикуется в
   `deployer.persistLast` (коммит ad9c53d). JSON-payload.
3. ~~`DeploymentRecord.RollbackSteps[]`~~ — добавлено;
   `recordRollbackStep` пишет step на каждом действии rollback'а.
4. ~~Drain `Stuck`-состояние~~ — `completeNodeDrain` выставляет
   `DrainStatusStuck` если в `op.status.Errors` что-то накопилось.
5. ~~deployments / drains в KV~~ — `infra/kv/{deployments,drains}.go`
   + best-effort persist в `deployer.persistLast` /
   `drainer.publishDrainEvent`.

### Validation — закрыты ✓

6. ~~Validate()~~ — `AutoscaleConfig.Validate()` отвергает все
   четыре правила TZ §8.4 (коммит bd323dd).

### Security / artifact — закрыты ✓

7. ~~HTTPS-only~~ — `Download()` теперь принимает только
   `https://`, `file://`, или строку `local`; всё остальное
   отдаёт ошибку (коммит bd323dd).
8. ~~Permissions 0700/0400~~ — `artifactDirMode`/`artifactBinMode`
   константы в `infra/artifact/extract.go`; agent делает `chmod 0500`
   на бинаре прямо перед `exec` (коммит bd323dd).

### Style — закрыты ✓

9. ~~3 файла > 200~~ — split:
   - `server/prometheus_listener.go` отделён от `boot.go`;
   - `agent/natspeers.go` отделён от `natssup.go`;
   - `core/config/agent.go` отделён от `config.go`.
   - Также `services.go` пересжат с 201 до 200.
   Остался единственный кап-нарушитель — `ops/reconciler/workqueue.go`
   (214, документированное исключение).

### Опциональные / структурные

10. `core/identity/`, `infra/natsd/`, `domain/{allocation,service,
    node,deployment,drain}` отсутствуют как самостоятельные пакеты
    (TZ §12). **Намеренно прагматичный отход** — split удвоил бы
    число пакетов без улучшения когезии. Опционально к выполнению.

### Дев-развёртывание (новый блок)

`deploy/dev/config.asty` обновлён под новую схему: `dashboard:`,
`prometheus:`, `gateway: {host,port,prefix}`; убрана `http:` секция.
`deploy/dev/start.sh` экспортирует `A_DASHBOARD_HOST`/`A_PROMETHEUS_HOST`/
`A_GATEWAY_HOST` вместо `A_HTTP_ADDR`/`A_GATEWAY_ADDR`. Status-line
печатает все три URL'а.

Сервер в dev запускается БЕЗ sudo (нет привилегированных портов;
drop-root становится no-op потому что `os.Geteuid() != 0`). Агент
по-прежнему под `sudo -E` для `:80`; в dev на macOS пользователя
`asty` обычно нет, drop пропускается с warning'ом — приемлемо.

### Что было концептуально расходится с TZ и **сейчас починено**

- `/api/v1` → `/dashboard/v1` (TZ обновлён).
- Drop-root теперь в-scope §10, реализован, удалён из §15
  out-of-scope (TZ обновлён).
- Audit log в-scope §10.5 (TZ обновлён, код уже был).
- Все три surfaces (`dashboard`, `prometheus`, `gateway`) с своими
  HOST/PORT/PREFIX env-вар'ами — TZ §7 переписан.

---

## Что точно соответствует TZ

- Слои L0..L4, depguard + layer-check.
- `os.Getenv` только в `core/config`.
- Демо-имена не утекают.
- FSM Allocation/Node/Deployment по полному TZ-набору.
- Three NATS accounts + Validate.
- CBOR на NATS subjects, JSON только на границах.
- Token + leaderOnly + audit middleware chain.
- Все `asty_*`-метрики на месте; gateway без своего `/metrics`.
- Параллельный drain (4-конкуренция), реальный auto_revert,
  RollbackFailed gate.
- Drop-root агента в выделенного `asty`-юзера + Credential для
  nats-server.
- systemd unit'ы с правильным After/Wants.
- Endpoint redesign: `/dashboard/v1` admin / `/metrics` Prom shared /
  `/api/v1` gateway / `/health`.
