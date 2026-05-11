# SDK-пакеты: анализ и план переноса в Asty

## Текущее состояние

Четыре пакета обслуживают исключительно деплоимые сервисы (xauth, xhttp, xws) и не импортируются ни одним файлом внутри `internal/platform/asty/`:

| Пакет | Строк | Назначение |
|-------|-------|------------|
| `internal/platform/nc` | 565 | NATS-клиент: connect, auth, reconnect, KV CRUD, KV bucket init с авто-репликацией, health probe, drain |
| `internal/platform/logger` | 42 | Фабрика zerolog с полем `service` + уровень из `A_LOG_LEVEL` |
| `internal/middleware` | 155 | `Recover` (panic → 500) + `RequireAuth` (JWT из cookie → X-Auth-Sub header) |
| `utils/` | 268 | `Reply`/`ReplyError` (NATS-ответ с Status header), `GetCookie`, `BuildSetCookie`, `GetEnv`, `ValidateHosts` |

Asty-оркестратор использует собственные реализации: `core/netutil` (NATS connect, KV ensure), `log/slog` (stdlib), gateway сам ставит CORS/headers. Пересечений нет.

## Протокольный контракт платформы

Сервис работает на Asty если выполняет три условия:

1. **NATS QueueSubscribe** на `api.v1.{service}.{method}` — gateway доставляет запросы
2. **HTTP 200** на `health.addr + health.path` из `.asty` файла — agent проверяет
3. **Graceful exit** по SIGTERM за `kill_timeout` — agent ждёт, потом SIGKILL

SDK **не является частью контракта**. Сервис на любом языке/фреймворке работает, если соблюдает три пункта выше.

## Что SDK делает сверх контракта

### 1. KV bucket init с авто-репликацией (`initKV`) — НЕТРИВИАЛЬНО

```
DiscoveredServers() → replicas = min(cluster_size, 3)
CreateKeyValue(replicas=N)
  └─ err_code=10005? → replicas-- → retry
  └─ ErrBucketExists? → просто подключиться
```

Операционное знание:
- `Servers()` нельзя — seed URL дублируется с advertise, даёт завышенный count
- R=5 ломается на 6+ нодах — meta-raft не успевает сформировать placement group
- При первом холодном старте кластера placement может не работать секунды — нужен retry с degradation
- Деградация на 2+ шага = Error log (единственный сигнал оператору)

**Кто должен это делать:** оркестратор (server), а не сервис. Сервис не должен знать про topology кластера. Server создаёт бакеты при деплое (или при первом старте), сервис просто пишет/читает.

### 2. NATS self-ping health (`RegisterHealth`) — ПЕРЕНОСИМО В AGENT

Текущая схема:
```
Agent --HTTP GET--> Service /healthz --NATS self-ping--> Service subscription
```

Цель: поймать deadlock в dispatcher'е сервиса (когда NATS TCP PING проходит, но handler висит).

Проблема: сервис сам поднимает HTTP-сервер, сам пингует себя. Agent видит только HTTP 200/503.

Альтернатива со стороны agent:
```
Agent --NATS Request--> _probe.{service}.{nuid} --> Service handler responds
```

Agent сам шлёт NATS request через тот же dispatcher что и бизнес-подписки. Если нет ответа за 1s — deadlock. Сервису нужна одна строка:
```go
conn.Subscribe("_probe."+serviceName+"."+nuid, func(m *nats.Msg) { m.Respond(nil) })
```

Но это всё ещё сторона сервиса. Agent может:
- Добавить в `.asty` формат поле `health.type: nats` (vs текущий `http`)
- При `type: nats` — agent сам шлёт NATS request на probe subject, не нуждаясь в HTTP-сервере

### 3. `Recover` middleware — НУЖЕН СЕРВИСАМ, НЕ ASTY

NATS Go SDK не восстанавливается из panic в callback. Без обёртки одна паника роняет весь процесс. Это 30 строк, но они критичны. Каждый продовый сервис на Go будет нуждаться в этом.

**Кто должен это делать:** сервис (или его SDK). Gateway/agent не могут ловить панику внутри чужого процесса.

### 4. `Reply`/`ReplyError` — протокольные хелперы

Кодифицируют протокол ответа: JSON body + Header `Status` + Header `Content-Type` + Header `Set-Cookie`. Gateway читает именно эту конвенцию. Без хелпера каждый handler вручную собирает `nats.Msg` с правильными headers.

**Кто должен это делать:** SDK для Go-сервисов. Протокол определён gateway; хелперы — reference implementation.

### 5. `GetCookie`/`BuildSetCookie` — HTTP↔NATS cookie bridge

Gateway кладёт HTTP Cookie в NATS header. Сервис парсит куку из NATS msg и собирает Set-Cookie обратно. Без хелпера — ручной парсинг RFC 6265.

**Кто должен это делать:** SDK для Go-сервисов.

### 6. `RequireAuth` — JWT middleware

Проверяет access_token из cookie, ставит `X-Auth-Sub` header. Локальная проверка (HMAC + Exp), без сетевых вызовов. Используется xhttp и xws для защиты эндпоинтов.

