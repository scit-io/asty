# Правила работы с кодом Asty

Этот документ — собрание соглашений по написанию кода в пакете Asty. Они сформировались в ходе рефакторинга Phase 6 (см. `dev-docs/refactoring-audit.md`). Если ты только начинаешь, прочти его целиком — после этого код Asty будет читаться как книжка с понятными именами и комментариями «зачем».

> **Где искать пакет.** Папка называется `asty/` и лежит где-то в дереве (паттерн `**/asty/`). Стабильно только имя — родительский путь может поменяться при следующих рефакторингах. Не зашивай его в скриптах и документации, ищи папку, в которой есть `core/`, `features/`, `server/`, `agent/`.

---

## 1. Размер файлов

**Правило**: один файл — максимум 200 строк. Цель — 150–180.

**Единственное исключение**: `features/clustering/controller/workqueue.go` (~214 строк). Это самодостаточная структура данных (очередь задач), её бессмысленно резать. Любой другой файл больше 200 строк надо делить.

**Зачем**: когда файл больше 200 строк, его уже невозможно держать в голове целиком. Особенно тяжело тому, кто только пришёл в проект.

### Как делить большие файлы

Есть два способа:

**А. Делить внутри той же папки** (рекомендуется по умолчанию). У всех новых файлов остаётся та же декларация `package X`. Импорты у тех, кто этим пакетом пользуется, не меняются. Пример: `features/draining/` содержит `manager.go`, `run.go`, `system.go`, `migrate.go`, `wait.go` — всё это один пакет `draining`.

**Б. Выносить в новую подпапку (sub-package)** — только когда выделенный кусок представляет собой *самостоятельную подфичу* со своим внешним API. Пример: `features/scheduling/proximity/` — матрица задержек между датацентрами имеет свою собственную «жизнь» и используется планировщиком как чёрный ящик.

В исходном плане рефакторинга были sub-package для всех больших файлов; на практике оказалось, что in-folder splits дают тот же эффект для читаемости при меньшем количестве правок в коде. Так что следующим разработчикам — склоняйся к разделению *внутри той же папки*.

### Один файл — одна идея

Имя файла должно описывать, что в нём происходит:
- `wait.go` — кто-то чего-то ждёт.
- `tracker.go` — за чем-то следим.
- `canary.go` — деплоим канарейку.
- `rolling.go` — катим обновление волнами.

Если новый человек ищет «где код, который ждёт хелсчеки» — он должен по имени файла угадать.

### `doc.go` в подпакете

В каждой подпапке-пакете — `doc.go` с 5–10 строк:

```go
// Package proximity manages the inter-datacenter latency matrix.
// Operators define DC pairs and millisecond latencies via the
// A_DC_LATENCY environment variable; the scheduler uses the matrix
// to place replacements on the geographically nearest healthy node.
package proximity
```

### `tunables.go`

Если в пакете много именованных констант времени/таймаутов — выноси их в отдельный файл `tunables.go` (пример: `server/tunables.go`). Так основные файлы не захламляются.

---

## 2. Идиомы Go

### Используй stdlib, а не самописное

**Плохо**:

```go
// Самописный split, проходящий по символам.
func splitLines(data string, lastN int) []string {
    var lines []string
    current := ""
    for _, ch := range data {
        if ch == '\n' { … }
    }
}
```

**Хорошо**:

```go
parts := strings.Split(data, "\n")
```

Список «использовать stdlib, а не самописное»:

| Что нужно | Что использовать |
|---|---|
| Разделить строку | `strings.Split`, `strings.Cut` |
| Проверить префикс | `strings.HasPrefix` |
| Сортировка | `sort.Slice` (никогда не пузырьковая) |
| Удалить из слайса | `slices.Delete`, `slices.DeleteFunc` |
| Чтение по строкам | `bufio.Scanner` |

В Phase 6.1 было удалено пять самописных утилит — все заменились однострочниками из stdlib.

### Именованные константы вместо «магических чисел»

**Плохо**:

```go
ticker := time.NewTicker(5 * time.Second)
```

