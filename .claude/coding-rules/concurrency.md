# Concurrency — event-driven over polling

Asty is event-driven by design. Follow these rules when adding code that needs to react to state changes.

## ⚠️ Hall-of-shame — 2026-06-10

When asked to rewrite leader-election strictly event-driven and strictly by NATS canon, I:
- introduced `const watchRetryDelay = 2 * time.Second` IN THE FILE that was supposed to be the canonical strict-event-driven rewrite,
- justifying it to myself with "that's how the other watchers in this repo do it" (`RetryWatchForever(ctx, label, 2*time.Second, ...)` in streamhub_run.go / reconciler/watch.go).

That's a polling back-off, not an event. It belongs in the explicit allow-list below or nowhere. The user caught it on the next message.

Rule: **a new constant in code-written-to-satisfy-this-document whose name ends in `Delay` / `Interval` / `Sleep` is a self-audit failure.** Re-read the allow-list. Don't copy neighbouring patterns by reflex.

The correct pattern for "re-attach watcher on channel close": loop with no delay, re-call `bucket.Watch(ctx)` immediately. NATS auto-reconnect blocks the call during a disconnect, so on a healthy connection the loop only advances on real channel-close events.

## Default to event-driven

- For NATS KV state changes — use `state.WatchAllocations` / `state.WatchNodes` / `state.WatchAllocation` / `bucket.Watch`. The state package exposes a generic `watchKV` driver; build new watchers on top of it, not by polling `List*` in a ticker.
- For process exits — use `Process.OnExit(fn func(err error))` callback or `<-Process.Done()` channel. Both fire from the monitor goroutine when the OS process exits.
- For deploy/drain health waits — use `WatchAllocations` with a `healthTracker` state machine (see `ops/deployer/tracker.go`).

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
| `netutil.ConnectNATS` retry | 500 ms | Bootstrap race: server may start before the agent has the local broker bound |

Each of these has a comment at its `time.NewTicker` site explaining why polling is the right choice.

**Converted OUT of this list — now event-driven, no timer:** `agent.watchNATSPeers` and `server.watchStreamReplicas` both react to cluster-KV `WatchNodes` events (the latter also to NATS `gossipChanged`), and cluster SIZE is read from the JetStream RAFT meta on demand (`server.clusterSize`), not sampled. Earlier this table listed them as 5 s / 10 s pollers — that was stale doc; the code had no such tickers. Trust the code, delete the stale entry.

## Every timer: stop, classify, justify

When you ADD or REFACTOR any timer (`time.After`, `time.NewTicker`, `time.NewTimer`, `time.Sleep`, `time.AfterFunc`), STOP and name which kind it is — by reading the execution LOGIC, not the variable name:

- **Operation bound** — a request-reply timeout, or a SIGTERM→SIGKILL grace. Keep it, but the awaited reply/exit IS the event; the timer is only the fallback, so it must sit in a `select` next to that event channel (`select { case <-replyCh: …; case <-time.After(bound): … }`), never alone.
- **Backoff** before retrying a failed operation. Keep it, bounded (exponential preferred). A *clean* close re-opens immediately (event); only an *error* backs off.
- **Documented safety-net** already in the Acceptable-polling table above. Keep, with its rationale comment.
- **Polling for a state change** — DO NOT add. Convert to a watch/callback (`WatchNodes`, `Process.OnExit`/`Done()`, `gossipChanged`, a `$JS.EVENT.ADVISORY.*` subscription). Ask out loud: *what state change is this timer checking for, and which NATS event/callback already signals it?*

When a path becomes event-driven, DELETE its old timer — do not leave it "just in case" (that is the divergent second mechanism this whole file warns against).

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
