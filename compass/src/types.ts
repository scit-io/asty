// Options consumed by select() and selectOrigin().
export interface SelectOriginOptions {
  // bootstrapUrl is the URL that returns the live host list as a JSON
  // array of strings. Defaults to '/api/v1' — the gateway's host-list
  // endpoint on the SPA's own origin. Override only when the SPA is
  // served from a different domain than the gateway.
  bootstrapUrl?: string

  // healthPath is the path requested on each candidate to measure
  // latency. Default "/" — a HEAD round-trip there gives the same
  // RTT signal without depending on a particular server endpoint
  // (any response counts as "node answered"). Override only if your
  // edge needs a specific path to even reach the origin.
  healthPath?: string

  // timeoutMs is the per-request budget — applied to both the bootstrap
  // fetch and to each ping. Default 1500ms. The slowest plausible
  // long-haul RTT plus a couple of TCP retransmits.
  timeoutMs?: number

  // fallbackOrigin is returned when the bootstrap fetch fails or
  // produces no hosts. Defaults to globalThis.location?.origin
  // (the page's own origin) — the most useful baseline in browser
  // SPAs. Override to point at a different anchor host.
  fallbackOrigin?: string

  // scheme is the protocol prefixed to hosts that arrive without one.
  // Default "https://".
  scheme?: 'http://' | 'https://'

  // port, when set, is appended to every host that arrives without an
  // explicit port. Useful in dev where the gateway lives on a non-
  // standard port (e.g. 7060 for the Asty dashboard) and the
  // bootstrap response is a bare list of hostnames. Hosts that
  // already carry a scheme are passed through unchanged.
  port?: number

  // maxCandidates caps how many hosts compass actually pings. Larger
  // host lists are random-sampled down to this number — pinging 1000
  // origins on every page load is wasteful, and a random sample of
  // ~16 gives enough probes to find a low-latency node and enough
  // alternates for failover. Set 0 or negative to disable sampling
  // (ping every host). Default 16.
  maxCandidates?: number
}

// One entry in SelectionResult.candidates.
export interface Candidate {
  origin: string
  // latencyMs is the measured RTT in milliseconds, or null when the
  // ping failed (timeout, connection refused, non-2xx). null also
  // survives JSON serialization cleanly — Infinity would have become
  // null on a sessionStorage roundtrip anyway.
  latencyMs: number | null
}

// Output of select(). selectOrigin() returns just the chosen origin
// string from this shape.
export interface SelectionResult {
  // origin is the chosen scheme://host[:port] with no trailing slash.
  // Equals candidates[0].origin.
  origin: string

  // candidates is the full list of probed origins, sorted by latency
  // ascending (best first). Failed origins (latencyMs === null) sit
  // at the end. createApiFetch() reads this for runtime failover.
  candidates: Candidate[]
}

// Options for install — superset of SelectOriginOptions with debug
// and an origin-change hook.
export interface InstallOptions extends SelectOriginOptions {
  // debug, when true, logs bootstrap progress and failover events
  // through console.info. Off by default.
  debug?: boolean

  // onOriginChange fires when failover promotes a new origin to
  // "preferred". Same callback shape subscribe() uses.
  onOriginChange?: (origin: string) => void
}

// CompassClient is what install() returns. Most apps only need the
// `origin` getter for code that displays the current backend; fetch
// and EventSource are available as top-level exports from the
// package and already do the right thing by the time install resolves.
export interface CompassClient {
  // origin is the currently-preferred backend, live (reflects any
  // failover that has happened since bootstrap).
  readonly origin: string
  // selection is the full SelectionResult that bootstrap returned —
  // exposes the ranked candidate list and per-host latencies for any
  // page that wants to surface them.
  readonly selection: SelectionResult
  // subscribe registers a callback fired on origin change. Returns an
  // unsubscribe function.
  subscribe(listener: (origin: string) => void): () => void
}

// Options for createApiFetch.
export interface CompassFetchOptions {
  // selection is the SelectionResult that the bootstrap pass returned.
  // createApiFetch reads `candidates` to set up the failover order and
  // tracks the currently-preferred origin internally.
  selection: SelectionResult

  // fetch is the underlying fetch implementation the wrapper calls.
  // REQUIRED when callers intend to monkey-patch window.fetch with
  // the returned function — passing the post-patch global there
  // would cause infinite recursion on every request. Capture
  // window.fetch BEFORE the patch (e.g. `window.fetch.bind(window)`)
  // and pass it here. Defaults to globalThis.fetch.
  fetch?: typeof fetch

  // shouldRetry decides whether a Response is a hard failure (try the
  // next candidate) or an acceptable answer (return it). Default
  // treats 5xx and network errors as retryable; 4xx is returned as-is.
  shouldRetry?: (res: Response | null, err: unknown) => boolean

  // onOriginChange fires when failover promotes a different origin to
  // "preferred". The new value is the responding origin from the last
  // successful request. Useful for mirroring the choice into
  // window.__ASTY_ORIGIN__ or a Zustand store so the rest of the SPA
  // sees a single source of truth.
  onOriginChange?: (origin: string) => void
}