Что такое «5 секунд»? Почему именно 5? Кто читает код впервые, не сможет ответить.

**Хорошо**:

```go
// streamHubInterval — резервный таймер на случай, если KV.Watch
// пропустил событие. Нормальные обновления приходят сразу через
// debouncer внутри streamHub.Run; этот таймер срабатывает, только
// если за минуту вообще ничего не пришло. 60s — «если за минуту
// ничего не случилось, скорее всего что-то сломалось».
const streamHubInterval = 60 * time.Second

ticker := time.NewTicker(streamHubInterval)
```

Каждая константа с таймаутом, интервалом или порогом получает имя **и комментарий «зачем именно столько»**.

### Типизированные перечисления (enum), а не голые строки

**Плохо**:

```go
if alloc.Status == "running" { … }
alloc.Status = "pending"
```

Опечатка `"runninng"` пройдёт сборку — багу поймаем только в проде.

**Хорошо**:

```go
// В core/types/allocation.go:
type AllocationStatus string
const (
    AllocPending  AllocationStatus = "pending"
    AllocRunning  AllocationStatus = "running"
    AllocStopped  AllocationStatus = "stopped"
    AllocFailed   AllocationStatus = "failed"
    // ...
)

// В вызывающем коде:
if alloc.Status == types.AllocRunning { … }
alloc.Status = types.AllocPending
```

Компилятор поймает любую опечатку. JSON-формат остаётся тем же (под капотом всё ещё `string`).

У типов статусов есть удобные методы: `AllocationStatus.IsLive()`, `AllocationStatus.Occupies()`, `NodeInfo.IsHealthy(now)`.

### Хелперы, когда одна форма повторяется ≥3 раз

Если ты копипастишь один и тот же блок в третий раз — пора вынести в функцию. Примеры из рефакторинга:

- HTTP method guard. Раньше в 27 местах:
  ```go
  if r.Method != http.MethodGet {
      http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
      return
  }
  ```
  Стало одной строкой:
  ```go
  if !methodGuard(w, r, http.MethodGet) { return }
  ```

- Парсинг URL `:id/action`. Раньше:
  ```go
  for i, ch := range path {
      if ch == '/' {
          nodeID = path[:i]
          action = path[i+1:]
          break
      }
  }
  if nodeID == "" { nodeID = path }
  ```
  Стало:
  ```go
  nodeID, action, _ := strings.Cut(path, "/")
  ```

- Generic `subscribers[T]` (через дженерики Go) — заменил три почти одинаковых блока mutex+map+nextID в `streamHub`.

### Функциональный тип вместо интерфейса с одним методом

Если интерфейс — это `interface { OneMethod(args) error }`, лучше сделать его типом-функцией:

```go
// Не так:
type CommandDispatcher interface {
    SendStartCommand(nodeID string, svc *types.ServiceDefinition) error
}

// А так:
type SendStartCommand func(nodeID string, svc *types.ServiceDefinition) error
```

Тогда вызывающему коду не нужно писать struct-обёртку — он просто передаёт метод-значение:

```go
ctrl := controller.NewServiceController(..., s.sendStartCommand, ...)
```

### Парсить строки при загрузке, а не в горячем пути

В `.asty` файле есть поля типа `kill_timeout: "30s"`. Раньше каждый вызов `svc.GetKillTimeout()` парсил эту строку заново. Это лишняя работа на каждом тике.

Решение: добавить метод `Resolve()`, который вызывает загрузчик после `yaml.Unmarshal` — он один раз парсит все строки в `time.Duration` и кэширует в скрытых полях. Геттеры читают кэш.

### Защитные копии для shared state

Если метод возвращает что-то из карты под мьютексом — возвращай **копию**, чтобы вызывающий не сломал внутреннее состояние:

```go
func (c *Checker) GetStatus(name string) (*Check, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    check, ok := c.checks[name]
    if !ok { return nil, false }
    return cloneCheck(check), true   // ← копия!
}
```

### Оборачивай ошибки

