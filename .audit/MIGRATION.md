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
| `ffd5891` | §14.4 (доп) | Split `api/rest` → `api/{prometheus,stream,health,rest}` с собственными `Context`-интерфейсами и adapter'ами |
| `4d45918` | endpoint redesign | `api/rest` → `api/dashboard`; `/dashboard/v1` (admin), `/metrics` (prom), `/api/v1` (gateway); порты + префиксы конфигурируемы; общий listener при совпадении портов |
| `51105c5` | §10 (audit) | `asty.v1.audit.*` публикация на каждом write-эндпоинте dashboard'а; payload — CBOR `types.AuditEvent`; статус через `statusRecorder` |
| `ce7db92` | §11.1 | systemd unit'ы `asty-{agent,server}.service` с `After=`/`Wants=`; README и хардненинг |

Все коммиты с `make ci` passing (`build vet race test-integration
layer-check`).

## Что осталось

Узкий список:

### §10 безопасность — частично

Token-auth (`341bc43`), audit-log (`51105c5`), и systemd-хардненинг
(`ce7db92`) сделаны. Остаётся:
- **Drop-root** для агента после `fork+exec` дочерних. Требует
  `setuid`/`setgid` + cgroup-семантики, нетривиально кросс-платформенно.
- **TLS termination** — front-proxy (Caddy/Traefik/nginx) перед
  dashboard и gateway. Сам Asty слушает HTTP plain by design.

### TZ §15 (out of scope ветки)

- Multi-region failover state replication между гео-кластерами.
- Persistent service state за пределами KV.
- Identity bootstrap (`A_TOKEN` и `A_NATS_*_PASSWORD` provisioning).
- Per-operator accounts / RBAC (сейчас audit `actor_ip` без user-identity).

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

`git push origin migration/tz`, открыть PR. CI должен зеленить
(`make ci`). Дальше — drop-root для агента (TZ §10) и frontend
обновление под новый `/dashboard/v1` префикс с тестированием в браузере.
