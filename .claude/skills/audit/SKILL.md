---
name: audit
description: Wide-and-deep code audit — find wheels, overhead, dead code, bad idioms, comment quality (incl. Cyrillic), what's redundant, what's missing. Save to `.audit/{HH:MM_DD-MM-YY}.md` with prioritised plan and status-tracked tasks `[ ]`/`[~]`/`[+]`. Also covers "execute the audit" mode.
---

# /audit

## When to invoke

User asks for an audit, "find dead code / wheels / overhead", "tidy TODOs and comments". One call = one report. Don't use for narrow review of one file.

## Marker convention — STATUS, not priority

- `[ ]` TODO, not started
- `[~]` in progress
- `[+]` done

Fresh findings always start `[ ]`. User flips marker as work proceeds. Priority lives in **section** (Critical/High/Medium/Low) or order within the plan — never in the marker.

## What to look for

1. **Wheels** — handwritten stdlib (own split, own MaxInt, own Prometheus text vs `promhttp.Handler()`).
2. **Duplicates** — N copies of `envOr` / `durationOr` / `replyOK` etc.
3. **Dead code** — uncalled funcs/fields, 501-returning endpoints, UI buttons without backend, env flags with no effect, TCP listeners with no listener.
4. **Overhead** — N+1 to KV/DB, `time.Sleep` without `ctx`, missing HTTP-client timeouts, repeated sorts on hot path.
5. **Bad idioms** — stringly-typed enums, `interface{}` ↔ `any` mix, `fmt.Sscanf("%d")` over `strconv.Atoi`, `^uint(0)>>1` over `math.MaxInt`, `panic` where `log.Fatal` fits.
6. **Comments** — see `.claude/coding-rules/comments.md` for the full rule set; flag stale references, placeholders, dead links, and any comment that violates project comment conventions.
7. **Language** — Cyrillic in `*.go`/`*.ts`/`*.tsx`/configs outside `.audit/*`. Project is English-only.
8. **Redundant** — empty `Config struct{}` "for future", `error` returns always nil, dead fallback branches.
9. **Missing** — timeouts, contexts, tests on hot layers, env validation, `vet/race/tidy` Makefile targets, `doc.go` in sub-packages.
10. **Env drift** — structural delta between dev and prod environment configs (`feedback_dev_prod_sync`). Project-specific layout — discover the paths from the repo.
11. **TS ↔ Go drift** — frontend types referencing enums absent from Go.
12. **Architecture** — wrapper services that may be unnecessary (e.g. xws bridging WS over NATS). Raise as discussion item, not bug.

## Don't claim without verifying

- npm versions → WebFetch `https://registry.npmjs.org/<pkg>/latest` before "version X doesn't exist".
- Port/listener → `grep -rn "<port>"` before "no listener".
- Dead function → `grep -rn "<funcName>"` before "unused".
- Latest tooling → check current state, don't rely on training cutoff.

If a check is impossible, write "could not verify — assumption needs validation".

## Audit method

1. **Scale**: `wc -l` on `*.go`/`*.tsx`/`*.ts`, find largest files.
2. **Rules first**: read `CLAUDE.md` + all `.claude/coding-rules/*.md`. Findings must reference these.
3. **By layers**: 4–10 files in parallel via Read. Start at `core/`, move outward.
4. **Grep antipatterns**: see "Useful commands".
5. **Check documented exceptions** before flagging (e.g. file-size exceptions in `file-layout.md`).
6. **Check memory**: read `MEMORY.md`, especially `feedback_*` — verify each rule against current code.
7. **Cite**: every finding gets `path:line` anchors.

## Report structure

File `.audit/{HH:MM_DD-MM-YY}.md`, name from `date "+%H:%M_%d-%m-%y"`.

Header: scope (LOC, branch, commits) + marker convention.

Sections:

