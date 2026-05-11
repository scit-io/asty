# Testing

Testing rules for the asty orchestrator package.

## Mandatory checks before claiming done

- `go build ./...` — must succeed.
- `go test ./...` — must pass.
- `go test -race -count=1 ./...` — must pass (concurrency is the rule, not the exception).
- `go vet ./...` — must be clean (no warnings).

Run them at the end of every phase / sub-phase. The Phase-6 refactor committed only when all four were green.

## Fixtures

Shared test fixtures live in `testutil/` (e.g. `NewTestConfig`, `NewTestNode`, `NewTestService`, `NewTestAllocation`). When the typed status enums were introduced, fixtures had to switch from `"running"` to `types.AllocRunning`. Always use typed constants in fixtures — they are part of the public test surface.

## Test fixture pattern

Each fixture is a constructor that returns a fully-populated struct. No "options" pattern; tests override fields after the constructor returns.

## Concurrent code requires race tests

The streamHub, controller workqueue, agent restart channel, drain manager, and process monitor all have race-tested code paths. New code that uses goroutines + shared state must be exercised under `-race`.

## Test next to code

Not in a sibling `_test` package. Package `scheduling` has `scheduler_test.go`; both files share the same package directive. This lets tests use unexported helpers without forcing them to be exported.

## Avoid network / external dependencies in unit tests

Tests that need NATS use the embedded test server; tests that need filesystem use `t.TempDir()`. No real cluster state.

## Test naming follows Go conventions

`Test<Type><Behavior>` (e.g. `TestSchedulerFilterHealthyNodes`, `TestMatrixSortByProximity`). Sub-tests via `t.Run("descriptive name", ...)` for table-driven cases.
