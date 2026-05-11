# Анализ: Asty как фреймворк для сервисов

## Тезис

Сервис подключается к **asty**, а не к NATS. NATS — деталь реализации платформы, скрытая от разработчика. Единственная точка настройки — `.asty`-файл. Код сервиса — чистая бизнес-логика.

---

## Принцип

Разработчик не должен знать:
- Что под капотом NATS (а не HTTP, gRPC, или что-то ещё)
- Про subjects, queue groups, JetStream, KV buckets
- Про health probes, reconnect, topology

Разработчик знает только:
- Свои endpoints (имена методов)
- Свою бизнес-логику
- Формат request/response (JSON)
- Как работать с headers (Cookie, Authorization, etc.) — это его ответственность

---

## Как работает gateway (факт из кода)

Gateway — **полностью прозрачный прокси**. Не интерпретирует, не фильтрует, не принимает решений.

Routing автоматический: `POST /v1/{service}/{method...}` → NATS subject `api.v1.{service}.{method}`. Никакой регистрации, никаких routing table. Добавление нового сервиса или endpoint не требует изменений в gateway.

Все HTTP headers пробрасываются в NATS message как есть. Gateway добавляет только два платформенных заголовка:
- `X-Real-IP` — вычисленный реальный IP клиента (клиент не может подделать)
- `X-Request-Id` — сгенерированный ID для трейсинга

Авторизация, cookie, content-type, кастомные заголовки — всё доходит до сервиса. Сервис сам решает, что с ними делать.

---

## Текущий developer experience

Чтобы написать сервис сегодня, разработчик:

1. Создаёт `cmd/myservice/main.go` (~100 LOC boilerplate):
   - `logger.New("myservice")` — платформенная фабрика
   - `nc.NewClient(cfg.NATS, log)` — SDK с авто-KV, reconnect, health
   - `natsClient.RegisterHealth(...)` — HTTP-сервер для health probe
   - `middleware.Recover(log, handler)` — panic isolation
   - `middleware.RequireAuth(cfg, handler)` — JWT проверка
   - Signal handling + graceful drain

2. Создаёт `config.go` (~50 LOC):
   - `nc.DefaultConfig()` + ручное переопределение полей из env
   - KV bucket name, history — сервис знает про topology

3. Создаёт `handlers.go`:
   - `utils.Reply(log, msg, 200, data)` — протокольный ответ
   - `utils.GetCookie(msg, "access_token")` — парсинг cookie из NATS header
   - `utils.ReplyError(log, msg, 400, "bad request")` — ответ с ошибкой

4. Создаёт `.asty` файл для деплоя

**Проблемы:**
- 4 импорта платформенных пакетов (`nc`, `logger`, `middleware`, `utils`)
- Сервис знает про cluster topology (replicas, placement)
- Сервис поднимает HTTP-сервер только для health check
- 100 строк boilerplate одинаковы между xauth/xhttp/xws
- Конфигурация дублируется: env vars в `.asty` И в `config.go`

---

## Целевая модель: asty framework

### Вариант A: Framework library (Go)

Сервис импортирует один пакет — `asty`. Это runtime фреймворка, не SDK.

```go
package main

import "github.com/upway/asty"

func main() {
    app := asty.New()

    app.Handle("create", handleCreate)
    app.Handle("list", handleList)

    app.Run()
}

func handleCreate(ctx *asty.Context) {
    var req CreateRequest
    if err := ctx.Bind(&req); err != nil {
        ctx.Error(400, "invalid request")
        return
    }

    result := doSomething(req)
    ctx.JSON(201, result)
}

func handleList(ctx *asty.Context) {
    items := fetchItems(ctx.KV("cache"))
    ctx.JSON(200, items)
}
```

**Что делает `asty.New()` под капотом:**
1. Читает конфигурацию из env (agent пробрасывает всё нужное)
2. Подключается к транспорту (сейчас NATS, завтра может быть что-то другое)
3. Регистрирует health probe автоматически
4. Настраивает panic recovery на каждый handler
5. Подписывается на endpoints (маппинг: endpoint name → transport subject)
6. Обрабатывает graceful shutdown по SIGTERM

**Что такое `asty.Context`:**

```go
type Context struct {
    Auth    *AuthInfo           // nil если сервис не парсил — это его дело
    Headers map[string]string   // все headers от клиента (прозрачно через gateway)
}

type KVStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
}
```