```go
// Плохо:
return fmt.Errorf("failed: %v", err)

// Хорошо:
return fmt.Errorf("read allocation %s: %w", key, err)
```

Через `%w` сохраняется цепочка ошибок — потом её можно проверить через `errors.Is/As`.

### Заглушки возвращают 501, а не врущий 200

Если endpoint ещё не реализован — `http.StatusNotImplemented` (501), а не «всё ок, action initiated (not yet fully implemented)». Заглушки, которые врут оператору, страшнее, чем честное «ещё не сделано».

### Нет префикса `Get` у простых аксессоров

Стиль Go: `node.Datacenter`, а не `node.GetDatacenter()`. Если метод делает работу (I/O, вычисления) — называй по делу: `ListAllocations`, не `GetAllocations`.

### `MustJSON` для «никогда не должно упасть»

Внутри типов есть `types.MustJSON(v)` — она маршалит и возвращает `{}` в случае ошибки. Используется в SSE/NATS-пайплайнах, где маршалинг физически не может упасть, но Go-сигнатура требует ошибку.

---

## 3. Реактивность вместо опроса

Раньше во многих местах был цикл: «каждые 5 секунд проверь, что там в БД». В Phase 6.3 это заменили на подписки и колбэки.

### Дефолт — реактивно

Когда тебе надо «дождаться, что какое-то поле в KV станет таким-то значением»:

- **Не пиши**: тикер каждые 200 мс + `ListAllocations(...)`.
- **Пиши**: подписку через `state.WatchAllocations`, `state.WatchNodes`, `state.WatchAllocation`.

В `state/watch.go` уже есть generic `watchKV` driver — все Watch-методы построены на нём.

Когда тебе надо «дождаться, что процесс завершился»:

- **Не пиши**: тикер, который каждые 5 с смотрит `proc.Status() == StatusFailed`.
- **Пиши**: `proc.OnExit(func(err) { ... })` или `<-proc.Done()`. Эти каналы/колбэки срабатывают мгновенно из горутины-монитора.

### Допустимые исключения (с обоснованием)

Не всё можно сделать реактивно. Вот список оставленных таймеров и почему:

| Где | Период | Почему именно опрос |
|---|---|---|
| `leader.CampaignForLeader` refresh | 5 с | Физика TTL: запись должна обновляться до истечения |
| `controller.periodicResync` | 60 с | Safety net на случай пропущенных событий |
| `agent.publishHeartbeat` | 5 с | Доказательство «нода жива» — должно идти регулярно |
| `agent.publishProcessMetrics` | 10 с | Семплирование CPU/Memory |
| `MetricsCollector.Start` | EvalInterval | То же — sampling |
| `HealthChecker.Start` | 1 с | HTTP-пробы должны инициироваться периодически |
| `Process.TailLogs` | 100 мс | Чтение файла; fsnotify усложняет ротацию логов |
| `proximity.RunValidation` | 1 час | Тяжёлая работа, медленный дрифт |

В каждом таком месте рядом с `time.NewTicker` стоит комментарий «почему здесь нужен опрос».

### Backoff вместо линейного sleep

В `core/netutil.EnsureBucket` — `100ms → 3s → ... → 30s budget`. Раньше было `30 × 1s` — это значило, что даже в нормальном случае boot тратил пару секунд впустую. Теперь типичный случай — 100мс.

### Универсальный fan-out — `subscribers[T]`

Если рассылаешь события нескольким подписчикам — используй generic-хелпер `subscribers[T]` (см. `server/streamhub_subs.go`):

```go
type subscribers[T any] struct { ... }
func (s *subscribers[T]) add(buffer int) (chan T, func())
func (s *subscribers[T]) fanout(v T)
```

Медленные подписчики получают drop по полному буферу — никогда не блокируем publisher.

### Debounce при бурстах

Когда watch выдаёт пачку обновлений за миллисекунду (флипы статусов), не нужно перестраивать снапшот на каждое — ждём 500 мс после последнего и перестраиваем один раз. См. `streamHub.driveLoop`.

### Process-колбэки не должны блокировать

