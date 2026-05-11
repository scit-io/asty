# Рефакторинг: разделение на asty + demo

## Цель

Разделить монолит на две части:
- `asty/` — оркестратор (server + agent + gateway)
- `demo/` — демо-сервисы, используют `nats.go/micro` напрямую

SDK не нужен. `nats.go/micro` даёт всё из коробки.

---

## Почему не SDK

Пакет `github.com/nats-io/nats.go/micro` (уже в go.mod) предоставляет:
- Автоматические health/ping/info/stats endpoints
- Queue groups по имени сервиса
- Request-Reply с headers
- Discovery без кода

Сервис подключается напрямую:

```go
nc, _ := nats.Connect("nats://127.0.0.1:4222")

srv, _ := micro.AddService(nc, micro.Config{
    Name:    "xauth",
    Version: "1.0.0",
})

srv.AddEndpoint("login", micro.HandlerFunc(func(req micro.Request) {
    var body LoginRequest
    json.Unmarshal(req.Data(), &body)
    // бизнес-логика
    req.Respond(responseBytes, micro.WithHeaders(micro.Headers{"Status": []string{"200"}}))
}))
```

Что остаётся за пределами `micro`:
- Cookie-парсинг из NATS header — ~10 строк, копируется в сервис
- Status header конвенция — одна строка в `req.Respond()`
- JWT проверка — бизнес-логика сервиса
- KV bucket creation — ответственность server'а (см. ниже)

Это не тянет на пакет. Каждый сервис просто знает конвенцию.

---

## KV бакеты: server создаёт при деплое

### Проблема

Раньше SDK (`initKV`) при старте сервиса:
1. Вызывал `DiscoveredServers()` → определял размер кластера
2. Рассчитывал `replicas = min(cluster_size, 3)`
3. Создавал bucket с retry и деградацией (R=3 → R=2 → R=1) при placement errors
4. Логировал деградацию как единственный сигнал оператору

Операционное знание зашитое в SDK:
- `Servers()` нельзя — seed URL дублируется с advertise, даёт завышенный count
- R=5 ломается на 6+ нодах — meta-raft не успевает сформировать placement group
- При холодном старте кластера placement может не работать секунды

Сервис не должен этим заниматься. Без SDK — эту работу берёт server.

### Решение: секция `kv:` в `.asty` файле

```yaml
name: xauth
type: service

kv:
  - bucket: authms_refresh_tokens
    history: 1
    replicas: 3               # явно задаёт число реплик (0 или отсутствует = auto)
  - bucket: xauth_sessions
    history: 1
    ttl: 24h
    # replicas не указан → auto (min(cluster_size, 3))

# ...
```

### Что делает server при деплое

1. Читает `.asty` → видит секцию `kv:`
2. Для каждого bucket:
   - Если `replicas` указан в `.asty` — использует его как есть
   - Если `replicas` = 0 или не указан — auto: определяет через `DiscoveredServers()` (полный `connect_urls` из NATS INFO, все пиры кластера включая self). `Servers()` использовать нельзя: seed URL и advertise не совпадают, дают завышенный count. Single-node без кластера: INFO без connect_urls → 0 → 1
   - Вызывает `CreateKeyValue(bucket, replicas, history, storage=FileStorage)`
   - При `err_code=10005` (no suitable peers for placement) — понижает R на 1 и пробует снова, до R=1. Причина: JetStream meta-cluster может включать меньше нод, чем route-level, особенно первые секунды после старта
   - При `ErrBucketExists` — подключается к существующему (идемпотентность)
   - Деградация на 1 шаг — Info (типичный first-start race). Деградация на 2+ шага — Error (misconfig, единственный сигнал оператору)
3. Пробрасывает имена бакетов в env аллокации: `A_KV_AUTHMS_REFRESH_TOKENS=authms_refresh_tokens`
4. Только после успешного создания всех бакетов — запускает сервис

### Что делает сервис

Одна строка:

```go
js, _ := jetstream.New(nc)
kv, _ := js.KeyValue(context.Background(), os.Getenv("A_KV_AUTHMS_REFRESH_TOKENS"))
```

Сервис не знает про replicas, placement, topology. Подключается к готовому бакету.

### Edge cases

- **Бакет уже существует** — `EnsureBucket` идемпотентен (уже реализовано в `core/netutil`)
- **Бакет нужен нескольким сервисам** — оба объявляют в `.asty`, server создаёт один раз
- **TTL** — объявляется в `.asty`, server применяет при создании
- **Холодный старт кластера** — server ждёт и ретраит, сервис не стартует пока bucket не ready
- **Деградация replicas** — server логирует, оператор реагирует. Сервис не знает и не падает