Сервис работает с абстракциями. `KVStore` сегодня — NATS JetStream KV. Завтра может быть Redis. Сервису всё равно.

### Headers и авторизация — ответственность сервиса

Gateway передаёт все HTTP headers прозрачно. Сервис сам:
- Парсит Cookie, если ему нужны куки
- Читает Authorization, если использует bearer tokens
- Проверяет JWT, если у него auth endpoints
- Выставляет Set-Cookie в ответе, если нужно

```go
func handleProtected(ctx *asty.Context) {
    token := ctx.Cookie("access_token")
    if token == "" {
        ctx.Error(401, "unauthorized")
        return
    }

    claims, err := verifyJWT(token, secret)
    if err != nil {
        ctx.Error(401, "invalid token")
        return
    }

    // бизнес-логика с claims.Subject
    ctx.JSON(200, result)
}
```

Платформа не лезет в авторизацию. Это бизнес-решение сервиса.

### Вариант B: Sidecar / HTTP-прокси (любой язык)

Для не-Go сервисов — agent выступает локальным прокси:

```
HTTP Client → Gateway → [NATS] → Agent (sidecar) → HTTP localhost → Service
```

Сервис — обычный HTTP-сервер на любом языке:

```python
from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route("/create", methods=["POST"])
def create():
    token = request.cookies.get("access_token")
    # сервис сам проверяет auth
    result = do_something(request.json)
    return jsonify(result), 200

app.run(port=int(os.environ["ASTY_PORT"]))
```

В `.asty`:
```yaml
runtime: http
port: auto
```

---

## Полный пример: реальный сервис

### `myservice.asty`

```yaml
name: myservice
type: service

artifact:
  url: https://artifacts.example.com/myservice_${ARCH}.tar.gz
  checksum: sha256:${CHECKSUM}

command: ./myservice
user: asty
kill_timeout: 30s

endpoints:
  - name: create
  - name: get
  - name: update
  - name: delete
  - name: list

kv:
  - name: cache
    history: 1
    ttl: 30m

env:
  DATABASE_URL: "${DATABASE_URL}"
  AUTH_SECRET: "${AUTH_SECRET}"

resources:
  cpu: 200
  memory: 128

restart:
  attempts: 5
  delay: 10s

update:
  max_parallel: 1
  min_healthy_time: 10s
  healthy_deadline: 3m
```

### `main.go`

```go
package main

import (
    "context"
    "database/sql"

    "github.com/upway/asty"
    _ "github.com/lib/pq"
)

func main() {
    app := asty.New()

    db, _ := sql.Open("postgres", app.Env("DATABASE_URL"))
    defer db.Close()

    h := &Handlers{db: db, secret: []byte(app.Env("AUTH_SECRET"))}

    app.Handle("create", h.Create)
    app.Handle("get", h.Get)
    app.Handle("update", h.Update)
    app.Handle("delete", h.Delete)
    app.Handle("list", h.List)

    app.Run()
}

type Handlers struct {
    db     *sql.DB
    secret []byte
}

func (h *Handlers) Create(ctx *asty.Context) {
    // Auth — ответственность сервиса
    claims, err := h.requireAuth(ctx)
    if err != nil {
        return
    }

    var req struct {
        Title string `json:"title"`
        Body  string `json:"body"`
    }
    if err := ctx.Bind(&req); err != nil {
        ctx.Error(400, "invalid json")
        return
    }

    var id int
    err = h.db.QueryRowContext(context.Background(),
        "INSERT INTO items (title, body, author) VALUES ($1, $2, $3) RETURNING id",
        req.Title, req.Body, claims.Subject,
    ).Scan(&id)
    if err != nil {
        ctx.Error(500, "database error")
        return
    }

    ctx.KV("cache").Delete(context.Background(), "list")
    ctx.JSON(201, map[string]int{"id": id})
}

func (h *Handlers) List(ctx *asty.Context) {
    if cached, err := ctx.KV("cache").Get(context.Background(), "list"); err == nil {
        ctx.Raw(200, cached)
        return
    }

    rows, _ := h.db.QueryContext(context.Background(), "SELECT id, title FROM items LIMIT 100")
    defer rows.Close()

    var items []Item
    for rows.Next() {
        var item Item
        rows.Scan(&item.ID, &item.Title)
        items = append(items, item)
    }

    data := ctx.Marshal(items)
    ctx.KV("cache").Put(context.Background(), "list", data)
    ctx.JSON(200, items)
}

// requireAuth — бизнес-логика сервиса, не платформы
func (h *Handlers) requireAuth(ctx *asty.Context) (*Claims, error) {
    token := ctx.Cookie("access_token")
    if token == "" {
        ctx.Error(401, "unauthorized")
        return nil, errUnauthorized
    }
    claims, err := verifyJWT(token, h.secret)
    if err != nil {
        ctx.Error(401, "invalid token")
        return nil, err
    }
    return claims, nil
}
```