`Process.OnExit(fn)` запускается на горутине-мониторе. `fn` обязан быть не-блокирующим:

```go
proc.OnExit(func(err error) {
    select {
    case a.failed <- name:    // non-blocking send
    default:                   // буфер полный — дропаем
    }
})
```

Drop-on-full всегда лучше, чем «заблокировать монитор».

### Сначала проверь текущее состояние, потом подписывайся

Когда watch используется как «дождись, пока случится X» — сначала проверь, не случилось ли X уже сейчас. Если да — не надо поднимать watcher. Канонический пример: `DrainManager.healthyReplacementExists`.

---

## 4. Архитектура — где что лежит

### Верхний уровень пакета Asty

- `core/` — примитивы без знания о конкретных фичах. Сейчас: `config`, `types`, `errors`, `netutil`. Новый код сюда **только** если он используется ≥3 фичами и не ссылается на типы конкретной фичи.
- `features/` — вертикальные слайсы по фичам: `clustering`, `scheduling`, `autoscaling`, `deployment`, `draining`, `execution`, `observability`, `api`. Каждая фича владеет своими типами, логикой и подпапками.
- `server/`, `agent/` — тонкие оркестраторы, склеивающие фичи. Бизнес-логике здесь делать нечего — она в `features/`.

### Интерфейсы — только на границах пакетов

```go
// features/api/context.go — интерфейс ОПРЕДЕЛЁН в api
type ServerContext interface { … }

// server/context.go — Server РЕАЛИЗУЕТ его
var _ apiPkg.ServerContext = (*Server)(nil)
```

Зачем: api-handlers могут читать данные сервера, но при этом не импортируют пакет `server` (это бы дало цикл).

Не выдумывай интерфейсы заранее. Добавляй только если:

1. Иначе будет циклическая зависимость между пакетами.
2. Тесту нужна замена реализации (fake).

### Sub-package — это подфича, а не «большой файл»

- `clustering/state/` — это подпакет, потому что cluster-state — самостоятельная подсистема со своими KV-конвенциями, watch-хелперами, CAS-логикой.
- `scheduling/proximity/` — подпакет, потому что матрица задержек имеет свою «жизнь» (load → validate → query).
- `autoscaling/metrics/` — подпакет, потому что RPS-time-series + scaling-events — самодостаточный хранилище данных.

Просто потому что файл стал большим — **не подпакет**, а split внутри той же папки.

### Server — мешок зависимостей

`Server struct` — это просто поля-указатели на реализации фич. Геттеры, удовлетворяющие `api.ServerContext`, — однострочники в `server/context.go`. Реальный lifecycle — в `server/boot.go` (`Start()`) и по-фичевые файлы (`commands.go`, `deployment.go`, `leadership.go`, `logbuffer.go`, `metrics.go`, `nats.go`).

### Agent зеркалит Server

Тонкий `agent/agent.go` — struct + `Start`. Плюс по-фичевые файлы: `services.go` (StartService/StopService), `heartbeat.go`, `restart.go`, `logstream.go`, `nodeinfo.go`, `commands.go`.

### Циклы зависимостей

Уже встречались два: `server` ↔ `draining` решили через `draining.DrainDeps`, `server` ↔ `api` — через `api.ServerContext`. Если ты ловишь импорт обратно в `server` — добавь интерфейс в потребляющий пакет.

### Path independence

Код внутри пакета Asty должен импортировать только сам себя. Стабильно только имя папки — `asty/`; её родительский путь *может поменяться* в будущих рефакторах. Когда в комментариях и документации ссылаешься на файлы — используй относительный путь от корня пакета (например, `features/draining/manager.go`), а не абсолютный.

---

## 5. Тестирование

### Перед коммитом — обязательно

```bash
go build ./...                  # SUCCESS
go test ./...                   # ALL PASS
go test -race -count=1 ./...    # PASS
go vet ./...                    # ничего не печатается
```

Все четыре. В Phase 6 я коммитил только после того, как все четыре зелёные.

### Фикстуры — в `testutil/`

