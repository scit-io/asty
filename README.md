# Asty

Оркестратор микросервисов с locality-aware autoscaling. Заточен под NATS-платформу. Заменяет Nomad — берёт только то что реально используется, добавляет умное масштабирование.

## Зачем

Nomad не умеет размещать копии сервисов с учётом того, где реальная нагрузка. При нагрузке в Москве может отправить новую копию в Сидней. Asty решает **где** и **сколько** копий запускать, ориентируясь на locality — чтобы запросы обрабатывались на той же ноде, где Gateway принял трафик.

## Архитектура

```
Клиент → LB (geo) → Gateway (:80) → NATS (127.0.0.1:4222) → Сервис (та же нода)
```

**На каждой ноде:**
- NATS — транспорт, часть кластера
- Asty Agent — управление процессами, метрики, health checks
- Gateway — всегда (type=system)
- Сервисы — от 0 до N копий (autoscaler решает)

**Один на кластер:**
- Asty Server — scheduling, autoscaling, Web UI, деплой

Agent и Server — один бинарник. Server выбирается через leader election в NATS.

### Коммуникация

Вся коммуникация Agent ↔ Server через NATS (уже есть в стеке). Subjects: `asty.v1.*`. Состояние кластера хранится в NATS JetStream KV.

### Обнаружение кластера

Через DNS — A-записи домена (`A_DOMAIN`). Агент при старте резолвит домен, находит другие ноды, подключается к NATS. Идентично подходу platform.go.

---

## Управление процессами

### Что Asty делает с процессами

**Запуск:**
- Скачивание бинарника по URL + проверка SHA256 checksum
- Запуск от непривилегированного пользователя (`asty`)
- Передача переменных окружения
- Gateway запускается от root (bind на порт 80)

**Жизненный цикл:**
- Health check (HTTP GET по URL, interval + timeout)
- Перезапуск при падении (attempts, interval, delay между попытками)
- Graceful shutdown: SIGTERM → ожидание kill_timeout → SIGKILL

**Логи:**
- stdout/stderr процесса пишутся в файл
- Ротация: max_files × max_file_size

**Ресурсы:**
- CPU и Memory limits per process (cgroups v2)
- Резервирование ресурсов ноды для OS и системных демонов

### Типы сервисов

**system** — копия на каждой ноде автоматически. Для Gateway и подобных сервисов, которые принимают входящий трафик напрямую.

**service** — управляется autoscaler. Количество и размещение копий определяется нагрузкой.

---

## Locality-Aware Autoscaling

### Принцип

Платформа всегда быстрая и отказоустойчивая. Запросы обрабатываются локально. Ресурсы используются максимально.

### Базовая избыточность

- Минимум 3 копии каждого сервиса (type=service) в разных datacenter
- При падении одной из базовых — немедленное восстановление без cooldown
- Размещение восстановленной копии в другом DC (geo-diversity)

### Gateway метрики — главный индикатор

Gateway на каждой ноде. Если Gateway принимает трафик — на ноде должны быть сервисы. Сам факт трафика = повод для размещения, не нужно ждать перегрузки. Юзер не должен ждать пока нагрузка "дорастёт до порога" — он уже получает +250ms из-за inter-DC hop.

### Логика Scale UP

1. **Gateway трафик на ноде без сервиса** → сразу поднять копию на этой ноде
2. **Процесс нагружен** (CPU >75% или Memory >75%) → добавить копию на ту же ноду (если есть ресурсы)
3. **Ресурсы ноды кончились** → ближайшая нода в том же datacenter
4. **DC заполнен** → ближайший DC по latency-матрице
5. **Несколько нод нагружены** → масштабировать параллельно на все

Несколько копий одного сервиса на одной ноде — нормально. Задействовать все доступные ресурсы.

### Логика Scale DOWN

1. Все процессы сервиса ниже target и count > min(3)
2. Удалять копии с наименее нагруженных нод
3. Сохранять geo-diversity (min=3 в разных DC)
4. Cooldown 5 минут между удалениями

### Параметры

| Параметр | Значение | Описание |
|----------|----------|----------|
| Min copies | 3 | Базовая избыточность в разных DC |
| Max copies | unlimited | В рамках свободных ресурсов кластера |
| Target CPU | 75% | Порог для добавления копии |
| Target Memory | 75% | Порог для добавления копии |
| Cooldown UP | 30s | Агрессивное масштабирование |
| Cooldown DOWN | 5m | Осторожное уменьшение |
| Eval interval | 10s | Частота опроса метрик |

### Datacenter proximity

Статичная latency-матрица в конфиге. Фоновые ping-замеры раз в час для валидации. При расхождении >50% — alert оператору. Используется конфиг, не runtime (защита от сетевых флуктуаций).

---

## Деплой сервисов

### Rolling Update

1. Создаётся 1 копия новой версии (canary)
2. Health check canary
3. Если ок — promote: старые копии обновляются по одной (max_parallel)
4. Между обновлениями ожидание min_healthy_time
5. Если canary или обновлённая копия не проходит health check за healthy_deadline — auto_revert на предыдущую версию

