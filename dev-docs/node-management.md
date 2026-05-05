# Node Management Operations

Документация по операции Drain для управления нодами.

## Drain (плавный вывод ноды)

### Описание
Плавный вывод ноды из активного использования. Нода остаётся в кластере, agent продолжает работать, но scheduler перестаёт размещать новые аллокации, а существующие постепенно переезжают на другие ноды.

### Процесс

1. **Server**: ставит `status: "draining"` в KV (`nodes/{node_id}`)
2. **Scheduler**: перестаёт размещать новые аллокации на эту ноду (проверяет `node.Status == "ready"`)
3. **Server**: запускает фоновую задачу переразмещения:
   - Берёт список аллокаций на draining-ноде
   - Для каждой аллокации:
     - Запускает новую копию на другой ноде
     - Ждёт пока новая аллокация станет `status: "running"` и `health_status: "healthy"`
     - Отправляет команду agent'у остановить старую аллокацию
     - Ждёт подтверждения остановки
     - Пауза 5-10 секунд перед следующей
4. **Когда все аллокации переехали**: `status: "drained"`
5. **Нода остаётся**: в списке нод, agent работает, heartbeat продолжается
6. **Resume**: переключение switch обратно → `status: "ready"`

### Statuses

- `ready` → `draining` → `drained`
- `drained` → `ready` (resume)

### UI Component

**Switch with AlertDialog confirmation** (shadcn/ui Switch + AlertDialog components)

```tsx
const [showDrainDialog, setShowDrainDialog] = useState(false)
const [pendingDrainState, setPendingDrainState] = useState(false)

const handleSwitchChange = (checked: boolean) => {
  if (checked) {
    // Enabling drain - show confirmation dialog
    setPendingDrainState(true)
    setShowDrainDialog(true)
  } else {
    // Disabling drain (resume) - no confirmation needed
    handleDrainToggle(false)
  }
}

const confirmDrain = () => {
  setShowDrainDialog(false)
  handleDrainToggle(true)
}

return (
  <>
    <div className="flex items-center gap-2">
      <Switch
        checked={node.status === 'draining' || node.status === 'drained'}
        onCheckedChange={handleSwitchChange}
        disabled={node.status === 'draining'} // disable during process
      />
      <label>Drain Mode</label>
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger>
            <HelpCircle className="h-4 w-4 text-muted-foreground" />
          </TooltipTrigger>
          <TooltipContent>
            <p>Gracefully migrate all services to other nodes.</p>
            <p>Node remains in cluster but won't receive new allocations.</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>

    <AlertDialog open={showDrainDialog} onOpenChange={setShowDrainDialog}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Drain Node</AlertDialogTitle>
          <AlertDialogDescription>
            This will gracefully migrate all running services from{' '}
            <code className="font-mono">{node.id}</code> to other nodes.
            The node will remain in the cluster but won't receive new allocations.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={confirmDrain}>
            Start Drain
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </>
)
```

**Sonner notifications:**

```tsx
import { toast } from 'sonner'

// On drain start
toast.loading('Draining node...', {
  id: 'drain-node-123',
  description: 'Migrating 4 allocations',
})

// Progress updates (via SSE)
toast.loading('Draining node...', {
  id: 'drain-node-123',
  description: 'Migrated 2/4 allocations',
})

// On completion
toast.success('Node drained', {
  id: 'drain-node-123',
  description: 'All allocations migrated successfully',
})

// On error
toast.error('Drain failed', {
  id: 'drain-node-123',
  description: 'Failed to migrate allocation xyz',
})
```

### API

#### `POST /api/v1/nodes/:id/drain`

Start draining node.

**Request:**
```json
{
  "enable": true  // true = start drain, false = resume (cancel drain)
}
```

**Response:**
```json
{
  "node_id": "dev-node-1",
  "status": "draining",
  "message": "drain initiated",
  "allocations_count": 4
}
```

#### `GET /api/v1/nodes/:id/drain/status`

