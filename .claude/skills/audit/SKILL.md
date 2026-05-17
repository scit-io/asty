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

## Mandatory check on every audit: demo-service artefacts inside `asty/`

Run this **before** anything else. The platform (`asty/`) must
contain no references to specific managed services. **Scope is
strictly `asty/`**: demos appearing in `demo/`, `deploy/`,
`Makefile`, and coding-rule examples are intentional
customer-facing boilerplate (see
[[project-demo-boilerplate]]) — not findings.

Three independent passes, file:line for every match, all scoped
to `asty/`:

1. **Names** — grep (case-insensitive) for demo service names
   (`xauth`, `xhttp`, `xws`, `demo`) under `asty/` only.
2. **Shapes** — grep under `asty/` for demo-shaped paths and
   subjects (`api.v1.`, `/v1/auth`, `/login`, `/refresh`,
   `X_AUTH_*`, `JWT`, demo bucket names, hardcoded subject
   formats like `fmt.Sprintf("api.v1.%s…"`).
3. **Tests and comments** — re-grep `asty/internal/**/*_test.go`
   and comments for demo-shaped examples — fixtures named
   `xauth`/`xhttp`, comments showing `/v1/auth/login` as the
   example. These are easy to miss because they read like docs.

Report as the **first Critical finding** of every audit
(§1.1), even if empty — write "verified clean" with `[+]` so
the reader knows the check was done. See
[[feedback-audit-no-demo-artefacts]],
[[project-demo-boilerplate]].

## Mandatory check on every audit: read labels, not names

For every Prometheus instrument (or any labelled data point —
typed events, log fields, NATS subject segments) the audit
touches, **list the labels and trace the increment sites** before
making any claim about what the data is. The metric NAME tells
you where it was registered; the LABELS and the CALL SITES tell
you what the data actually describes.

A counter named `gateway_http_requests_total{service, method, status}`
is not "about the gateway" just because of its prefix — if the
`service` label's value is a user-facing service name, the data
is about that service. The naming itself is then a finding (the
name lies about the subject).

See [[feedback-audit-read-labels-not-names]].

## Mandatory check on every audit: format discipline

Binary-first is a project-wide rule (see `project_binary_first`).
Every audit MUST grep the touched scope for `json.Marshal`/
`json.Unmarshal` outside the codec package and verify each use is
on an allowed format boundary — HTTP responses to the dashboard
or CLI, Server-Sent Events payloads, Prometheus text, or
application logs. Anything else is a **bug from a previous
iteration**, not a stylistic remark — report it as such, with the
specific file:line and a one-line fix (swap to `codec.Wire`/
`codec.State`).

The rule has exactly five exit doors: HTTP, SSE, Prometheus text,
logs (zerolog), and the dev-mode escape hatch (`UseJSONForDev`).
Every other JSON site is a finding.

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
13. **Surface mirror gaps** — when auditing an observability area (logs, metrics, events, status), list ALL parallel surfaces it touches (dashboard SSE, `/metrics`, JSON API, NATS subjects, CLI) and verify the audit covers each. The mirror rule in `CLAUDE.md` ("UI and Prometheus stay in lockstep") is enforceable, not aspirational — auditing one surface in isolation misses half the picture and is a rejectable defect. Before writing the report, enumerate the surfaces explicitly in your head: "this feature is visible at X, Y, Z — have I checked each?"

## Don't claim without verifying — EVERY claim, EVERY section

This is the core rule of the audit skill. An audit that contains one
unverified statement is compromised in full and the user is right to
throw the whole report away — not just the offending line. Sloppy
audits are worse than no audit, because they push the user into
wasting time chasing claims that don't hold.

### Trust only the executing code — not docs, not comments, not identifiers

**The body of a function is the only source of truth.** Every
other surface lies eventually:

- **Doc comments at the top of structs / functions / packages.**
- **Prose paragraphs in CLAUDE.md, READMEs, design docs.**
- **Function names** — a function called `validateInput` may not
  validate.
- **Variable names** — a variable called `safeBuffer` may not be
  safe.
- **Struct / type names** — `GatewayConfig` may hold fields that
  don't belong to a gateway.
- **File names** — `metrics.go` may not be where the increment
  happens.
- **Package names** — `gateway/metrics/` may register metrics
  about user-facing services, not about the gateway.
- **Prometheus metric names** — `gateway_http_requests_total`
  whose `service` label takes user-service values is metric
  about a user service, not about the gateway. The name prefix
  lies.
- **NATS subject literals quoted in docs**, command-line examples
  copied from old branches, anything that isn't running code.

During an audit:
- Read functions and call graphs. Read what gets written to the
  response, registered on the mux, branched on, looped over.
  Trace label values back to their assignment site.
- For every claim, the source is the unrolled call graph —
  never an identifier or a comment.
- When name and behaviour disagree, the **name is itself a
  finding**. Report it: "name X promises Y, body does Z." Don't
  silently rename — surface the gap so the user decides whether
  to rename the symbol or change the behaviour.
- Documentation drift is reportable on its own — "comment at
  file:line claims X, code does Y."

This rule was added after consecutive audit failures: the API
doc comment promised three-way content negotiation while the
code did two; the `gateway_*` Prometheus counters described user
services while their name said gateway. Each time the name was
misleading and I leaned on it instead of reading the body. The
rule applies to every audit going forward.

See [[feedback-audit-trust-only-execution]],
[[feedback-audit-read-labels-not-names]],
[[feedback-intent-implementation-drift]].

- npm versions → WebFetch `https://registry.npmjs.org/<pkg>/latest` before "version X doesn't exist".
- Port/listener → `grep -rn "<port>"` before "no listener".
- Dead function → `grep -rn "<funcName>"` before "unused".
- Latest tooling → check current state, don't rely on training cutoff.
- Subject names, file paths, line numbers, struct field lists, function
  signatures, counts ("two formatters", "three shapes") — re-check
  against the code at write time. Do not paraphrase from memory of
  what you read minutes ago.
- Cross-cutting claims ("same prefix", "every path", "always",
  "never") — verify both halves. Each universal quantifier multiplies
  the verification burden.
- The **top summary is not a free zone**. It restates findings from
  later sections; if the later section is loose, the summary inherits
  the looseness — and the user reads the summary first.

If a check is impossible in this pass, write "could not verify —
assumption needs validation" instead of asserting.

**Fresh-eyes rule on re-audit.** When re-auditing a file or topic
after a prior pass (yours or another agent's), do not lean on the
previous finding's wording. Re-read the code from scratch. Prior
conclusions are a starting hypothesis to test, not a fact to inherit
— they may have been wrong, or the code may have changed since.

**No quiet rewording during edits.** If the user rejects a finding
and you edit the report, do not tighten language ("the same prefix",
"both", "always") without re-verifying the new wording against code.
A "fix" that introduces a new unverified claim re-compromises the
report.

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

### Coupling exception

If the clean solution to task A naturally extends into task B (same new
package, same file, same refactor pattern, splitting forces creating
throwaway structure or leaves obviously incomplete work), surface the
coupling in the brief and offer a variant that does both under one
approval and one commit. Don't artificially decompose tightly-coupled
tasks just to honour the audit's numbering — that produces churn and
worse code organisation. Only split when the two halves can stand alone
with no shared scaffolding.

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