**Ноль знаний о NATS. Auth — решение сервиса. `.asty` — единственная конфигурация.**

---

## Что делает runtime `asty.New()` + `app.Run()`

| Шаг | Действие | Скрыто от разработчика |
|-----|----------|----------------------|
| 1 | Читает `ASTY_*` env vars (agent пробрасывает) | Адрес транспорта, credentials |
| 2 | Подключается к транспорту | Весь transport layer |
| 3 | Подписывается на probe subject | Health автоматически |
| 4 | Для каждого `app.Handle(name, fn)` — подписка на computed subject | Subject naming |
| 5 | Оборачивает каждый handler в recover | Panic isolation |
| 6 | Подключается к KV buckets из env | Bucket names, replicas |
| 7 | Блокирует на SIGTERM | Graceful shutdown |
| 8 | При shutdown: drain, wait in-flight, exit | Lifecycle |

### Env vars, которые agent пробрасывает (все `ASTY_` prefixed):

| Var | Назначение |
|-----|-----------|
| `ASTY_TRANSPORT_URL` | Адрес транспорта |
| `ASTY_TRANSPORT_USER` | Credentials |
| `ASTY_TRANSPORT_PASS` | Credentials |
| `ASTY_SERVICE_NAME` | Имя сервиса из .asty |
| `ASTY_PROBE_SUBJECT` | Subject для health probe |
| `ASTY_KV_CACHE` | Имя KV bucket (по одному на каждый из `kv:`) |
| `ASTY_LOG_LEVEL` | Уровень логирования |

Разработчик не читает их руками. Runtime читает при `asty.New()`.

---

## API поверхность `asty` runtime package

```go
package asty

// App
type App struct{}

func New() *App
func (a *App) Handle(name string, fn Handler)
func (a *App) Run()
func (a *App) Env(key string) string

// Handler
type Handler func(ctx *Context)

// Context
type Context struct{}

func (c *Context) Bind(v any) error
func (c *Context) JSON(status int, v any)
func (c *Context) Raw(status int, data []byte)
func (c *Context) Error(status int, msg string)
func (c *Context) KV(name string) KVStore
func (c *Context) Log() Logger
func (c *Context) Body() []byte
func (c *Context) Header(key string) string
func (c *Context) Cookie(name string) string
func (c *Context) SetCookie(name, value string, opts ...CookieOption)
func (c *Context) Marshal(v any) []byte

// KVStore
type KVStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Keys(ctx context.Context) ([]string, error)
}

// Logger
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

~200 LOC реализации. Thin wrapper над транспортом, но сервис этого не видит.

---

## Что скрыто за абстракцией

| Скрытое | Почему не нужно сервису |
|---------|----------------------|
| NATS connection | Managed платформой |
| Subject naming convention | `api.v1.{service}.{endpoint}` — внутреннее |
| Queue groups | Всегда = service name |
| Health probe | Платформенная забота |
| Reconnect | Agent перезапустит при необходимости |
| KV bucket creation | Server создаёт при деплое |
| Panic recovery | Runtime гарантирует |
| Graceful shutdown | Стандартный lifecycle |

**Escape hatch** для advanced use cases:
```go
func (a *App) Transport() *nats.Conn
```

Conscious opt-in в implementation detail, не норма.

---

## KV lifecycle: server ownership

### Текущая проблема

Сервис сам создаёт bucket при старте через SDK. Знает про replicas, placement, topology. 100 строк кода.

### Целевой flow

1. Deploy: server читает `.asty` → видит `kv: [{name: cache, history: 1, ttl: 30m}]`
2. Server вызывает `EnsureBucket` с авто-replicas (topology — его знание)
3. Agent пробрасывает env: `ASTY_KV_CACHE=myservice_cache`
4. Runtime подключается к bucket автоматически
5. Сервис: `ctx.KV("cache").Get(...)` — одна строка

---

## Health: от HTTP к transport probe

### Текущая схема

Сервис поднимает HTTP-сервер только для health check. 40 строк boilerplate.

### Целевая схема

Agent шлёт request через transport dispatcher сервиса. Runtime отвечает автоматически. Сервис не пишет ни строки.

Ловит deadlock так же: если dispatcher завис, probe не отвечает → unhealthy.

---

## .asty файл — единственная точка настройки

```yaml
name: myservice
type: service                    # service (autoscaled) | system (1 per node)

