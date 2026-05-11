# Concurrency — event-driven over polling

Asty was deliberately converted from a polling architecture to an event-driven one in Phase 6.3. Follow these rules when adding code that needs to react to state changes.

## Default to event-driven

- For NATS KV state changes — use `state.WatchAllocations` / `state.WatchNodes` / `state.WatchAllocation` / `bucket.Watch`. The state package exposes a generic `watchKV` driver; build new watchers on top of it, not by polling `List*` in a ticker.
- For process exits — use `Process.OnExit(fn func(err error))` callback or `<-Process.Done()` channel. Both fire from the monitor goroutine when the OS process exits.
- For deploy/drain health waits — use `WatchAllocations` with a `healthTracker` state machine (see `features/deployment/tracker.go`).

## Acceptable polling — only with documented rationale

| Where | Period | Why polling |
|---|---|---|
| `leader.CampaignForLeader` refresh | 5 s | TTL physics — must rewrite KV entry before TTL expires |
| `controller.periodicResync` | 60 s | Safety net for missed events; watchers handle the reactive path |
| `agent.publishHeartbeat` | 5 s | Proof of life — must be periodic |
| `agent.publishProcessMetrics` | 10 s | Sampling /proc — physical sampling rate |
| `MetricsCollector.Start` | EvalInterval | Sampling CPU% — same as above |
| `HealthChecker.Start` | 1 s | External HTTP probes — must be initiated periodically |
| `Process.TailLogs` | 100 ms | File reading; fsnotify complicates log rotation |
| `proximity.RunValidation` | 1 hour | Heavy job, slow drift |
| `streamHubInterval` | 60 s | Pure safety net for missed events; debounce drives normal path |

Each of these has a comment at its `time.NewTicker` site explaining why polling is the right choice.

## Exponential backoff for retries

`core/netutil.EnsureBucket` is the model: 100 ms → 3 s with a 30 s total budget. Linear `30×1s` would burn 30 s every cold start; exponential lets typical cases finish in ~100 ms.

## Subscriber / fan-out pattern

When fanning out events to multiple consumers (SSE, channels), use a generic `subscribers[T]` helper rather than three copies of mutex+map+nextID. See `server/streamhub_subs.go`. Slow consumers drop on full channel — never back-pressure the publisher.

## Debounce vs throttle

When a watcher fires bursts of updates (alloc status flips), debounce. `streamHub.driveLoop` waits 500 ms after the last event before rebuilding a snapshot, which collapses a 100-event burst into one rebuild.

## Process callbacks must not block

`Process.OnExit(fn)` runs on the monitor goroutine; `fn` must do non-blocking work (e.g. `select { case ch <- name: default: }`). Drop-on-full beats deadlock-the-monitor.

## Initial state, then watch

When a watch is used as "wait for condition X to become true", check current state once before subscribing. If the condition is already true (e.g. replacement already exists in KV before drain started), skip the watcher entirely. See `DrainManager.healthyReplacementExists` as the canonical example.
