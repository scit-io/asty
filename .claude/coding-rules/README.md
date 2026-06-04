# Asty coding rules

When in doubt: read the topic file that matches the work you are doing.

- [refactoring.md](refactoring.md) — every refactor pass: trust execution logic over comments/names/docs; re-verify NATS recommendations each time; make it cheaper; cut overhead/говнокод; stop+classify every timer.
- [code-idioms.md](code-idioms.md) — stdlib over handwritten, named constants, typed enums, helper extraction, method values, MustJSON, 501-over-fake-200, single source of truth (follow NATS).
- [concurrency.md](concurrency.md) — default to KV.Watch / OnExit / Done; acceptable polling list; every-timer-stop-and-classify; `subscribers[T]`; debounce; initial-state-then-watch.
- [testing.md](testing.md) — `go build/test/race/vet` required; `testutil` fixtures use typed enums; race-test concurrent code.
- [clarity.md](clarity.md) — one file = one concept; no dead stubs; magic numbers → named constants.
- [comments.md](comments.md) — WHY over WHAT, key-focused YAML inlines, block-style for shell envs, no value-centric comments.
