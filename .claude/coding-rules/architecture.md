# Architecture

Rules for where new code goes in the asty orchestrator package. Apply when deciding "which package should this live in".

## Top-level layout (inside the asty package root)

- `core/` — primitives with no domain knowledge of specific features. Currently: `config`, `types`, `errors`, `netutil`. New code goes here only if it is reused by 3+ features and references no feature-specific types.
- `features/` — vertical feature slices: `clustering`, `scheduling`, `autoscaling`, `deployment`, `draining`, `execution`, `observability`, `api`. Each feature owns its types, logic, and any sub-package it needs.
- `server/`, `agent/` — thin orchestrators that wire features together. They should not contain business logic; that lives in `features/`.

## Interfaces only at package boundaries

- `api.ServerContext` — interface defined in `features/api`, implemented by `server.Server`. Lets API handlers reach into server state without importing `server/` (which would create a cycle).
- `draining.DrainDeps` — same pattern: defined in `features/draining`, implemented by Server.
- `controller.SendStartCommand` — function type (not interface) because it is single-method.

Do not invent new interfaces preemptively. Add one only when:

1. Crossing a package boundary that would otherwise become circular.
2. A test needs to substitute a fake implementation.

## Sub-packages are sub-features, not file splits

- `clustering/state/` is a sub-package because cluster-state is a coherent sub-feature with its own KV-key conventions, watch helpers, and CAS retry logic.
- `scheduling/proximity/` is a sub-package because the DC latency matrix has its own lifecycle (load → validate → query) used by scheduling.
- `autoscaling/metrics/` is a sub-package because RPS time-series and scaling events are a self-contained data store.

Do not create a sub-package just because one file got big — split into multiple files in the same package instead.

## Server is a bag of dependencies

The `Server` struct fields are pointers to feature implementations; `Server` methods that satisfy `api.ServerContext` are one-line getters living in `server/context.go`. Real lifecycle is in `server/boot.go` (`Start()`) and per-feature files (`commands.go`, `deployment.go`, `leadership.go`, `logbuffer.go`, `metrics.go`, `nats.go`).

## Agent mirrors Server's structure

Thin top-level `agent.go` with struct + `Start`, plus feature-specific files for `services.go` (StartService/StopService), `heartbeat.go`, `restart.go`, `logstream.go`, `nodeinfo.go`, `commands.go`.

## Tests live next to code

Not in a sibling `_test` package. Use the existing `testutil/` for shared fixtures. Test fixtures must use typed constants (`types.AllocRunning`, `types.NodeReady`), never bare string literals.

## Cycles to watch

`server` ↔ `draining` was broken by `draining.DrainDeps`. `server` ↔ `api` was broken by `api.ServerContext`. If you find yourself importing back into `server`, add an interface in the consumer package.

## Path independence

Code under the asty package root should use only intra-package imports. Only the folder name `asty/` is stable — its parent path *may shift* between refactors. When referring to file locations in docs and comments, describe them relative to the package root (e.g. `features/draining/manager.go`), not by absolute path.