Во время обновления сервис доступен — старые копии продолжают работать.

### Artifact

- URL бинарника (GitHub Releases, S3, любой HTTP)
- SHA256 checksum (защита от подмены)
- Архитектура: amd64 / arm64 (определяется автоматически на ноде)

### CI/CD интеграция

platform.go CI собирает бинарники → GitHub Release → Asty Server получает новую версию (webhook или polling) → rolling update через агентов.

---

## Web UI (Админка)

Встроенная, на том же порту что API. Только loopback (127.0.0.1), доступ через SSH-tunnel.

### Дашборд
- Карта нод: IP, datacenter, CPU/Memory (текущее/доступное), статус
- Карта сервисов: имя, число копий, распределение по нодам
- Визуализация: какой сервис на какой ноде, сколько копий, нагрузка

### Сервисы
- Каждая копия: нода, CPU/Memory, health, версия, uptime
- История scaling: когда добавлена/удалена копия, причина, нода
- Ручное управление: добавить/удалить копию, указать ноду

### Деплой
- Текущая версия каждого сервиса
- Rolling update прогресс
- Canary: promote / revert
- Rollback на предыдущую версию

### Логи
- stdout/stderr каждой копии (tail -f, streaming)
- Фильтрация по сервису, ноде, уровню

### Алерты
- Кластер на пределе ресурсов
- Нода недоступна
- Health check failed
- Autoscaler не может разместить копию (нет ресурсов)

---

## Конфигурация

### Переменные окружения (префикс `A_`)

**Кластер:**

| Переменная | Описание | Пример |
|------------|----------|--------|
| `A_DOMAIN` | DNS домен кластера | `nodes.example.com` |
| `A_DATACENTER` | Имя DC текущей ноды | `eu-west` |
| `A_TOKEN` | Токен авторизации API | `uuid` |
| `A_LOG_LEVEL` | Уровень логирования | `info` |

**NATS (транспорт):**

| Переменная | Описание | Пример |
|------------|----------|--------|
| `A_NATS_HOST` | Адрес NATS | `127.0.0.1` |
| `A_NATS_PORT` | Порт NATS | `4222` |
| `A_NATS_USER` | Логин NATS | `nats` |
| `A_NATS_PASSWORD` | Пароль NATS | `secret` |

**Autoscaling:**

| Переменная | Описание | По умолчанию |
|------------|----------|-------------|
| `A_MIN_COPIES` | Минимум копий service-типа | `3` |
| `A_TARGET_CPU` | Целевой CPU (%) | `75` |
| `A_TARGET_MEMORY` | Целевая память (%) | `75` |
| `A_COOLDOWN_UP` | Cooldown scale up | `30s` |
| `A_COOLDOWN_DOWN` | Cooldown scale down | `5m` |
| `A_EVAL_INTERVAL` | Интервал опроса | `10s` |
| `A_DC_LATENCY` | Latency-матрица | `eu:us:100,eu:asia:250` |

**Ресурсы ноды:**

| Переменная | Описание | По умолчанию |
|------------|----------|-------------|
| `A_RESERVED_CPU` | Резерв CPU для OS (MHz) | `100` |
| `A_RESERVED_MEMORY` | Резерв RAM для OS (MB) | `250` |

**Админка:**

| Переменная | Описание | По умолчанию |
|------------|----------|-------------|
| `A_UI_ADDR` | Адрес Web UI | `127.0.0.1:4646` |

### Конфигурация сервиса

Каждый сервис описывается отдельным файлом (YAML):

```yaml
name: xauth
type: service           # service (autoscaling) или system (копия на каждой ноде)

artifact:
  url: https://github.com/org/repo/releases/download/${VERSION}/xauth_linux_${ARCH}.tar.gz
  checksum: sha256:abc123

command: ./xauth
user: asty
kill_timeout: 30s

env:
  PLATFORM_NATS_HOST: "127.0.0.1"
  PLATFORM_NATS_PORT: "4222"
  PLATFORM_NATS_USER: "${A_NATS_USER}"
  PLATFORM_NATS_PASSWORD: "${A_NATS_PASSWORD}"
  X_AUTH_USERNAME: "${X_AUTH_USERNAME}"
  X_AUTH_ACCESS_SECRET: "${X_AUTH_ACCESS_SECRET}"
  X_HEALTH_ADDR: "${ASTY_HEALTH_ADDR}"

resources:
  cpu: 100       # MHz
  memory: 32     # MB

health:
  type: http
  path: /healthz
  interval: 10s
  timeout: 3s

logs:
  max_files: 5
  max_file_size: 10  # MB

update:
  max_parallel: 1
  min_healthy_time: 10s
  healthy_deadline: 3m
  auto_revert: true
  canary: 1

restart:
  attempts: 10
  interval: 5m
  delay: 15s
```

---

## Безопасность