- `0.` Top summary, 5–10 bullets
- `1.` Critical — bugs, drift, lies to operator/user
- `2.` High — refactors, duplicates, antipatterns, architecture questions
- `3.` Medium — non-blocking improvements
- `4.` Low — cosmetic, names, comments
- `5.` Refactor plan — phases A/B/C, every task `[ ]`
- `6.` Not-defects — flag suspicious-but-fine things to head off "why didn't you catch X"
- `7.` Quality metrics table
- `8.` What's good — anchor as standard

Each task: `### N.M title [ ]` + `path:line` + minimal example + explicit **Action:** (one or two named variants, not "could consider").

## Don't

- Don't write "all good, no issues" — if true, skip the report.
- Don't duplicate existing docs — link them.
- Don't optimise preemptively — note "N+1 on hot path", don't rewrite in same commit.
- Don't apply fixes during the audit — audit describes, doesn't edit. Fixes happen in execute mode.
- Don't run linters/tests unless asked.
- Don't subjectivise — every claim cites a rule (`.claude/coding-rules/`, `CLAUDE.md`, language idiom) or measurable defect.
- **Don't hardcode project-specific paths** in this skill itself. Skills must work across projects — use generic discovery (`find .`, `git ls-files`) and let the model find the relevant directories.

## After saving

Path + 3–5 sentence summary. Don't restate the whole report.

---

## Execute-the-audit mode

When user says "приступай к выполнению .audit/<file>.md" or similar:

**Strictly task-by-task**, no batching. Cycle per task:

1. **Re-investigate** the task in code from scratch — verify it's real and the wording still applies. If stale or wrong, propose dropping as `[+]` (resolved by being non-issue).
2. **Brief in Russian, simple words.** Assume user doesn't remember context — pull them in. Minimal or no code blocks.
3. **Variants numbered `1..N`** with trade-offs. End with `Рекомендация: N — потому что …`. Use numbers, not letters.
4. **Wait for approval.** No edits until user picks a variant or ends discussion.
5. **Make minimal edits** strictly per the approved variant. If during
   investigation/implementation a pre-existing defect adjacent to the task
   is uncovered, surface it in the brief and offer a variant that fixes it.
   Never silently log it as "not my scope" — user wants the option to
   include the fix in the same task.
6. **Short report** — what changed, which files, what was verified (build/tests if relevant). No rationalisations.
7. **Wait for verdict** — approval (`[+]` + commit) or rework (`[~]` + discussion).
8. **Mark + commit** only after explicit approval. Commit message describes the task.
9. **Next task**: next `[ ]` in section; if none, take any `[~]`s in section; if section empty, next section. If plan empty, tell user audit is done.

### Anti-patterns for this mode

- Don't batch tasks.
- Don't add anything beyond the approved variant.
- Don't mark `[+]` without explicit approval.
- Don't skip re-investigate even for trivial tasks.
- Don't commit before approval.

## Language

Think in English (Russian is ~1.5× longer in tokens). Reply to user in Russian. Code identifiers and comments stay English per project rule.

## Useful commands

```bash
date "+%H:%M_%d-%m-%y"
find . -type f -name "*.go" -not -path "*/bin/*" -not -path "*/vendor/*" -print0 | xargs -0 wc -l | sort -rn | head -30
find . -type f \( -name "*.tsx" -o -name "*.ts" \) -not -path "*/node_modules/*" -not -path "*/ui/*" -not -path "*/dist/*" -print0 | xargs -0 wc -l | sort -rn | head -30
grep -rnE "TODO|FIXME|XXX|HACK" --include="*.go" --include="*.tsx" --include="*.ts"
grep -rcE "[А-Яа-я]" --include="*.go" --include="*.tsx" --include="*.ts" | grep -v ':0$'
grep -rnE "fmt\.Sscanf|fmt\.Sscan\b" --include="*.go"
grep -rn "interface{}" --include="*.go" | wc -l
# npm version check: WebFetch https://registry.npmjs.org/<pkg>/latest
```