---

## Текущая структура

```
/
├── cmd/
│   ├── asty/main.go
│   ├── xauth/main.go
│   ├── xhttp/main.go
│   └── xws/main.go
├── internal/
│   ├── platform/asty/            → оркестратор
│   ├── platform/nc/              → NATS client wrapper (лишний теперь)
│   ├── platform/logger/          → zerolog фабрика (лишняя)
│   ├── middleware/               → recover + xauth
│   └── services/
│       ├── xauth/
│       ├── xhttp/
│       └── xws/
├── utils/                        → reply, cookie, env, hosts
└── go.mod
```

---

## Целевая структура

```
/
├── asty/
│   ├── cmd/main.go               ← из cmd/asty/main.go
│   └── internal/                 ← из internal/platform/asty/*
│       ├── core/
│       │   ├── config/
│       │   ├── types/
│       │   ├── errors/
│       │   └── netutil/
│       ├── features/
│       │   ├── api/
│       │   ├── autoscaling/
│       │   ├── clustering/
│       │   ├── deployment/
│       │   ├── draining/
│       │   ├── execution/
│       │   ├── gateway/          ← hosts.go переезжает сюда
│       │   ├── observability/
│       │   └── scheduling/
│       ├── server/
│       ├── agent/
│       └── testutil/
├── demo/
│   ├── cmd/
│   │   ├── xauth/main.go        ← переписан на nats.go/micro
│   │   ├── xhttp/main.go
│   │   └── xws/main.go
│   └── internal/
│       ├── xauth/                ← из internal/services/xauth/
│       ├── xhttp/
│       └── xws/
├── go.mod
└── ...
```

---

## Шаг 1: Перенос оркестратора в `asty/`

| Откуда | Куда |
|--------|------|
| `internal/platform/asty/core/` | `asty/internal/core/` |
| `internal/platform/asty/features/` | `asty/internal/features/` |
| `internal/platform/asty/server/` | `asty/internal/server/` |
| `internal/platform/asty/agent/` | `asty/internal/agent/` |
| `internal/platform/asty/testutil/` | `asty/internal/testutil/` |
| `cmd/asty/main.go` | `asty/cmd/main.go` |
| `utils/hosts.go` | `asty/internal/features/gateway/hosts.go` |

Import paths: `asty/internal/platform/asty/...` → `asty/asty/internal/...`

---

## Шаг 2: Перенос демо-сервисов в `demo/`

| Откуда | Куда |
|--------|------|
| `cmd/xauth/main.go` | `demo/cmd/xauth/main.go` |
| `cmd/xhttp/main.go` | `demo/cmd/xhttp/main.go` |
| `cmd/xws/main.go` | `demo/cmd/xws/main.go` |
| `internal/services/xauth/` | `demo/internal/xauth/` |
| `internal/services/xhttp/` | `demo/internal/xhttp/` |
| `internal/services/xws/` | `demo/internal/xws/` |

---

## Шаг 3: Переписать demo на `nats.go/micro`

### До (xauth/main.go сейчас)

```go
package main

import (
    "asty/internal/middleware"
    "asty/internal/platform/logger"
    "asty/internal/platform/nc"
    "asty/internal/services/xauth"
    "asty/utils"
)

func main() {
    log := logger.New("xauth")
    cfg := xauth.LoadConfig(log)
    natsClient := nc.NewClient(cfg.NATS, log)
    defer natsClient.Drain(5 * time.Second)

    natsClient.RegisterHealth("xauth", cfg.HealthAddr)

    h := xauth.NewHandlers(cfg, natsClient, log)

    natsClient.QueueSubscribe("api.v1.xauth.login", "xauth",
        middleware.Recover(log, h.HandleLogin))
    natsClient.QueueSubscribe("api.v1.xauth.refresh", "xauth",
        middleware.Recover(log, h.HandleRefresh))
    natsClient.QueueSubscribe("api.v1.xauth.logout", "xauth",
        middleware.Recover(log, middleware.RequireAuth(cfg.Auth, h.HandleLogout)))

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
    <-sig
}
```

### После (demo/cmd/xauth/main.go)

```go
package main

import (
    "os"
    "os/signal"
    "syscall"

    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/micro"
)

func main() {
    nc, _ := nats.Connect(os.Getenv("A_NATS_URL"))
    defer nc.Drain()

    h := NewHandlers(nc)

    srv, _ := micro.AddService(nc, micro.Config{
        Name:    "xauth",
        Version: "1.0.0",
    })

    srv.AddEndpoint("login", micro.HandlerFunc(h.Login))
    srv.AddEndpoint("refresh", micro.HandlerFunc(h.Refresh))
    srv.AddEndpoint("logout", micro.HandlerFunc(h.Logout))
    srv.AddEndpoint("me", micro.HandlerFunc(h.Me))

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
    <-sig
}
```

