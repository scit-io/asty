# Code idioms

Code-style rules for the asty orchestrator package. Apply when writing or modifying Go code anywhere under the asty package root.

## Use stdlib over handwritten utilities

- Splitting strings: `strings.Split`, `strings.Cut` (never iterate chars manually).
- Prefix checks: `strings.HasPrefix` (never `key[:len(prefix)] != prefix`).
- Slicing/copying: the `slices` package.
- Reading lines: `bufio.Scanner`, not char-by-char loops.
- Sorting: `sort.Slice`, never bubble sort.

A prior version had `splitLines`, `splitSubject`, `removeFromSlice`, hand-rolled prefix checks, and a bubble sort — all replaced with one-liners.

## Single source of truth — and follow NATS for cluster state

Cluster membership, replication, quorum, leadership and cluster-health follow NATS/JetStream's own model, **uniformly**. Never keep a SECOND copy of state that can drift from NATS / JetStream KV.

- Derive health **on demand** from NATS's own reported state (`StreamInfo.Cluster` → leader present + replicas `current && !offline`; JSZ meta peers). Don't cache a boolean you then have to keep in sync.
- Prefer JetStream/NATS APIs and the server's own quorum/replica behaviour over hand-rolled logic — don't reimplement what nats-server already manages.
- If a second surface must exist (a Prometheus gauge, a KV value other nodes read), it is a pure **projection** of the single source — recomputed on each read, never a separately-maintained copy.
- When unsure what NATS recommends, look it up and verify against the server's actual behaviour before inventing a mechanism.

Anti-pattern that motivated this: a `cluster.stable` flag held in BOTH an in-memory `atomic.Bool` (the kill-gate's authority) AND a KV key (what the snapshot/metric read), synced only by a transition-only publish. One failed KV write left the two permanently diverged — the metric read `false` while every stream was healthy (leader present, all replicas current, lag 0). The fix is ONE source, not a better sync.

## Named constants over magic numbers

Every timeout, interval, threshold, retry count gets a named constant with a one-paragraph comment explaining *why* the value has the magnitude it does. Example:

```go
// streamHubInterval — pure safety-net for missed KV-watch events.
// The reactive path (debounced rebuilds on each watcher event) is what
// drives normal updates; this fires only if no event has arrived
// within the interval. 60s is a sane "something must be wrong if no
// events have happened in a minute" cadence.
const streamHubInterval = 60 * time.Second
```

Non-developers reading the code learn the rationale alongside the value.

## Typed status enums, not bare string literals

Allocation/Node statuses (`AllocPending`, `AllocRunning`, `NodeReady`, `NodeDraining`, …) live in `core/types`. Compilers catch typos that runtime checks would miss. Helper methods (`AllocationStatus.IsLive()`, `NodeInfo.IsHealthy(now)`) replace inline switches/comparisons.

## Helpers when the same shape repeats 3+ times

- HTTP method guards (was 27 sites of boilerplate): `methodGuard(w, r, http.MethodGet)`.
- SSE driver: `runSnapshotStream` factored out of 4 handlers.
- Path parsing: `strings.Cut(path, "/")` replaces 3 hand-rolled loops.
- Generic fan-out: `subscribers[T]` with `add`/`fanout` replaces 3 mutex+map+nextID copies in streamHub.
- Generic KV watcher: one `watchKV` driver behind WatchNodes/WatchAllocations/Init variants.

## Method values over single-method interfaces

If an interface has exactly one method, pass a `func` value instead and store it. Example: `controller.SendStartCommand` is `func(nodeID string, svc *types.ServiceDefinition) error`, not an interface. The caller passes `s.sendStartCommand` directly without a wrapper struct.

## Pre-parse strings at load time

Convert `kill_timeout: "30s"` to `time.Duration` once via a `Resolve()` method, not on every getter call. The hot path reads the cached value; the getter falls back to a default if `Resolve()` was skipped.

## Defensive copies for shared state

Anything returned from a method that holds a lock should be cloned. Example: `Checker.GetStatus` returns a `cloneCheck(check)` so callers cannot mutate the locked map.

## Error wrapping

`fmt.Errorf("doing X: %w", err)` everywhere. Never `fmt.Errorf("failed: %v")` — that loses the error chain.

## Stubs return 501

Never a misleading 200 with `"not yet implemented"` text. Half-implemented endpoints lie to users; an honest 501 is better.

## No "Get" prefix on Go-idiomatic accessors

`node.Datacenter` over `node.GetDatacenter()`. The audit codebase still has some `GetX()` methods (e.g. on `ServiceDefinition`) for compatibility — new code should drop the prefix.

## MustJSON pattern

`types.MustJSON(v)` panics-or-empty for marshaling, used in SSE/NATS payload paths where the marshaling is "should never fail" and the call sites must stay one-line. One implementation in `core/types`, not three copies.
