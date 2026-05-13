# Clarity — write for non-backend readers

The asty orchestrator codebase is read by people without deep backend experience. Phase 6.4 of the refactor restructured code with that audience in mind. Apply these rules to new code as well.

## Comments

All comment rules live in [comments.md](comments.md) — WHY-over-WHAT, key-focused YAML inlines, block style for shell envs, no value-centric comments.

## One file = one concept

`wait.go` waits, `tracker.go` tracks, `canary.go` does canary, `rolling.go` does rolling. A new reader can locate code by guessing the file name.

## No dead stubs

Anything that returns `"not yet implemented"` with a 200 OK is forbidden. Either implement, or return `http.StatusNotImplemented` (501). Half-truth responses lie to operators.

## No half-finished functions

If a method is unused or returns a placeholder, delete it. (Phase 6.2.10 removed the dead `Process.Context()` that returned a non-cancellable `context.Background()`.)

## Magic numbers → named constants with rationale

A non-developer cannot tell `5 * time.Second` from `5 * time.Minute` at a glance. Every timing or threshold constant carries a sentence-or-paragraph "why".

## Helpers with explanatory names

Inline a 3-line filter loop and the reader has to parse it; extract to `collectFailed()` and the loop disappears behind a self-documenting name. Same for `dispatchOne`, `markCurrent`, `recordError`, `completeNodeDrain`, etc.

## Avoid clever code

No bit-twiddling, no non-obvious recursion, no long chains of conditionals. If a reader needs to trace the recursion, restructure. See `SortDatacentersByProximity` for the canonical example of "remove the recursion, use `sort.Slice`".

## Status strings are typed enums

A reader scanning code sees `types.AllocRunning` and can cmd-click to the constant; with `"running"` they cannot.

## Function naming

Verbs for actions (`Reconcile`, `Send`, `Drain`), nouns for data, no `Get` prefix on plain accessors. When a method does I/O or work, name it for the work (`ListAllocations`, not `GetAllocations`).