### Handler (после)

```go
func (h *Handlers) Login(req micro.Request) {
    var body struct {
        Username string `json:"username"`
        Password string `json:"password"`
    }
    if err := json.Unmarshal(req.Data(), &body); err != nil {
        req.Error("400", "invalid json", nil)
        return
    }

    if !h.validateCredentials(body.Username, body.Password) {
        req.Error("401", "invalid credentials", nil)
        return
    }

    access, refresh := h.issueTokens(body.Username)

    headers := micro.Headers{
        "Status":     []string{"200"},
        "Set-Cookie": []string{
            buildCookie("access_token", access, 900),
            buildCookie("refresh_token", refresh, 86400),
        },
    }
    resp, _ := json.Marshal(map[string]string{"status": "ok"})
    req.Respond(resp, micro.WithHeaders(headers))
}
```

Ноль платформенных импортов. Только `nats.go` + `nats.go/micro` + stdlib.

---

## Шаг 4: Удаление старого кода

| Путь | Причина |
|------|---------|
| `internal/platform/nc/` | Заменён на `nats.go/micro` |
| `internal/platform/logger/` | Сервис сам выбирает логгер |
| `internal/middleware/` | recover — в каждом handler (defer); auth — бизнес-логика сервиса |
| `utils/reply.go` | `req.Respond()` + headers |
| `utils/cookie.go` | ~10 строк в каждом сервисе где нужно |
| `utils/env.go` | `os.Getenv()` + `strconv` |
| `utils/hosts.go` | Перенесён в gateway |
| `internal/services/` | Перенесён в `demo/internal/` |
| `cmd/xauth/`, `cmd/xhttp/`, `cmd/xws/` | Перенесён в `demo/cmd/` |

---

## Конвенции для сервисов (вместо SDK — документация)

### Subject naming

Gateway маршрутизирует: `POST /v1/{service}/{method}` → NATS `api.v1.{service}.{method}`

`micro.AddEndpoint("login", ...)` автоматически подписывается на `api.v1.{service_name}.login` (если настроить subject prefix).

### Response protocol

Gateway читает из NATS response:
- Header `Status` → HTTP status code
- Header `Set-Cookie` → пробрасывается клиенту
- Body → HTTP response body
- Все остальные headers → пробрасываются клиенту

### Cookie parsing

```go
func getCookie(req micro.Request, name string) string {
    raw := req.Headers().Get("Cookie")
    header := http.Header{"Cookie": []string{raw}}
    r := http.Request{Header: header}
    c, err := r.Cookie(name)
    if err != nil {
        return ""
    }
    return c.Value
}
```

10 строк. Копируется куда нужно.

### Panic recovery

```go
func withRecover(handler micro.Handler) micro.Handler {
    return micro.HandlerFunc(func(req micro.Request) {
        defer func() {
            if r := recover(); r != nil {
                req.Error("500", "internal error", nil)
            }
        }()
        handler.Handle(req)
    })
}
```

---

## Порядок выполнения

### Фаза 1: перенос файлов (механический)

1. `mkdir -p asty/cmd asty/internal`
2. `mv internal/platform/asty/* asty/internal/`
3. `mv cmd/asty/main.go asty/cmd/main.go`
4. `mv utils/hosts.go asty/internal/features/gateway/hosts.go`
5. `mkdir -p demo/cmd demo/internal`
6. `mv cmd/xauth cmd/xhttp cmd/xws demo/cmd/`
7. `mv internal/services/* demo/internal/`
8. Починить import paths
9. `go build ./...`

### Фаза 2: переписать demo на nats.go/micro

1. Переписать demo/cmd/xauth/main.go
2. Адаптировать handlers: `*nats.Msg` → `micro.Request`
3. Аналогично xhttp, xws
4. `go build ./demo/...`

### Фаза 3: удаление

1. Удалить `internal/platform/nc/`, `internal/platform/logger/`, `internal/middleware/`, `utils/`
2. Удалить пустые `cmd/`, `internal/services/`
3. `go build ./...` && `go test ./...`

---

## Результат

```
/
├── asty/                    # Оркестратор
│   ├── cmd/main.go
│   └── internal/
├── demo/                    # Демо-сервисы (nats.go/micro напрямую)
│   ├── cmd/{xauth,xhttp,xws}/
│   └── internal/{xauth,xhttp,xws}/
├── go.mod
└── ...
```

Два мира. Оркестратор отдельно. Сервисы — обычные Go-программы с `nats.go/micro`, без платформенных зависимостей.
