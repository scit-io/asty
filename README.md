# Asty

Оркестратор микросервисов с locality-aware autoscaling для NATS-платформы.

Единый бинарник: agent + server. Конфиги — файлы `.asty`. Пространство имён: `asty`.

## Архитектура

```
Клиент → LB (geo) → Gateway (:80) → NATS (127.0.0.1:4222) → Сервис (та же нода)
```

**На каждой ноде:** NATS, Asty Agent, Gateway (type=system), сервисы (0..N).
**Один на кластер:** Asty Server (scheduling, autoscaling, Web UI, деплой) — leader election через NATS.

Agent ↔ Server — NATS subjects `asty.v1.*`. Состояние — NATS JetStream KV.

## Структура проекта

```
asty/
  cmd/asty/main.go                       — точка входа, единый бинарник
  internal/platform/asty/
    agent.go                             — lifecycle агента
    server.go                            — lifecycle сервера
    process.go                           — запуск/остановка процессов
    health.go                            — HTTP health checks
    collector.go                         — CPU/Memory метрики per process
    logs.go                              — ротация логов
    artifact.go                          — скачивание бинарника + checksum
    scheduler.go                         — locality-aware placement
    autoscaler.go                        — scaling decisions
    deployer.go                          — rolling update, canary
    leader.go                            — leader election (NATS JetStream)
    discovery.go                         — обнаружение нод через DNS
    state.go                             — состояние кластера (JetStream KV)
    proximity.go                         — DC latency matrix
    config.go                            — загрузка A_* переменных
    service.go                           — парсинг .asty-определений сервисов
    api.go                               — HTTP API (REST)
    handlers.go                          — обработчики: nodes, services, deploy
    ui.go                                — встроенный Web UI
  deployments/systemd/asty.service       — systemd unit
  services/                              — определения сервисов (.asty)
```

## Типы сервисов

- **system** — копия на каждой ноде (Gateway)
- **service** — управляется autoscaler, количество и размещение по нагрузке

## Locality-Aware Autoscaling

Запросы обрабатываются локально. Gateway отдаёт autoscaler только валидный трафик (authenticated, прошедший rate limit). Размещение срабатывает при устойчивом потоке — порог 5 valid rps за скользящее окно 1 минута.

**Scale UP:**
1. Gateway valid трафик на ноде без сервиса (>5 rps, окно 1m) → поднять копию
2. Процесс нагружен (CPU/Memory >75%) → добавить копию на ту же ноду
3. Ресурсы ноды кончились → ближайшая нода в том же DC
4. DC заполнен → ближайший DC по latency-матрице

**Scale DOWN:** удаление с наименее нагруженных нод, сохранение geo-diversity (min=3 в разных DC), cooldown 5m.

## Конфигурация

Переменные окружения с префиксом `A_`. Сервисы описываются файлами `.asty` (декларативный YAML).

## Деплой

Rolling update: canary → health check → promote (max_parallel) → auto_revert при неудаче.

## Web UI

Встроенная админка на `127.0.0.1:4646` (SSH-tunnel). Ноды, сервисы, деплой, логи, алерты.

## Setup новой ноды

```bash
wget -qO- https://raw.githubusercontent.com/org/asty/main/setup.sh | \
  A_DOMAIN=nodes.example.com \
  A_DATACENTER=eu-west \
  bash
```

## Development

See `dev-docs/` for technical documentation, implementation plan, and architecture details.