Общие фикстуры (`NewTestConfig`, `NewTestNode`, `NewTestService`, `NewTestAllocation`) живут в подпакете `testutil/`. Тесты пользуются ими как готовыми «болванками».

Когда ввели типизированные перечисления, фикстуры тоже переехали на `types.AllocRunning` вместо `"running"` — это часть **публичной поверхности тестов**, и она тоже типизирована.

### Concurrent-код требует race-тестов

streamHub, controller workqueue, agent restart-channel, drain manager, process monitor — у всех есть race-тесты. Новый код с горутинами и shared state **обязательно** прогоняется под `-race`.

### Тесты — рядом с кодом

`scheduling/scheduler.go` и `scheduling/scheduler_test.go` — оба `package scheduling`. Это позволяет тестам использовать неэкспортированные хелперы напрямую.

### Без сетевых зависимостей

Тесты, которым нужен NATS — используют embedded test-сервер. Тесты, которым нужен файл — `t.TempDir()`. Никаких реальных кластеров.

### Имена тестов

`Test<Тип><Поведение>`: `TestSchedulerFilterHealthyNodes`, `TestMatrixSortByProximity`. Подтесты через `t.Run("descriptive name", ...)` для table-driven кейсов.

---

## 6. Чистота для не-бэкендера

Этот пункт самый важный. Asty читают люди без глубокого бэкенд-опыта. Пиши так, чтобы они не утонули.

### Комментарии — ЗАЧЕМ, а не ЧТО

Имя переменной говорит «что». Комментарий должен говорить «зачем».

Плохо:

```go
// Increment migrated counter
op.status.Migrated++
```

Хорошо — либо имя достаточно говорящее, либо комментарий объясняет *причину*:

```go
// bumpMigrated increments the counter and publishes a progress event.
// Used by both the system and regular paths.
func (dm *DrainManager) bumpMigrated(op *drainOp, total int) { ... }
```

### Один файл — одна идея

`wait.go` — ждём. `tracker.go` — следим. `canary.go` — канарейка. Новый читатель угадывает имя файла по описанию задачи.

### Нет заглушек

Любой endpoint, который возвращает `"not yet implemented"` с HTTP 200 — **запрещён**. Либо реализуй, либо отдай 501. Полу-правда вреднее, чем честное «не работает».

### Нет недоделанных функций

Если метод не используется или возвращает плейсхолдер — **удаляй**. В Phase 6.2.10 был удалён мёртвый `Process.Context()`, который возвращал не-отменяемый `context.Background()` — он просто лежал и врал.

### Magic numbers → именованные константы

Новичок не отличит `5 * time.Second` от `5 * time.Minute` беглым взглядом. Каждая «магическая» цифра — это **именованная константа** + комментарий с обоснованием.

### Хелперы с понятными именами

Inline-цикл из трёх строк — это не короче, чем `collectFailed()`. Зато читателю надо парсить три строки кода вместо «о, это собирает фейлы». Извлекай хелперы: `dispatchOne`, `markCurrent`, `recordError`, `completeNodeDrain` и так далее.

### Без «умного» кода

Никаких bit-twiddling, нестандартной рекурсии, длинных цепочек тернарных операторов. Если читателю надо разворачивать рекурсию у себя в голове — переписывай линейно. Канонический пример — старая версия `SortDatacentersByProximity`, заменённая на `sort.Slice`.

### Имена функций

Глаголы для действий (`Reconcile`, `Send`, `Drain`). Существительные для данных. Без префикса `Get` у простых аксессоров — `node.Datacenter`, а не `node.GetDatacenter()`. Если метод делает работу — называй по делу: `ListAllocations`.

---

## Что читать дальше

- `dev-docs/refactoring-audit.md` — полный план рефакторинга Phase 6 с метриками «было/стало».
- `dev-docs/asty-refactoring.md` — план рефакторинга Phase 1–5 (Feature-Based архитектура).
- `CLAUDE.md` — общее описание проекта и его структуры.
- `.claude/coding-rules/` — те же правила, но в кратком формате для AI-ассистента (зависит от инструмента).
