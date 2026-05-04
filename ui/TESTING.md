# Testing Astiui

## Current Status

The UI works with **existing** Asty API endpoints and gracefully handles missing ones.

### Working with Current API

Dashboard and Node Detail pages work with existing endpoints:
- ✅ `GET /api/v1/status` — cluster status
- ✅ `GET /api/v1/nodes` — nodes list
- ✅ `GET /api/v1/allocations?node_id=X` — allocations by node

Missing endpoints have **fallbacks**:
- Node Detail: falls back to data from `/api/v1/nodes`
- Service Detail: falls back to data from `/api/v1/allocations`
- Logs: gracefully shows "No logs available"
- Actions: buttons are visible but API calls may fail (need endpoints)

## Testing Locally

### Option 1: Static File (No API)

Open directly in browser:
```bash
open dist/index.html
```

**Expected behavior:**
- Dashboard loads empty state (no API)
- Shows loading skeletons → "No nodes found"

### Option 2: With Asty Server

1. Start Asty server:
```bash
cd /Volumes/SSD/dev/UPWAY\ LC/up.mt/asty
./bin/asty -mode server
```

2. Start UI dev server with API proxy:
```bash
cd astiui
pnpm dev
```

3. Open http://localhost:5173

**Expected behavior:**
- Dashboard shows cluster stats + nodes table
- Click node → shows node detail (tabs work, uses fallback data)
- Click service → shows service detail (tabs work, uses fallback data)
- Logs/Actions tabs may show "not available" (missing endpoints)

### Option 3: Production Build with Asty

Serve from Asty (requires updating `api.go` to serve static files):

```bash
# Build UI
cd astiui
pnpm build

# Copy to Asty (if you want to embed)
# Option: update internal/platform/asty/ui.go to serve from astiui/dist/
```

## What Works Now

✅ **Dashboard:**
- Cluster stats cards
- Nodes table with status, CPU, Memory
- Theme toggle
- Click to navigate to node

✅ **Node Detail:**
- Overview tab (CPU, Memory, Services count, Last Seen)
- Services tab (allocations table, click to navigate)
- Logs tab (shows UI, but needs endpoint)
- Actions tab (buttons shown, but need endpoints)

✅ **Service Detail:**
- Overview tab (allocation details)
- Health tab (health status)
- Logs tab (shows UI, but needs endpoint)
- Actions tab (Restart/Stop buttons)

✅ **Theme:**
- Light/Dark/System modes
- Persisted to localStorage
- Toggle works

## Missing API Endpoints

These endpoints would enhance functionality but are not critical:

### Node Detail Endpoint (has fallback)
```
GET /api/v1/nodes/:id
Response: { id, datacenter, status, cpu_total, cpu_available, ... }
```

Currently falls back to data from `GET /api/v1/nodes` list.

### Allocation Detail Endpoint (has fallback)
```
GET /api/v1/allocations/:id
Response: { id, service, status, node_id, cpu_usage, ... }
```

Currently falls back to data from `GET /api/v1/allocations?node_id=X`.

### Logs Endpoints (no fallback)
```
GET /api/v1/logs/node/:id
GET /api/v1/logs/allocation/:id
Response: { logs: ["line1", "line2", ...] }
```

Shows "No logs available" when missing.

### Action Endpoints (no fallback)
```
POST /api/v1/nodes/:id/drain
POST /api/v1/nodes/:id/pause
POST /api/v1/allocations/:id/restart
POST /api/v1/allocations/:id/stop
```

Buttons trigger API calls but will fail if endpoints don't exist.

## Known Limitations

1. **No real-time logs streaming** — logs tab shows static snapshot
2. **No metrics charts** — chart component exists but needs data
3. **No confirmation dialogs** — actions execute immediately (should add)
4. **Errors not toasted** — only shown in console (should add toast notifications)

## Next Steps for Full Functionality

1. Add missing endpoints in `internal/platform/asty/api.go`
2. Implement confirmation dialogs for destructive actions
3. Add toast notifications for success/error
4. Add metrics endpoints and wire up charts
5. Consider SSE for real-time updates

## Browser Compatibility

Tested in:
- ✅ Chrome/Edge (latest)
- ✅ Safari (latest)
- ✅ Firefox (latest)

Requires modern browser with ES2023 support.
