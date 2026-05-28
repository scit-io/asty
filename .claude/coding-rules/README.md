# Asty coding rules

When in doubt: read the topic file that matches the work you are doing.

- [code-idioms.md](code-idioms.md) — stdlib over handwritten, named constants, typed enums, helper extraction, method values, MustJSON, 501-over-fake-200.
- [concurrency.md](concurrency.md) — default to KV.Watch / OnExit / Done; acceptable polling list; `subscribers[T]`; debounce; initial-state-then-watch.
- [testing.md](testing.md) — `go build/test/race/vet` required; `testutil` fixtures use typed enums; race-test concurrent code.
- [clarity.md](clarity.md) — one file = one concept; no dead stubs; magic numbers → named constants.
- [comments.md](comments.md) — WHY over WHAT, key-focused YAML inlines, block-style for shell envs, no value-centric comments.
