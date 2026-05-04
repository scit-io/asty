# Development Documentation

Эта директория содержит рабочую документацию для разработки Asty. Здесь можно упоминать технические детали, происхождение из Nomad, связь с platform.go.

## Файлы

- `architecture.md` — откуда берём код, что из Nomad используется, маппинг конфигов
- `autoscaling.md` — locality-aware autoscaling алгоритм, фильтрация ботов
- `configuration.md` — все переменные A_*, формат .asty, firewall, безопасность
- `monitoring.md` — метрики Prometheus, Web UI, тестирование

## Текущий статус проекта

### Инфраструктура (готово)

- ✅ Структура проекта: `cmd/asty/main.go`, `internal/platform/asty/`
- ✅ Go модуль с зависимостями (NATS, zerolog, yaml)
- ✅ Конфигурация через переменные A_*
- ✅ Примеры .asty файлов (gateway, xauth)
- ✅ Лендинг docs/index.html

### Базовые компоненты (завершено)

- ✅ Config loader (config.go)
- ✅ Service definition parser (service.go)
- ✅ Agent with full lifecycle (agent.go)
- ✅ Server skeleton (server.go)
- ✅ NATS connection management
- ✅ Process management (process.go) - start/stop, graceful shutdown
- ✅ Health checks (health.go) - HTTP probes with periodic checking
- ✅ Metrics collector (collector.go) - CPU/Memory from /proc
- ✅ Artifact downloader (artifact.go) - tar.gz extraction with SHA256 verification

### Оркестрация (завершено)

- ✅ DNS discovery (discovery.go) - A-record resolution, change detection
- ✅ Leader election (leader.go) - TTL-based via JetStream KV
- ✅ State management (state.go) - NATS JetStream KV, node/allocation CRUD
- ✅ Scheduler (scheduler.go) - system/service placement, geo-diversity, proximity-aware
- ✅ Command protocol (commands.go) - agent↔server communication
- ✅ Autoscaler (autoscaler.go) - locality-aware scaling decisions, cooldown tracking
- ✅ Deployer (deployer.go) - rolling updates, canary, auto-revert
- ✅ Proximity matrix (proximity.go) - DC latency, nearest DC selection
- ✅ Service loader (loader.go) - load .asty definitions from directory

### Операции (TODO)

- ⏳ Artifact downloader (artifact.go)
- ⏳ Log rotation (logs.go)
- ⏳ DC proximity (proximity.go)
- ⏳ HTTP API (api.go, handlers.go)
- ⏳ Web UI (ui.go)

## План разработки

### Фаза 1: Базовое управление процессами

1. Копировать process management из platform.go (если есть готовый код для raw_exec)
2. Адаптировать под .asty конфиги
3. Реализовать health checks
4. Реализовать метрики (CPU/Memory per process)
5. Реализовать log rotation

**Результат:** Agent может запускать/останавливать процессы, мониторить здоровье.

### Фаза 2: Кластеризация

1. DNS discovery (retry_join логика из Nomad)
2. Leader election через NATS JetStream
3. State management в NATS KV
4. Agent ↔ Server коммуникация через NATS subjects `asty.v1.*`

**Результат:** Несколько нод видят друг друга, выбирают лидера, синхронизируют состояние.

### Фаза 3: Базовый scheduler

1. System scheduler: копия на каждой ноде (gateway)
2. Service scheduler: min 3 копии в разных DC
3. Простое размещение без locality (round-robin по нодам)

**Результат:** Можно задеплоить gateway (system) и xauth (service).

### Фаза 4: Locality-aware autoscaler

1. Gateway метрики (valid_rps per node)
2. Process метрики (CPU/Memory per instance)
3. Логика scale up: traffic threshold + CPU/Memory overload
4. Логика scale down: cooldown + geo-diversity
5. DC proximity matrix

**Результат:** Автоматическое размещение сервисов на нодах с трафиком.

### Фаза 5: Деплой

1. Artifact downloader с checksum
2. Rolling update (canary → promote)
3. Health check during deploy
4. Auto-revert on failure

**Результат:** Zero-downtime deployments.

### Фаза 6: Наблюдаемость

1. Prometheus metrics endpoint
2. HTTP API (REST)
3. Web UI (встроенный, loopback)
4. Structured logging (zerolog JSON)

**Результат:** Мониторинг и управление через UI.

## Источники кода

При реализации смотреть в ../platform.go на готовые куски:
- `internal/platform/nc/` — NATS client wrapper
- `internal/platform/logger/` — zerolog setup
- `internal/platform/metrics/` — Prometheus metrics

Из Nomad брать только то, что реально используется (см. architecture.md).
