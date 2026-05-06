# SSE Architecture

## Принцип

Весь UI работает **только через SSE**. HTTP запросы — только для мутаций (drain, restart, stop, deploy). Никакого поллинга.

## Подписки

### Глобальная (всегда активна)

**Endpoint:** `GET /api/v1/stream`

| Event | Данные | Частота |
|-------|--------|---------|
| `status` | cluster leader, leader_ip, is_leader, nodes_total, nodes_healthy, services.loaded | 5 сек |
| `nodes` | полный список нод: id, ip, dc, status, cpu_total/available, mem_total/available, alloc counts | 5 сек |
| `services` | ServiceWithUsage: definition + current_copies, avg_cpu/memory, autoscaler состояние (min_copies, target_*, cooldown_*) | 5 сек |
| `drain_progress` | node_id, status, migrated, total_allocations | по событию |

Payload: ~200 KB / тик при 1000 нод. Для 3-5 клиентов — норма.

### Детальные (по запросу, пока открыта страница)

**Endpoint:** `GET /api/v1/stream/node/:id`

| Event | Данные |
|-------|--------|
| `allocations` | все аллокации на ноде (id, service_name, status, health, cpu_usage, mem_usage, restarts, started_at) |
| `metrics` | cpu[], memory[], rps[] time-series за 1h |

---

**Endpoint:** `GET /api/v1/stream/service/:name`

| Event | Данные |
|-------|--------|
| `detail` | service definition + allocations |
| `metrics` | cpu[], memory[], allocations_count[] time-series за 1h |

---

**Endpoint:** `GET /api/v1/stream/allocation/:id`

| Event | Данные |
|-------|--------|
| `detail` | allocation full object |
| `metrics` | cpu[], memory[] time-series за 1h |

---

**Endpoint:** `GET /api/v1/stream/metrics/cluster`

| Event | Данные |
|-------|--------|
| `metrics` | cluster cpu[], memory[], rps[] time-series за 1h |

### Поведение

- При подключении сервер **сразу** шлёт initial snapshot (все events одним пакетом)
- Далее обновления каждые 5 сек
- Каждые 30 сек шлётся keepalive comment (`: keepalive\n\n`) против idle-timeout прокси
- При disconnect клиент переподключается с exponential backoff (3s → 6s → ... → 60s, max 10 попыток)

## Серверная архитектура

Один **streamHub** в server: одна горутина раз в 5 сек собирает полный snapshot из KV (nodes + allocations + cluster status + per-service usage/autoscaler), кеширует и пушит всем подписчикам через каналы. Все SSE handlers подписываются на hub — никакого N×M доступа к KV. Drain events (`asty.v1.drain.progress`) тоже идут через hub (один общий NATS subscribe, fan-out по подписчикам).

## Архитектура фронтенда

```
App.tsx
  └─ initSSE() — глобальный EventSource('/api/v1/stream')
        ├─ status → store.clusterStatus
        ├─ nodes → store.nodes + store.nodeCache[id].node
        ├─ services → store.services
        └─ drain_progress → store.nodeCache[id].node.status

Страницы:
  cluster.tsx
    └─ подключает /api/v1/stream/metrics/cluster → локальный стейт (cpu, mem, rps)

  node-detail.tsx
    └─ подключает /api/v1/stream/node/:id → store.nodeCache[id].allocations + metrics

  service-overview.tsx
    └─ подключает /api/v1/stream/service/:name → store.serviceCache[name]

  service-detail.tsx
    └─ подключает /api/v1/stream/allocation/:id → store.allocationCache[id]
```

### Жизненный цикл страничных подписок

1. `useEffect(() => { ... }, [id])` — при маунте создаёт EventSource
2. Cleanup function — при unmount закрывает EventSource
3. При смене id (навигация между нодами) — закрывает старый, открывает новый

### Навигация без задержки

- Cluster page, таблица нод, список сервисов — данные из глобального SSE, всегда актуальны
- Node detail — node data из глобального SSE (уже в кеше), allocations/metrics из страничного SSE (~100ms на первый event)
- Возврат на предыдущую страницу — данные в zustand store, рисуется мгновенно + SSE обновит если что-то изменилось

## Масштаб

- 3-5 одновременных пользователей мониторинга
- Глобальный SSE: 5 коннектов × 200 KB / 5 сек = ~200 KB/сек с сервера
- Детальные SSE: максимум 5 дополнительных коннектов (по одному на пользователя, если все на разных страницах)
