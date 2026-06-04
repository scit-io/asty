# Refactoring — what to check on every pass

Run this checklist whenever you refactor (especially the asty server/agent cluster code). It is in addition to the topic rules; where a point has a detailed home, follow the link.

## Trust the execution logic — NOT comments, names, or docs

Comments, variable/function names, `.asty` fields, `README`/`CLAUDE.md`, and even this file are **claims**. Verify each against what the code actually does at runtime — the execution logic is the only source of truth. A mismatch is a finding: fix the stale claim, never preserve it or reason from it.

Real example from this codebase: `concurrency.md` listed `watchNATSPeers` and `watchStreamReplicas` as 5 s / 10 s pollers long after the code had become event-driven (`WatchNodes` + `gossipChanged`, no ticker). The doc was wrong; the code was right. If you'd trusted the doc you'd have "preserved" a poll that doesn't exist.

## Follow NATS/JetStream — and re-verify EACH time

For anything touching cluster membership, replication, quorum, leadership, or health: do it the way NATS/JetStream recommends, and **check the NATS docs/behaviour each time** rather than assuming. Prefer NATS's own APIs and reported state over hand-rolled logic — don't reimplement what nats-server already manages.

Authoritative cluster membership is the **JetStream RAFT meta group** (read on demand via JSZ), NOT gossip `DiscoveredServers` (a cumulative discovery set that never shrinks and goes stale after a solo collapse) and NOT a separately-maintained count. See the single-source rule in [code-idioms.md](code-idioms.md) and the size source `server.clusterSize`.

## Make it cheaper

Look for work repeated needlessly: a handle/client constructed per call (hoist it — e.g. one shared `jetstream.JetStream` on the server, not `jetstream.New` per call); a `List*` or round-trip on a hot path; allocations inside a loop. Before deciding what's hot, count the call frequency — per request? per snapshot build? per heartbeat? — and optimise the genuinely hot path, not a cold one.

## Cut overhead and cruft (говнокод)

Delete what isn't pulling weight: dead/unused functions, half-finished stubs, duplicate mechanisms doing the same job, band-aids layered over a wrong root cause, a second source of truth that can drift from the first. If a solution turned out not to work, REMOVE it — do not leave it sitting next to the real fix.

## Every timer: stop and classify

Stop on each timer (`time.After`, `NewTicker`, `NewTimer`, `Sleep`, `AfterFunc`) and decide what kind it is: operation-bound / backoff / documented-safety-net (keep, with rationale) vs polling-for-a-state-change (convert to a watch or callback). Full rule and the acceptable-polling allow-list are in [concurrency.md](concurrency.md). Default is event-driven; a surviving timer needs an explicit, written justification at its site.
