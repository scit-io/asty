# Мониторинг

## Метрики (Prometheus)

Endpoint: `GET http://127.0.0.1:4646/metrics`

### Кластер
- `asty_nodes_total` — число нод
- `asty_nodes_healthy` — число здоровых нод

### Сервисы
- `asty_service_copies{service,node,datacenter}` — число копий
- `asty_service_cpu_percent{service,node}` — CPU per process
- `asty_service_memory_percent{service,node}` — Memory per process

### Autoscaler
- `asty_scaling_actions_total{service,action,reason}` — scaling decisions
- `asty_scaling_cooldown_active{service}` — cooldown статус

### Health
- `asty_health_check_duration_seconds{service,node}` — latency health check
- `asty_health_check_failures_total{service,node}` — число провалов

## Web UI

Встроенная админка на `127.0.0.1:4646` (SSH-tunnel).

### Дашборд
- Карта нод: IP, datacenter, CPU/Memory, статус
- Карта сервисов: копии, распределение по нодам

### Сервисы
- Каждая копия: нода, CPU/Memory, health, версия, uptime
- История scaling: когда, причина, нода
- Ручное управление: добавить/удалить копию

### Деплой
- Текущая версия, rolling update прогресс
- Canary: promote / revert / rollback

### Логи
- stdout/stderr каждой копии (streaming)
- Фильтрация по сервису, ноде

### Алерты
- Кластер на пределе ресурсов
- Нода недоступна
- Health check failed
- Autoscaler не может разместить копию

## Тестирование

### Unit
- scheduler_test.go: locality placement
- autoscaler_test.go: scaling decisions, cooldown, min enforcement
- deployer_test.go: rolling update, canary
- artifact_test.go: скачивание + checksum

### Integration (embedded NATS)
- 3 агента в одном процессе
- Симуляция нагрузки → locality placement
- Падение ноды → восстановление min=3
- Rolling update → zero downtime

### E2E
- Dev-окружение 3 ноды
- Полный цикл: деплой → нагрузка → autoscaling → scale down
