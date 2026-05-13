# WS: отвязка велосипеда xws, topic-based hub в gateway

## Контекст

xws — отдельный процесс-посредник между gateway и браузером. Gateway
уже держит WS-соединение, уже подключён к NATS. xws добавляет лишний
hop, лишнюю точку отказа, собственный health check (`xws.ping`) и
ручной протокол без пользы: вся "бизнес-логика" — echo + inactivity
timeout (который gateway тоже уже имеет).

Решение: убрать xws, перенести WS-логику в gateway как topic-based hub.
Клиент подключается к `/v1/ws`, подписывается на NATS-топики через
JSON-протокол, gateway пушит входящие сообщения в WS напрямую.

## Протокол (клиент → gateway)

```jsonc
{"type":"subscribe",   "topic":"chat.room.42"}
{"type":"unsubscribe", "topic":"chat.room.42"}
{"type":"publish",     "topic":"chat.room.42", "data":"hello"}
{"type":"ping"}
```

## Протокол (gateway → клиент)

```jsonc
{"type":"message", "topic":"chat.room.42", "data":"hello"}
{"type":"subscribed", "topic":"chat.room.42"}
{"type":"unsubscribed", "topic":"chat.room.42"}
{"type":"pong"}
{"type":"error", "text":"topic not allowed"}
```

## Ограничения безопасности

- Whitelist разрешённых topic-префиксов (запрет `$SRV.*`, `asty.v1.*`, `_INBOX.*`)
- Лимит подписок на сессию (default: 16)
- Rate limit на publish per session
- Максимальный размер data (наследуем wsReadLimit = 64 KB)

---

## Задачи

### 1. Gateway: topic-based WS hub

- [ ] Новый файл `gateway/wshub.go` — обработка subscribe/unsubscribe/publish
- [ ] Изменить `routing.go`: маршрут `/v1/ws` → `gw.handleWSHub()` (без `{service}`)
- [ ] Реализовать per-session state: map[topic]*nats.Subscription
- [ ] Topic validation: whitelist префиксов, запрет системных subjects
- [ ] Лимит подписок на сессию
- [ ] Rate limit на publish per session (token bucket)
- [ ] Graceful unsubscribe all при закрытии сессии

### 2. Gateway: рефакторинг существующего WS-кода

- [ ] Удалить `wsConnectAck` (больше нет внешнего сервиса для ack)
- [ ] Удалить `wsSubscribeOut` (нет out-subject, подписки управляются hub'ом)
- [ ] Оставить: wsSession, ping/pong, inactivity timeout, wsConnGuard, graceful shutdown
- [ ] Убрать `wsReadLoop` publish в `{base}.in.{sid}` — заменить на dispatch в hub

### 3. Config

- [ ] Убрать `WSConnectTimeout` из `GatewayHTTPConfig` (нет connect-ack)
- [ ] Добавить `WSMaxSubscriptions int` в `GatewayRateLimitConfig`
- [ ] Добавить `WSPublishRate float64` + `WSPublishBurst int`
- [ ] Добавить `WSAllowedTopics []string` (префиксы)

### 4. Удаление xws

- [ ] Удалить `demo/internal/xws/` (manager.go, session.go, config.go)
- [ ] Удалить `demo/cmd/xws/main.go`
- [ ] Удалить `deployments/envs/dev/xws.asty`
- [ ] Удалить `deployments/infra/xws.asty`
- [ ] Убрать `xws` из `Makefile` (строка `go build -o bin/xws`)
- [ ] Убрать `xws` из `deployments/envs/dev/start.sh` (строки 162, 295)

### 5. Demo frontend (demo/web)

- [ ] Переписать `src/tabs/Ws.tsx` — новый протокол (subscribe/publish/rooms)
- [ ] Обновить `wsURL` в `api.ts`: `/v1/xws/ws` → `/v1/ws`
- [ ] UI: поле "topic/room", кнопки subscribe/unsubscribe, publish с текстом
- [ ] Демонстрация: два таба браузера в одной "комнате" обмениваются сообщениями

### 6. Документация и .asty

- [ ] Обновить CLAUDE.md: убрать упоминания xws как отдельного сервиса
- [ ] Обновить `dev-docs/architecture.md` если есть упоминания xws

### 7. Тесты

- [ ] Тест gateway WS hub: subscribe → publish → receive
- [ ] Тест: запрет подписки на системные топики
- [ ] Тест: лимит подписок на сессию
- [ ] Тест: inactivity timeout по-прежнему работает
- [ ] Тест: graceful shutdown закрывает все WS с CloseGoingAway

---

## Порядок выполнения

1. Config (3) — добавить поля
2. Gateway hub (1, 2) — основная работа
3. Удаление xws (4) — чистка
4. Demo frontend (5) — адаптация UI
5. Документация (6) — обновить ссылки
6. Тесты (7) — покрытие

## Что НЕ входит в scope

- Presence (кто онлайн в комнате) — отдельная задача
- JetStream history (replay пропущенных сообщений) — отдельная задача
- Auth per-topic (проверка прав на конкретный топик) — отдельная задача