**Кто должен это делать:** SDK для Go-сервисов. Часть платформенного auth-контракта — сервисы на платформе используют единый механизм проверки access token.

### 7. Reconnect + Drain — boilerplate

4 callback'а reconnect-логирования + `Drain(timeout)` обёртка. Стандартный код из nats-go docs. Полезен, но не содержит платформенного знания.

## Что можно и стоит перенести в Asty

### A. Создание KV бакетов → Server при деплое

Сейчас: каждый сервис сам создаёт свой бакет при старте (xauth → `authms_refresh_tokens`, xhttp → `xhttp_cache`).

Предложение: добавить секцию `kv:` в `.asty` файл сервиса (рядом с `health:`, `resources:` и т.д.):

```yaml
# deployments/infra/xauth.asty (добавляется секция kv)
kv:
  - bucket: authms_refresh_tokens
    history: 1
```

Server при деплое/старте аллокации:
1. Создаёт бакет через свой `EnsureBucket` (с retry, backoff)
2. Выставляет replicas автоматически (у server уже есть знание о topology)
3. Пробрасывает имя бакета в env сервиса

Сервис подключается к готовому бакету через `js.KeyValue(ctx, bucketName)` — одна строка stdlib, без SDK.

**Выигрыш:**
- Сервис не знает про replicas, placement errors, cluster topology
- Нет restart-loop при холодном старте
- Server контролирует все бакеты в кластере (observability, backup, migration)
- Удаляется 100 строк из SDK (`initKV`, `Config.KV`, авто-replicas)

### B. Health probe через NATS → Agent

Добавить `health.type: nats` в `.asty`:

```yaml
health:
  type: nats        # agent сам пингует через NATS dispatcher
  interval: 10s
  timeout: 1s
```

Agent подписывает probe subject при запуске сервиса. Сервис добавляет одну строку: `conn.Subscribe(probeSubject, respond)`. Agent генерирует probe subject и передаёт через env (`A_PROBE_SUBJECT`).

**Выигрыш:**
- Убирает RegisterHealth + HTTP-сервер из сервиса
- Ловит deadlock так же эффективно (request идёт через dispatcher)
- Agent контролирует probe lifecycle целиком

### C. Протокольные хелперы → отдельный лёгкий модуль

`Reply`, `ReplyError`, `GetCookie`, `BuildSetCookie`, `Recover` — это reference implementation протокола gateway↔service. ~150 строк.

Варианты:
1. **Отдельный Go-модуль** (`github.com/.../asty-go`) — сервисы в отдельных репо могут импортировать
2. **Встроить в документацию** — протокол простой, каждый пишет сам на своём языке
3. **Оставить в `utils/`** — пока все сервисы в одном модуле, работает

Рекомендация: вариант 1, когда появится первый внешний сервис. До тех пор — оставить.

## Что удалить

| Компонент | Причина |
|-----------|---------|
| `internal/platform/logger` | 42 строки zerolog-фабрики. Сервис подключает свой логгер. Asty использует slog. |
| `internal/platform/metrics` | Уже удалён (`git status: D`). Зафиксировать. |
| `nc.GetValue` / `PutValue` / `Delete` / `WatchKey` | Тривиальные обёртки: `kv.Get(ctx, key)` — stdlib jetstream. 5 строк каждый. |
| `nc.Close()` | `conn.Close()` — одна строка. |
| `nc.Drain(timeout)` | `conn.Drain()` + select с timer — 10 строк в main.go. |
| `nc.Config` / `DefaultConfig()` | Сервису нужны host, port, user, password. 4 env-переменные. Дефолты (reconnect=-1, wait=2s) ставятся при connect. |
| `nc.Config.KV` + `initKV` | Создание бакетов переезжает в server. Конфигурация replicas — не забота сервиса. |

## Итоговая картина

### До (сейчас)

```
Сервис → SDK (nc.NewClient, initKV, RegisterHealth, Drain)
       → utils (Reply, GetCookie, BuildSetCookie)
       → middleware (Recover, RequireAuth)
```

Сервис обязан знать: cluster topology, replicas, placement errors, health probe architecture.

### После

```
Asty Server → создаёт KV бакеты при деплое (topology — его забота)
Asty Agent  → NATS health probe (dispatcher deadlock detection — его забота)
Сервис      → nats.Connect + QueueSubscribe + простой handler + exit on SIGTERM
            → опционально: asty-go модуль для Reply/Cookie/Recover/RequireAuth хелперов
```

Сервис знает только: subject name, bucket name (из env), свою бизнес-логику.

## Порядок реализации

1. Добавить `kv:` секцию в `.asty` формат + server создаёт бакеты при деплое
2. Добавить `health.type: nats` в agent + передача probe subject через env
3. Удалить `internal/platform/nc`, `internal/platform/logger`, `internal/platform/metrics`
4. Перенести `utils/reply.go` + `utils/cookie.go` + `middleware/recover.go` + `middleware/xauth.go` в `pkg/astygo/` (или оставить пока сервисы в monorepo)
5. Обновить x-сервисы: прямой `nats.Connect`, читать bucket name из env, подписка на probe subject
