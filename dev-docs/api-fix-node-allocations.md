# API Fix: Node Allocations Display

## Problem

When clicking on a node in ui, the Services tab shows "No services running on this node" even if the node has running processes.

## Root Cause

1. **Frontend** (`ui/src/pages/node-detail.tsx`):
   - Calls `api.getNodeAllocations(nodeId)` which makes request: `GET /api/v1/allocations?node_id=${nodeId}`
   - Expects response: `{ allocations: [...], count: N }`

2. **Backend** (`internal/platform/asty/api.go`):
   - Handler `handleAllocations()` only supported `?service=<name>` parameter
   - Did NOT support `?node_id=<id>` parameter
   - Returned error message for unsupported query

## Solution

Added support for `node_id` query parameter in `handleAllocations()`:

```go
func (api *API) handleAllocations(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	nodeID := r.URL.Query().Get("node_id")

	// ... handle serviceName case ...

	if nodeID != "" {
		// Get allocations for specific node - iterate through all services
		var allAllocs []*ServiceAllocation
		for _, svc := range api.server.services {
			allocs, err := api.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == nodeID {
					allAllocs = append(allAllocs, alloc)
				}
			}
		}

		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id":     nodeID,
			"allocations": allAllocs,
			"count":       len(allAllocs),
		})
		return
	}
	// ...
}
```

## Additional Fix

Updated node-detail Overview tab to show allocation count instead of `node.processes.length`:

**Before:**
```tsx
<div className="text-2xl font-bold">{node.processes.length}</div>
```

**After:**
```tsx
<div className="text-2xl font-bold">{allocations.length}</div>
```

This ensures the count reflects actual running service allocations, not just process names.

## Testing

To verify:
1. Start asty server: `./bin/asty -mode server -api :4747`
2. Start asty agents on nodes (they will register and create allocations)
3. Open ui: http://localhost:5173
4. Click on a node → go to Services tab
5. Should see table with service allocations (Service Name, Status, Health, CPU, Memory, Restarts)

## API Response Format

**Request:** `GET /api/v1/allocations?node_id=node-abc123`

**Response:**
```json
{
  "node_id": "node-abc123",
  "allocations": [
    {
      "id": "gateway-node-abc123-1234567890",
      "service_name": "gateway",
      "node_id": "node-abc123",
      "status": "running",
      "version": "v1.0.0",
      "pid": 12345,
      "started_at": "2026-05-04T10:00:00Z",
      "health_status": "healthy",
      "cpu_usage": 15,
      "memory_usage": 128,
      "restarts": 0,
      "created_at": "2026-05-04T09:59:00Z",
      "updated_at": "2026-05-04T10:00:00Z"
    }
  ],
  "count": 1
}
```

## Files Changed

1. `internal/platform/asty/api.go` — added `node_id` parameter support in `handleAllocations()`
2. `ui/src/pages/node-detail.tsx` — fixed service count to use `allocations.length`
