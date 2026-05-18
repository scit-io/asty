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
| `701909a` | §5.3 | FSM-расширения: `AllocStopping`, `AllocRestarting`, `NodeJoining`, `NodeStale`, `EffectiveStatus` |
| `5ceab5c` | §4.4 | `update.canary_retries`, `ServiceCooldown.RollbackFailed`-флаг как gate для автоскейлера |
| `341bc43` | §10 (часть) | `tokenAuth` middleware на POST `/api/v1` (Authorization / X-Asty-Token, constant-time) |
| `4f86c2a` | §14.2 (sunset) | Legacy `/metrics/*` data-prefix удалён; `deprecatedAPI` middleware больше нет |
| `6809da5` | §14.4 | Большой рефактор: `features/*` → `infra/`, `domain/`, `ops/`, `api/`; depguard правила L0..L4 |

Все коммиты с `make ci` passing (`build vet race test-integration
layer-check`).

## Что осталось

Эти участки требуют отдельных PR-серий:

### §14.4 — внутри `api/rest` (не разделён на rest/prom/stream)

Большой рефактор слоёв сделан, но `api/rest` пока — единая директория с
prom_*.go и stream_*.go рядом. TZ §12 хочет их в `api/prom/` и
`api/stream/`. Это вторая итерация рефактора, требует:
- Поднять отдельный частный `prometheus.Registry` из `prom` в
  `api/prom/registry.go`.
- Передавать `Snapshot()` через интерфейс, который `api/prom` импортирует
  из `api/rest` (или из новой `api/internal/snapshot/`-точки).

### §11.1 — bootstrap последовательности

Сейчас server и agent поднимаются независимо. TZ §11.1 рисует ровный
порядок: agent сначала, server потом, чтобы server точно подключился к
готовому NATS. В коде это можно оставить как есть и просто
задокументировать, но если хочется enforcement — `systemd Wants=`/
`After=` в unit-файлах prod-деплоя.

### Безопасность (§10) — частично

Token-auth middleware на `/api/v1` сделан (`341bc43`). Остаётся:
- Drop-root для агента после fork+exec.
- TLS termination (front-proxy схема, не сам Asty).
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
"migration to TZ — stages 1, 2, 3, 4.4, 5.1, 5.2, 5.3, 6.1, 6.2, 6.3,
7.2, 10 (partial), 14.4", в описании сослаться на `.audit/TZ.md` и
этот журнал. CI должен зеленить (`make ci`).