artifact:
  url: https://.../${ARCH}.tar.gz
  checksum: sha256:${CHECKSUM}

command: ./myservice
user: asty
kill_timeout: 30s

# API endpoints — определяют подписки runtime
endpoints:
  - name: create
  - name: get
  - name: update
  - name: delete
  - name: list
  - name: admin.users            # dot → вложенный путь

# State (KV buckets) — server создаёт при деплое
kv:
  - name: cache
    history: 1
    ttl: 30m
  - name: sessions
    history: 1

# Бизнес-конфигурация. ASTY_* добавляются agent'ом автоматически.
env:
  DATABASE_URL: "${DATABASE_URL}"
  AUTH_SECRET: "${AUTH_SECRET}"
  REDIS_URL: "${REDIS_URL}"

# Runtime mode
runtime: asty-go                 # asty-go | http (sidecar)

resources:
  cpu: 200
  memory: 128

health:
  interval: 10s
  timeout: 1s

restart:
  attempts: 5
  delay: 10s

update:
  max_parallel: 1
  min_healthy_time: 10s
  healthy_deadline: 3m

logs:
  max_files: 5
  max_file_size: 10
```

---

## Порядок реализации

### Фаза 1: `asty` Go package (1-2 недели)

Создать `pkg/asty/` (~200 LOC):
- `app.go`: New, Handle, Run, Env
- `context.go`: Context, Bind, JSON, Error, Raw, Cookie, SetCookie, Header, KV, Log
- `kv.go`: KVStore implementation over JetStream (hidden)
- `transport.go`: connect + subscribe + probe (internal, не exported)

### Фаза 2: .asty format extensions (1 неделя)

- Секция `endpoints:` → ServiceDefinition parser
- Секция `kv:` → ServiceDefinition parser
- Поле `runtime:` → определяет probe тип
- Server создаёт KV buckets при деплое
- Agent пробрасывает `ASTY_*` env

### Фаза 3: Миграция x-сервисов (1 неделя)

- Переписать xauth/xhttp/xws на `asty` framework
- Удалить `internal/platform/nc`, `internal/platform/logger`, `internal/middleware`, `utils/`
- Каждый сервис: ~40-60 LOC main.go + handlers

### Фаза 4: HTTP sidecar mode (опционально, 1 неделя)

- Agent при `runtime: http` проксирует transport↔HTTP
- Polyglot сервисы
- Пробрасывает `ASTY_PORT`, все headers

---

## Сравнение

| Метрика | Сейчас | После |
|---------|--------|-------|
| Платформенные импорты | 4 пакета (nc, logger, middleware, utils) | 1 (`asty`) |
| Boilerplate в main.go | ~100 LOC | ~20 LOC |
| Конфигурация | .asty + config.go + env | Только .asty |
| Знание о transport | Полное (NATS subjects, headers, queue groups) | Ноль |
| Знание о topology | Да (replicas, placement) | Нет |
| HTTP-сервер для health | Да | Нет (автоматически) |
| Auth | Middleware в каждом handler | Сервис решает сам, чисто |
| Gateway changes при новом сервисе | Нет (уже динамический) | Нет |
| Время до первого handler | ~2 часа | ~10 минут |

---

## Итог

**Gateway** — прозрачный прокси. Все headers, все cookies, все данные доходят до сервиса. Routing автоматический.

**Auth** — ответственность сервиса. Платформа не лезет в бизнес-решения.

**Asty runtime** — thin framework, скрывающий transport. Сервис пишет handlers и `.asty` файл.

**Контракт:**
1. `.asty` файл — декларация всего (endpoints, KV, resources, env)
2. `app.Handle(name, fn)` — реализация
3. `app.Run()` — запуск

Три строки инфраструктурного кода. Остальное — продукт.
