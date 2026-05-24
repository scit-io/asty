// Project-wide tunables that don't belong to any single feature.
// Group by domain so the table stays readable; if a constant has a
// single consumer (e.g. SCROLL_STICKY_PX inside LogsView), let it
// live next to that consumer — this file is for what crosses
// boundaries or describes the runtime as a whole.

// --- SSE / stream lifecycle ------------------------------------------

// Backoff base for EventSource reconnects (ms). First retry fires
// after BACKOFF_BASE_MS, then doubles each attempt up to BACKOFF_MAX_MS.
export const BACKOFF_BASE_MS = 3000

// Backoff ceiling (ms). Past this we stop accelerating — operator
// either fixes the backend or the cap kicks in.
export const BACKOFF_MAX_MS = 60000

// Max reconnect attempts before the stream is declared dead. With
// BACKOFF_MAX_MS at 60s, 10 attempts cover ~10 minutes of outage —
// past that we stop hammering the server. Applies to every SSE the
// SPA opens (store's per-resource feeds AND LogsView).
export const STREAM_MAX_RETRIES = 10

// --- Store batching ---------------------------------------------------

// STORE_FLUSH_MS bounds how often SSE-driven updates land in zustand.
// The backend already debounces snapshots at 500 ms, but a single
// snapshot fans out as multiple event types ('status', 'nodes',
// 'services', 'metrics', …) — without batching those would be N
// separate setState calls and N re-renders per snapshot. 100 ms keeps
// updates feeling live (10 fps) while collapsing each snapshot into
// one render burst regardless of how many event listeners fire for it.
export const STORE_FLUSH_MS = 100

// MAX_CHART_POINTS — chart window size, points kept in memory per
// series. 5 minutes at 5s cadence = 60 points.
export const MAX_CHART_POINTS = 60

// --- Per-feature polling ----------------------------------------------

// AUTOSCALER_POLL_MS — cadence for the per-service /autoscaler REST
// poll that feeds Overview's configuration card and the Scaling events
// tab. The data doesn't move at SSE pace; 15 s is the operator-
// expected freshness.
export const AUTOSCALER_POLL_MS = 15000
