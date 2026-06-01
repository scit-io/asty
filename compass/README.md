# @asty-web-app/compass

Locality-aware origin selection for Asty SPAs. Picks the lowest-
latency live node at startup and routes `fetch` / `EventSource`
through it with automatic failover when the chosen node drops.

## Install

```sh
npm install @asty-web-app/compass
```

## Quick start

```ts
import { install, fetch, EventSource } from '@asty-web-app/compass'

await install()

const res = await fetch('/api/v1/services')
const es = new EventSource('/dashboard/v1/stream')
```

`install()` with no arguments uses production defaults — see
[Configure](#configure) for the full list. `fetch` and `EventSource`
are drop-in replacements for the browser globals; if you'd rather not
shadow the globals, alias on import:

```ts
import {
  install,
  fetch as compassFetch,
  EventSource as CompassEventSource,
} from '@asty-web-app/compass'
```

## How it works

`install` fetches the host list from `bootstrapUrl`, sends a `HEAD`
to each candidate to measure RTT, and remembers the ranked list in
memory. The lowest-latency origin becomes "preferred".

Once installed:

* `fetch(input, init)` prefixes relative paths with the preferred
  origin and retries on the next candidate when the response is 5xx
  or the request throws a network error. Successful retries promote
  the responding origin to preferred. Absolute URLs that match a
  candidate go through the same failover; other absolute URLs pass
  through untouched.
* `EventSource(url, init?)` owns an internal native EventSource and
  re-opens it on the next candidate when the current connection
  closes for good. `addEventListener` subscribers and `on*` handlers
  survive the swap.

Both paths share preferred-origin state, so a fetch failover updates
which node future EventSource reconnects target, and vice versa.

`origin()` returns the live preferred origin string; `subscribe(fn)`
notifies you on every change.

## Configure

Every option is optional.

```ts
await install({
  // Where the host list is fetched from. Default '/api/v1' on the
  // page's own origin.
  bootstrapUrl: 'https://asty.example.com/api/v1',

  // Used when the bootstrap fetch fails or returns no hosts.
  // Default globalThis.location?.origin.
  fallbackOrigin: 'https://asty.example.com',

  // HEAD probe path on each candidate. Default '/'.
  healthPath: '/',

  // Per-request budget for both the bootstrap fetch and each probe.
  // Default 1500 ms.
  timeoutMs: 1500,

  // Scheme prefixed to bare hostnames returned by the bootstrap.
  // Default 'https://'. Use 'http://' in dev.
  scheme: 'https://',

  // Port appended to bare hostnames. No port by default — set this
  // when the gateway listens on a non-standard port.
  port: 7060,

  // Cap on how many hosts compass actually pings. Larger lists are
  // random-sampled down to this number. Default 16; 0 disables
  // sampling (ping every host).
  maxCandidates: 16,

  // Log selection and failover events through console.info.
  // Default false.
  debug: false,

  // Fires on every failover. Same shape as subscribe().
  onOriginChange: (origin) => { /* … */ },
})
```

## Inspect

`install` returns a client for diagnostics:

```ts
const client = await install()

client.origin                 // current preferred origin (live)
client.selection.candidates   // [{ origin, latencyMs }, …] sorted best-first
client.subscribe((next) => {
  // fires on each failover; returns an unsubscribe function
})
```

Or read the live origin from anywhere without holding the client:

```ts
import { origin, subscribe } from '@asty-web-app/compass'

origin()                      // string, '' before install resolves
subscribe((next) => { /* … */ })
```

## Low-level building blocks

If you want to drive the lifecycle yourself:

```ts
import { select, selectOrigin, createApiFetch } from '@asty-web-app/compass'

const result = await select(/* same options as install */)
const apiFetch = createApiFetch({ selection: result })

const res = await apiFetch('/foo')
```

`select` does the bootstrap pass and returns a `SelectionResult`.
`selectOrigin` returns just the chosen origin string. `createApiFetch`
builds a fetch wrapper bound to the selection; pass `fetch: <ref>` to
override the underlying implementation (defaults to `globalThis.fetch`).

## TypeScript

All types are exported from the entry point:

```ts
import type {
  InstallOptions,
  CompassClient,
  SelectOriginOptions,
  SelectionResult,
  Candidate,
  CompassFetchOptions,
} from '@asty-web-app/compass'
```

## License

MIT.
