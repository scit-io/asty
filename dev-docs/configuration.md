# Конфигурация Asty

## Переменные окружения (префикс `A_`)

### Кластер

| Переменная | Описание | Пример |
|---|---|---|
| `A_DOMAIN` | DNS домен кластера | `nodes.example.com` |
| `A_DATACENTER` | Имя DC текущей ноды | `eu-west` |
| `A_TOKEN` | Токен авторизации API | `uuid` |
| `A_LOG_LEVEL` | Уровень логирования | `info` |

### NATS (транспорт)

| Переменная | Описание | Пример |
|---|---|---|
| `A_NATS_HOST` | Адрес NATS | `127.0.0.1` |
| `A_NATS_PORT` | Порт NATS | `4222` |
| `A_NATS_USER` | Логин NATS | `nats` |
| `A_NATS_PASSWORD` | Пароль NATS | `secret` |

### Autoscaling

| Переменная | Описание | По умолчанию |
|---|---|---|
| `A_MIN_COPIES` | Минимум копий service-типа | `3` |
| `A_TARGET_CPU` | Целевой CPU (%) | `75` |
| `A_TARGET_MEMORY` | Целевая память (%) | `75` |
| `A_COOLDOWN_UP` | Cooldown scale up | `30s` |
| `A_COOLDOWN_DOWN` | Cooldown scale down | `5m` |
| `A_EVAL_INTERVAL` | Интервал опроса | `10s` |
| `A_DC_LATENCY` | Latency-матрица | `eu:us:100,eu:asia:250` |
| `A_TRAFFIC_RPS_THRESHOLD` | Минимум valid rps для размещения | `5` |
| `A_TRAFFIC_WINDOW` | Скользящее окно оценки трафика | `1m` |

### Ресурсы ноды

| Переменная | Описание | По умолчанию |
|---|---|---|
| `A_RESERVED_CPU` | Резерв CPU для OS (MHz) | `100` |
| `A_RESERVED_MEMORY` | Резерв RAM для OS (MB) | `250` |

### Админка

| Переменная | Описание | По умолчанию |
|---|---|---|
| `A_UI_ADDR` | Адрес Web UI | `127.0.0.1:4646` |

## Конфигурация сервиса (.asty)

Каждый сервис описывается файлом `services/<name>.asty` (YAML):

```yaml
name: xauth
type: service

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

## Firewall

| Порт | Назначение |
|---|---|
| 80/TCP | Gateway (публичный) |
| 4222/TCP | NATS клиент (между нодами) |
| 6222/TCP | NATS кластер (между нодами) |
| 4646/TCP | Asty UI/API (только loopback) |

## Безопасность

- Agent ↔ Server через NATS → шифрование NATS mTLS (порт 6222)
- Клиентское подключение к NATS: user/password
- Web UI/API на loopback — доступ через SSH-tunnel
- Авторизация по токену (`A_TOKEN`)
- Сервисы от непривилегированного пользователя (`asty`), Gateway от root
- Credentials в `/etc/asty/env` (chmod 600)
- Секреты через env, не через аргументы CLI