### Межнодовая коммуникация
- Asty Agent ↔ Server через NATS → шифрование обеспечивается NATS mTLS (порт 6222)
- Клиентское подключение к NATS: user/password авторизация
- Между нодами не нужен отдельный RPC-порт (в отличие от Nomad 4647/4648)

### API
- Web UI и API на loopback (127.0.0.1) — доступ только через SSH-tunnel
- Авторизация по токену (`A_TOKEN`)

### Процессы
- Сервисы запускаются от непривилегированного пользователя (`asty`)
- Gateway от root (bind на порт 80)
- Credentials в /etc/asty/env (chmod 600)
- Секреты передаются через env, не через аргументы командной строки

### Firewall
- Порт 80/TCP — Gateway (публичный)
- Порт 4222/TCP — NATS клиент (между нодами)
- Порт 6222/TCP — NATS кластер (между нодами)
- Порт 4646/TCP — Asty UI/API (только loopback)

Nomad RPC (4647) и Serf (4648) больше не нужны.

---

## Технический стек

- **Язык:** Go
- **Logging:** zerolog (JSON в stderr)
- **Metrics:** Prometheus (собственные + go_*, process_*)
- **Коммуникация Agent ↔ Server:** NATS subjects `asty.v1.*`
- **State:** NATS JetStream KV
- **Web UI:** встроенный HTTP-сервер + фронтенд (технология TBD)
- **Process management:** exec.Cmd + cgroups v2

---

## Структура проекта

```
asty/
  cmd/
    asty/main.go              — единый бинарник (agent + server)

  internal/
    agent/
      agent.go                — lifecycle агента на ноде
      process.go              — запуск/остановка/restart процессов
      health.go               — HTTP health checks
      collector.go            — сбор CPU/Memory метрик per process
      logs.go                 — ротация логов (stdout/stderr → файл)
      artifact.go             — скачивание бинарника + checksum

    server/
      server.go               — lifecycle сервера
      scheduler.go            — locality-aware placement
      autoscaler.go           — scaling decisions (UP/DOWN/min enforcement)
      deployer.go             — rolling update, canary promote/revert
      leader.go               — leader election через NATS JetStream

    cluster/
      discovery.go            — обнаружение нод через DNS
      state.go                — состояние кластера в NATS JetStream KV
      proximity.go            — datacenter latency matrix + ping validation

    config/
      config.go               — загрузка A_* переменных
      service.go              — парсинг YAML-определений сервисов

    api/
      api.go                  — HTTP API (REST)
      handlers.go             — обработчики: nodes, services, deploy, logs

    ui/
      ui.go                   — встроенный Web UI
      embed.go                — embedded static files (go:embed)

  deployments/
    systemd/
      asty.service            — systemd unit

  services/                   — определения сервисов (YAML)
    gateway.yaml
    xauth.yaml
    xhttp.yaml
    xws.yaml
```

---

## Setup новой ноды

Один скрипт, одна команда:

```bash
wget -qO- https://raw.githubusercontent.com/org/asty/main/setup.sh | \
  A_DOMAIN=nodes.example.com \
  A_DATACENTER=eu-west \
  A_NATS_USER=nats \
  A_NATS_PASSWORD=secret \
  A_TOKEN=uuid \
  bash
```

Скрипт:
1. Определяет IP ноды
2. Настраивает swap
3. Устанавливает NATS (standalone, кластеризация через отдельный workflow)
4. Скачивает бинарник Asty
5. Создаёт systemd unit + /etc/asty/env (chmod 600)
6. Настраивает firewall (ufw)
7. Создаёт пользователя `asty` (непривилегированный)
8. Запускает Asty Agent
9. Agent находит кластер через DNS, подключается к NATS

---

## Мониторинг

### Метрики Asty (Prometheus)

**Кластер:**
- `asty_nodes_total` — число нод
- `asty_nodes_healthy` — число здоровых нод

**Сервисы:**
- `asty_service_copies{service,node,datacenter}` — число копий
- `asty_service_cpu_percent{service,node}` — CPU per process
- `asty_service_memory_percent{service,node}` — Memory per process

**Autoscaler:**
- `asty_scaling_actions_total{service,action,reason}` — scaling decisions
- `asty_scaling_cooldown_active{service}` — cooldown статус

**Health:**
- `asty_health_check_duration_seconds{service,node}` — latency health check
- `asty_health_check_failures_total{service,node}` — число провалов

### Endpoint

```
GET http://127.0.0.1:4646/metrics
```

---

## Тестирование

### Unit-тесты
- scheduler_test.go: locality placement при разных сценариях
- autoscaler_test.go: scaling decisions, cooldown, min enforcement
- deployer_test.go: rolling update, canary
- artifact_test.go: скачивание + checksum

### Integration-тесты (embedded NATS)
- 3 агента в одном процессе
- Симуляция нагрузки → проверка locality placement
- Падение ноды → восстановление min=3
- Rolling update → zero downtime

### E2E-тесты
- Dev-окружение 3 ноды
- Полный цикл: деплой → нагрузка → autoscaling → scale down
