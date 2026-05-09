# xfrontend — Demo Frontend для Asty Gateway

React-приложение для тестирования всех паттернов взаимодействия с Gateway:
- **Auth** — JWT-авторизация через xauth (login, refresh, logout)
- **CRUD** — HTTP Request-Reply через xhttp (GET, POST, PUT, DELETE)
- **WebSocket** — bidirectional session через xws

## Быстрый старт

1. Убедитесь что Asty dev-окружение запущено:
   ```bash
   cd ../deployments/envs/dev
   ./start.sh 8  # 8 нод, gateway на dev-node-1 (127.0.0.1:80)
   ```

2. Установите зависимости:
   ```bash
   npm install
   ```

3. Запустите dev-сервер:
   ```bash
   npm run dev
   ```

4. Откройте http://localhost:3000

## Как это работает

- Vite dev-сервер проксирует `/v1/*` и `/health` на `http://127.0.0.1:80` (gateway dev-node-1)
- Фронт делает относительные запросы: `fetch('/v1/xauth/login')` → Vite → Gateway → NATS → xauth
- WebSocket: `new WebSocket('ws://localhost:3000/v1/xws/ws')` → Vite WS proxy → Gateway → xws
- HttpOnly cookies работают (same-origin через proxy)

## Конфигурация

По умолчанию всё настроено для Asty dev-окружения. Если нужен другой Gateway:

```bash
# Создайте .env
cp .env.example .env

# Раскомментируйте и измените URL
VITE_GATEWAY_URL=http://192.168.1.100
```

**Внимание:** прямое обращение к Gateway (без Vite proxy) требует CORS:
```bash
A_ALLOWED_HOSTS="localhost:3000" ./start.sh
```

## Build для продакшена

```bash
npm run build
```

Результат в `dist/` — статические файлы для любого веб-сервера (nginx, CDN).
В продакшене фронт обращается к Gateway напрямую по абсолютному URL из `VITE_GATEWAY_URL`.

## Структура

- `src/api.ts` — HTTP/WS клиент для Gateway
- `src/tabs/Auth.tsx` — демо JWT-авторизации
- `src/tabs/Crud.tsx` — демо REST API
- `src/tabs/Ws.tsx` — демо WebSocket
