# Asty coding rules

Conventions established during the Phase-6 refactor of the asty orchestrator package. The package lives at `**/asty/` — only the folder name is stable; the parent path may shift across refactors, so never hard-code it. Look for the directory containing `core/`, `features/`, `server/`, `agent/`.

When in doubt: read the topic file that matches the work you are doing.

- [file-layout.md](file-layout.md) — ≤200 lines per file, within-package vs sub-package splits, `doc.go`, `tunables.go` pattern.
- [code-idioms.md](code-idioms.md) — stdlib over handwritten, named constants, typed enums, helper extraction, method values, MustJSON, 501-over-fake-200.
- [concurrency.md](concurrency.md) — default to KV.Watch / OnExit / Done; acceptable polling list; `subscribers[T]`; debounce; initial-state-then-watch.
- [architecture.md](architecture.md) — core/features/server/agent split; interfaces only at boundaries; sub-packages are sub-features, not file splits.
- [testing.md](testing.md) — `go build/test/race/vet` required; `testutil` fixtures use typed enums; race-test concurrent code.
- [clarity.md](clarity.md) — one file = one concept; no dead stubs; magic numbers → named constants.
- [comments.md](comments.md) — WHY over WHAT, key-focused YAML inlines, block-style for shell envs, no value-centric comments.

History: these rules came out of the Phase-6 refactor (commits `19ea499` … `3450310` on `main`). They reflect what worked in practice and what we would do differently next time.