Get drain progress (for sonner updates).

**Response:**
```json
{
  "node_id": "dev-node-1",
  "status": "draining",
  "total_allocations": 4,
  "migrated": 2,
  "remaining": 2,
  "current_allocation": "xhttp-abc123",
  "errors": []
}
```

#### SSE `/api/v1/stream`

Real-time drain progress events:

```json
{
  "event": "drain_progress",
  "data": {
    "node_id": "dev-node-1",
    "migrated": 2,
    "total": 4,
    "current": "xhttp-abc123"
  }
}
```

### Agent Commands

Используется существующий механизм `asty.v1.agent.{nodeID}.cmd`.

**Stop allocation command:**
```json
{
  "type": "stop",
  "allocation_id": "abc123",
  "graceful": true,
  "timeout": 30
}
```

---

## Implementation Plan

### Backend

1. [ ] Add `status` validation: `ready | draining | drained | down`
2. [ ] Implement `POST /api/v1/nodes/:id/drain` handler
3. [ ] Implement `GET /api/v1/nodes/:id/drain/status` handler
4. [ ] Add drain logic to Server: background task for migration
5. [ ] Add SSE events for drain progress (`drain_progress`)
6. [ ] Update Scheduler: skip `status != "ready"` nodes

### Frontend

1. [ ] Install sonner: `pnpm add sonner`
2. [ ] Add `<Toaster />` to App.tsx
3. [ ] Replace Pause/Drain buttons with Drain Switch in node-detail.tsx
4. [ ] Add Tooltip with HelpCircle icon to Switch
5. [ ] Add AlertDialog for drain confirmation (only when enabling drain)
6. [ ] Implement drain toggle handler with sonner notifications
7. [ ] Subscribe to SSE drain progress events
8. [ ] Update switch state based on SSE events

---

## Manual Node Removal

После drain ноду можно удалить вручную:

1. Переведи ноду в drain mode
2. Дождись статуса `drained` (все аллокации мигрировали)
3. Останови agent процесс: `kill <pid>` или через process manager
4. Server автоматически пометит ноду как `down` через 2 минуты (missed heartbeat)
5. Можно удалить данные ноды: `rm -rf /tmp/asty-dev/work/dev-node-X`

**Преимущества:** полный контроль, нет риска случайного удаления, можно проверить состояние перед удалением.

---

## Testing

### Drain Testing

1. Start 3-node cluster with services running
2. Toggle drain switch on node-1
3. Verify AlertDialog appears with confirmation
4. Click "Start Drain"
5. Verify:
   - Dialog closes
   - Status changes to `draining`
   - Sonner shows progress ("Migrating 4 allocations")
   - Allocations migrate one-by-one
   - New allocations don't land on node-1
   - Status becomes `drained` when done
   - Sonner shows success ("All allocations migrated successfully")
6. Toggle switch back (resume) — no dialog should appear
7. Verify status returns to `ready`
8. Verify new allocations can land on node-1 again

### Dialog Cancellation Test

1. Toggle drain switch on node-1
2. AlertDialog appears
3. Click "Cancel"
4. Verify:
   - Dialog closes
   - Switch returns to OFF position
   - Node status remains `ready`
   - No drain process initiated

### Edge Cases

- **Drain with no allocations**: should complete immediately → `drained`
- **Drain fails (no capacity)**: sonner shows error, rollback to `ready`
- **Node goes down during drain**: mark as `down`, reschedule remaining allocations
- **Resume during drain**: cancel background task, mark as `ready`, keep migrated allocations

---

## Notes

- **Switch disabled**: during `draining` process to prevent toggle spam
- **Sonner position**: top-right corner (default shadcn/ui position)
- **SSE connection**: reuse existing `/api/v1/stream` endpoint
- **Grace period**: default 30s for allocation stop (SIGTERM → SIGKILL)
- **Min nodes**: scheduler ensures min_copies even during drain
- **Manual cleanup**: admin responsibility after drain, no automatic deletion
