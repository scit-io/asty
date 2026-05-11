# File layout

Rules for organizing Go files inside the asty orchestrator package (`**/asty/` — only the folder name is stable; never hard-code the parent path).

**Hard cap on file size**: ≤200 lines per Go file (excluding tests). Target 150–180.

**One documented exception**: `features/clustering/controller/workqueue.go` (~214 lines). It is a cohesive k8s-style data structure (workqueue + delayed heap) that does not benefit from splitting. Any other file above 200 should be split.

**Why**: a 400+ line file forces context-window pressure when reading; a non-developer cannot hold a file like that in their head.

How to apply:
- Before merging a file with > 200 lines, split it.
- For new code, watch the line count as you write and split early.

**Splitting strategy — prefer within-package over sub-package**:
- Splitting within the same folder (multiple `.go` files, same `package X` declaration) is the default. Callers' imports do not change.
- Only create a new sub-folder (and thus a new sub-package) when the split represents a *cohesive sub-feature* with its own external API (e.g. `scheduling/proximity/`, `clustering/state/`, `autoscaling/metrics/`). Reason: every new sub-package forces import-path updates across callers; pay that cost only when the sub-package is genuinely independent.
- The audit doc initially specified sub-folders for several big files; in practice within-folder splits achieved the same readability goal at far lower churn. Future refactors should bias toward in-folder splits.

**One concept per file**:
- `features/draining/manager.go` — struct + lifecycle.
- `features/draining/run.go` — `runDrain` orchestrator.
- `features/draining/system.go`, `migrate.go`, `wait.go` — separate verbs.
- Same pattern for `deployment/{deployer,canary,rolling,wait,history,tracker}.go`, `autoscaling/{autoscaler,cooldown,scale_up,scale_down,execute}.go`, `scheduling/{scheduler,reconcile,candidates,helpers}.go`, `clustering/controller/{controller,reconcile,watch,autoscale}.go`, `execution/process/{process,stop,monitor,logs}.go`, etc.

**`doc.go`** in each sub-package: 5–10 lines of `// Package X does Y.` plus background that a new reader needs. Not required for in-folder splits — the per-file top comment already explains the slice.

**`tunables.go` pattern**: when a package has many named timing constants, group them into a `tunables.go` file (e.g. `server/tunables.go`). Keeps the boot/orchestration files focused on flow.
