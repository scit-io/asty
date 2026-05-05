# Архитектура — рабочие заметки

## Контекст

Asty (коммерческое название) и platform.go (рабочее название) — один и тот же продукт. Репозитории разделены чтобы не путать контекст. В asty переносим работающие куски из platform.go с рефакторингом.

Ключевое отличие asty от текущего platform.go: кастомный оркестратор вместо Nomad + locality-aware autoscaler.

## Что берём из Nomad (только реально используемое в platform.go)

| Функциональность | Как используется сейчас | Nomad-источник |
|---|---|---|
| Запуск процессов | raw_exec, artifact download, checksum | client/allocrunner, client/driver/raw_exec |
| Health checks | HTTP (path, interval, timeout) | client/serviceregistration |
| Restart policy | attempts, interval, delay | client/allocrunner/taskrunner |
| Logs | stdout/stderr → файл, ротация | client/allocrunner/taskrunner/logmon |
| Resources | cpu, memory limits | client/lib/resources |
| Rolling update | max_parallel, min_healthy_time, healthy_deadline, auto_revert, canary | nomad/deploymentwatcher |
| System scheduler | type=system (копия на каждой ноде) | scheduler/system |
| Service scheduler | type=service (N копий) | scheduler/generic |
| Server/Client | единый бинарник | agent |
| ACL | токен авторизации | nomad/acl |
| DNS discovery | retry_join по DNS A-записям | helper/discover |

## Что НЕ берём из Nomad

- Consul, Vault интеграция — NATS + env
- CSI/Volumes, Connect/Service Mesh — не нужно
- Multi-region federation — один кластер с multi-DC
- Namespaces, Sentinel policies — один namespace, простой ACL
- HCL парсер — YAML (.asty)
- Serf gossip (4648), RPC (4647) — NATS заменяет оба
- UI (React) — свой встроенный

## Что добавляем поверх (нет в Nomad)

- **Locality-aware autoscaler** — главная фича
- **NATS как единый транспорт** вместо Serf+RPC — минус 2 порта
- **NATS JetStream KV** как state store вместо Raft (один бакет `asty-cluster`, без TTL — stale ноды фильтруются по `LastSeen` в scheduler)
- **Leader election через NATS** вместо Raft

## Коммуникация

Nomad: Serf gossip (4648) + RPC (4647) + Raft.
Asty: всё через NATS (4222 client, 6222 cluster).

## Маппинг конфигов .nomad → .asty

`.asty` — декларативный YAML вместо HCL. Без Nomad-специфичных полей.

- `job.type = "system"` → `type: system`
- `task.driver = "raw_exec"` → убрано (единственный вариант)
- `task.artifact` → `artifact` (url, checksum)
- `task.resources` → `resources` (cpu, memory)
- `service.check` → `health` (type, path, interval, timeout)
- `group.restart` → `restart` (attempts, interval, delay)
- `job.update` → `update` (max_parallel, min_healthy_time, etc.)

## Процесс переноса

Переносим работающий код из ../platform.go в asty с рефакторингом:
- Точка входа: `cmd/asty/main.go`
- Код оркестратора: `internal/platform/asty/`
- Конфиги сервисов: `services/*.asty`
- Пространство имён везде `asty`
