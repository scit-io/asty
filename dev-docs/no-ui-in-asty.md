# Asty — API-only, No Built-in UI

## Change Summary

Asty orchestrator no longer includes a built-in Web UI. It provides **only HTTP JSON API**.

### What Was Removed

1. **Old `ui/` directory** — Previous Vite-based UI (removed completely)
2. **`internal/platform/asty/ui.go`** — Embedded HTML dashboard (removed)
3. **`handleUI()` function** — Replaced with `handleRoot()` that returns API info as JSON

### What Remains

**ui/** — Separate React application (shadcn/ui based) that connects to Asty API as an external monitoring tool.

## API Root Path Behavior

**Before:**
```
GET http://localhost:4747/
→ Returns embedded HTML dashboard
```

**After:**
```
GET http://localhost:4747/
→ Returns JSON:
{
  "service": "Asty Orchestrator",
  "version": "0.1.0",
  "api": "/api/v1",
  "endpoints": {
    "status": "/api/v1/status",
    "nodes": "/api/v1/nodes",
    "services": "/api/v1/services",
    "allocations": "/api/v1/allocations",
    "health": "/health",
    "metrics": "/metrics"
  },
  "docs": "https://github.com/yourorg/asty"
}
```

## Monitoring Workflow

1. **Start Asty** (API server):
   ```bash
   ./bin/asty -mode server
   ```
   API available at: http://localhost:4747/api/v1

2. **Start ui** (separate dev server):
   ```bash
   cd ui
   pnpm dev
   ```
   UI available at: http://localhost:5173

3. **ui** makes API requests to `http://localhost:4747/api/v1/*` via Vite proxy

## Benefits

- **Separation of concerns** — Asty focuses on orchestration logic, ui focuses on visualization
- **Independent deployment** — ui can be deployed separately (Vercel, Netlify, static hosting)
- **Multiple UIs** — Different teams can build their own monitoring dashboards using Asty API
- **Smaller binary** — No embedded HTML/CSS/JS in Asty binary
- **Better development** — ui has HMR, modern tooling, independent versioning

## Files Changed

- `internal/platform/asty/api.go` — `handleUI()` → `handleRoot()`, returns JSON
- `internal/platform/asty/ui.go` — **deleted**
- `ui/` directory — **deleted** (old UI)
- `CLAUDE.md` — updated to mention API-only approach
- `deployments/envs/dev/start.sh` — removed old `ui` from cleanup

## Migration Notes

If you previously relied on embedded UI at `http://localhost:4747/`:
- Use ui instead: `cd ui && pnpm dev`
- Or build custom UI using Asty's HTTP API
- Or use curl/httpie for CLI monitoring: `curl http://localhost:4747/api/v1/status | jq`
