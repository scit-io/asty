# Comments

Rules for comments across all files in this repo. Skills must not duplicate these — link here.

## Core principle: comments explain WHY, not WHAT

The identifier name says WHAT; the comment says WHY this value, this trade-off, this defensive copy. A comment that just restates the code rots immediately. A comment that records a constraint or a decision survives refactors.

Bad:

```go
// Increment migrated counter
op.status.Migrated++
```

Good — already implicit from name; if a comment is added, it justifies the lock release / publish:

```go
// bumpMigrated increments the counter and publishes a progress event.
// Used by both the system and regular paths.
```

## Go code

- Doc comment for every exported symbol (`Package X does Y.`, `Func X returns Y.`, etc.).
- Unexported helpers: comment only when behaviour is non-obvious. Self-evident names need no commentary.
- Magic numbers → named constants with a sentence-or-paragraph "why".
- Never describe the current task / fix / caller (`// added for issue #123`, `// used by X` — belongs in PR description, rots in code).

## YAML configs (`.asty`, `config.asty`)

**Every parameter gets a one-line inline comment** describing the **KEY** (the purpose of the parameter — what knob it is, units, role). If the value itself carries meaning (an enum choice, environment-specific tuning, a magic number), append it in parentheses:

```
key: value   # Description of what the key is (value — value-specific note)
```

Good:

```yaml
type: service       # Scheduling mode (service — autoscaled; system — one per node)
kill_timeout: 30s   # Grace period after SIGTERM before SIGKILL
user: nobody        # OS user the process runs as (nobody — non-root, limits damage)
replicas: 3         # NATS replication factor (server/kv.go falls back if cluster smaller)
max_parallel: 2     # Instances replaced in parallel during rolling update (2 — faster feedback in dev)
```

Bad (value-centric, what NOT to do):

```yaml
kill_timeout: 30s   # Wait 30s before SIGKILL              # describes the value
type: service       # Autoscaled by Asty                   # describes one specific value
max_parallel: 2     # Replace 2 instances at a time        # value-centric
```

Section header comments (multi-line block above a section) are good in addition to per-parameter inlines — they never substitute. Value-centric comments become wrong the moment the value changes; key-focused comments age gracefully.

Lists: items get inline comments on the first scalar of each entry (e.g. `- bucket: name  # ...`).

## Shell env files (`.env` and similar)

`KEY = value` style files cannot carry inline comments (the shell parser would swallow `# ...` into the value). Use **block comments above each key**, key-focused, with a **blank line between blocks** for readability.

Visual section separators (`# === === === ===`) for major zones (NATS auth, host hardware, per-service tunables, etc.). Section headers do not substitute for per-key block comments.

Good:

```
# =============================================================================
# xauth (demo) — JWT authentication service
# =============================================================================

# Accepted login username.
X_AUTH_USERNAME       = "admin"

# HMAC key for signing access tokens (short-lived).
X_AUTH_ACCESS_SECRET  = "dev-access-secret"
```

Bad — no spacing between blocks, hard to scan:

```
# Accepted login username.
X_AUTH_USERNAME       = "admin"
# HMAC key for signing access tokens (short-lived).
X_AUTH_ACCESS_SECRET  = "dev-access-secret"
```

## Files / packages

`doc.go` per sub-package: 5–10 lines of `// Package X does Y.` plus background a new reader needs. Top-of-file comments for in-package splits document the slice of behaviour the file owns.

## What NOT to comment

- Don't restate the obvious. Well-named identifiers are the primary documentation.
- Don't reference the current task / PR / issue — belongs in the commit message or PR description.
- Don't leave dead `// TODO` markers without an actionable plan.
- Don't write placeholder comments (`// TODO: explain later`).
