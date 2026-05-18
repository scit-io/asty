# Migration log — ветка `migration/tz`

Журнал миграции от текущего состояния (`.audit/AS_IS.md`) к целевой
архитектуре (`.audit/TZ.md`). Ветка `migration/tz` ответвлена от `main`.

## Сделано в этой ветке

| Коммит | Этап TZ | Что |
|---|---|---|
| `47d84f1` | §14.1 | Memory threshold в `%`, `MaxParallel` default, `Canary` поле, реальный `auto_revert` + `StateRollbackFailed` |
| `80d45bf` | §14.3 | `leaderOnly` middleware: writes на followers → 307 на лидера, inline-checks убраны |
| `e34a927` | §14.2 | `apiPrefix /metrics → /api/v1`, legacy alias с deprecation header'ом, vite-proxy для обоих |
| `5704d69` | §14.5 | Drain regular-аллокаций параллельно, лимит `maxConcurrentMigrations=4` |
| `3359fe0` | §5.2 | `autoscale.max_copies` + `autoscale.idle_hold` (`IdleSince` в `ServiceCooldown`) |
| `7699122` | §14.6 | Все файлы `≤200` строк, кроме `workqueue.go` (документированное исключение) |
| `59387d4` | §2.9 | Все `A_*` env-чтения собраны в `core/config`, `Agent.Capacity`/`NATS.Peers*`/`Artifact` под-структуры |
| `ae6609e` | §14.6 | `make layer-check` + `.golangci.yml` (depguard) ловят регрессию принципа §2.9 в CI |

Все коммиты с `make ci` passing (`build vet race test-integration
layer-check`).

## Что осталось

Эти разделы TZ требуют больших структурных правок и в эту сессию не
вошли. Каждое — отдельная ветка / PR-серия:

### §14.4 — рефактор слоёв L0..L4 (6 недель по плану TZ)

Самое объёмное. Перенос пакетов:

- `asty/internal/features/*` → разнести по `infra/`, `domain/`, `ops/`, `api/`.
- `asty/internal/server`, `asty/internal/agent` остаются composition
  roots, но избавляются от любой бизнес-логики.
- `depguard` правила из `.golangci.yml` уже описывают будущую онион-структуру —
  включить enforcement, когда переезд состоится.
- Скрипт переезда: переименовать каталоги, потом `goimports -w
  ./...`, потом починить тесты, которые ссылаются на старые пути.

### §5.3 — FSM состояний `Allocation`/`Node`

Добавить `Stopping`, `Restarting`, `Joining`, `Stale`. Это требует:
- Расширить `core/types` enum'ы.
- Обновить `state.AllocationStatus.IsLive` / `Occupies`.
- Поправить агентский путь `restart.go` чтобы перевод в `Restarting`
  был явным, а не маскировался в `Running`.
- Обновить таблицы метрик (`asty_alloc_status`) и UI-теги.

### §4.4 — продвинутые поля деплоя

Уже частично сделано (canary, max_parallel, auto_revert с настоящим
откатом, RollbackFailed). Остаётся:
- `CanaryRetry`-фаза с конечным бюджетом ретраев канарейки.
- Метка `state=rollback_failed` в KV на уровне сервиса, которую
  autoscaler читает и **прекращает** работу с сервисом до явного
  снятия оператором.
- `RollbackSteps[]` в `DeploymentRecord` для аудита.

### §4.2 — FSM `Node`

`Joining` и `Stale` как явные состояния. Heartbeat-таймауты:
- `stalenessThreshold` (default 30 с) → `Stale`.
- `downThreshold` (default 2 минуты) → `Down`.
- Scheduler пропускает `Stale` для нового размещения, но не убивает
  существующее.

### §7.2 — UI обновление под `/api/v1`

`asty/web/src/api/client.ts` уже переключён, легаси `/metrics/*` на
backend'е держится один цикл. Полный sunset:
- Убрать legacy mux в `api.go`.
- Удалить vite-proxy для `/metrics` (оставить только `/api`,
  `/health`).

### §11.1 — bootstrap последовательности

Сейчас server и agent поднимаются независимо. TZ §11.1 рисует ровный
порядок: agent сначала, server потом, чтобы server точно подключился к
готовому NATS. В коде это можно оставить как есть и просто
задокументировать, но если хочется enforcement — `systemd Wants=`/
`After=` в unit-файлах prod-деплоя.

### Безопасность (§10)

- Drop-root для агента после fork+exec.
- Token-auth middleware на `/api/v1` (TLS + token проверяется
  constant-time).
- Audit-log: write-операции пишут в `asty.v1.audit.*` сабжект.

## Откат

Ветка `migration/tz` ребейзабельна. Если какой-то этап оказывается
проблемным — отдельный коммит можно `revert`'ить через `git revert
<sha>`, остальные продолжат работать; коммиты независимы по
содержимому, кроме:
- §2.9 (env централизация) опирается на §14.6 (split oversized files),
  потому что новые поля Config растянули `service.go`. Если откатывать
  §14.6, сначала откатить §2.9.

## Не в scope этой ветки

То, что в TZ §15 явно вне scope (multi-region replication, TLS
termination, audit log, identity bootstrap, persistent service state),
здесь не двигалось — это отдельные ТЗ.

## Следующий шаг

`git push origin migration/tz`, открыть PR с заголовком вида
"migration to TZ — stages 1, 2, 3, 5.1, 5.2, 6.1, 6.2, 6.3", в
описании сослаться на `.audit/TZ.md` и этот журнал. CI должен
зеленить (`make ci`).
