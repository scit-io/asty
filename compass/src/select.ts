import type { Candidate, SelectOriginOptions, SelectionResult } from './types'

// Default ping target is the bare origin. A HEAD round-trip there
// measures TCP + TLS + first-byte RTT, which is what we actually care
// about for latency-based ranking; binding the probe to '/health'
// would couple the package to a specific server endpoint and treat a
// 404 (or any other non-2xx) as "node is dead", which is wrong.
const DEFAULT_HEALTH_PATH = '/'
const DEFAULT_TIMEOUT_MS = 1500
const DEFAULT_SCHEME = 'https://'
const DEFAULT_BOOTSTRAP_URL = '/api/v1'
// Cap how many hosts get probed when the bootstrap returns a large
// list. 16 is enough to find a good origin (random sample of a few
// dozen typically lands within a handful of ms of the global best)
// and leaves headroom for several failover hops without blowing up
// the cold-start budget.
const DEFAULT_MAX_CANDIDATES = 16

// selectOrigin returns just the chosen origin string. For the full
// breakdown (latencies, candidates) use select().
export async function selectOrigin(opts: SelectOriginOptions): Promise<string> {
  const result = await select(opts)
  return result.origin
}

// select fetches the host list, pings entries in parallel (sampled
// to maxCandidates when the list is large), and returns the lowest-
// latency origin together with the sorted candidate list.
export async function select(opts: SelectOriginOptions = {}): Promise<SelectionResult> {
  const bootstrapUrl = opts.bootstrapUrl ?? DEFAULT_BOOTSTRAP_URL
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS
  const healthPath = opts.healthPath ?? DEFAULT_HEALTH_PATH
  const scheme = opts.scheme ?? DEFAULT_SCHEME
  const port = opts.port
  const cap = opts.maxCandidates ?? DEFAULT_MAX_CANDIDATES
  const fb = opts.fallbackOrigin ?? defaultFallback()

  let hosts: string[]
  try {
    hosts = await fetchHosts(bootstrapUrl, timeoutMs)
  } catch (e) {
    if (fb) return fallback(fb)
    throw e
  }
  if (hosts.length === 0) {
    if (fb) return fallback(fb)
    throw new Error('@asty-web-app/compass: bootstrap returned no hosts')
  }

  const origins = sample(hosts.map((h) => hostToOrigin(h, scheme, port)), cap)
  const candidates = await pingAll(origins, healthPath, timeoutMs)
  candidates.sort(byLatency)
  const best = candidates[0]
  if (!best) throw new Error('@asty-web-app/compass: no candidates produced')

  return { origin: best.origin, candidates }
}

// sample takes up to `cap` random elements from `list` using a partial
// Fisher-Yates shuffle. `cap` <= 0 disables sampling. We avoid the
// "sort by Math.random" approach because the comparator isn't
// consistent — the resulting distribution is biased toward the
// original order on V8.
function sample<T>(list: T[], cap: number): T[] {
  if (cap <= 0 || list.length <= cap) return list
  const arr = list.slice()
  for (let i = 0; i < cap; i++) {
    const j = i + Math.floor(Math.random() * (arr.length - i))
    const tmp = arr[i]!
    arr[i] = arr[j]!
    arr[j] = tmp
  }
  return arr.slice(0, cap)
}

// byLatency puts reachable origins (latencyMs !== null) before
// unreachable ones, then ascending RTT within each group.
function byLatency(a: Candidate, b: Candidate): number {
  if (a.latencyMs === null && b.latencyMs === null) return 0
  if (a.latencyMs === null) return 1
  if (b.latencyMs === null) return -1
  return a.latencyMs - b.latencyMs
}

async function fetchHosts(url: string, timeoutMs: number): Promise<string[]> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), timeoutMs)
  try {
    const res = await fetch(url, { signal: ctl.signal, cache: 'no-store' })
    if (!res.ok) {
      throw new Error(`@asty-web-app/compass: bootstrap ${url} → ${res.status}`)
    }
    const data: unknown = await res.json()
    if (!Array.isArray(data) || !data.every((x) => typeof x === 'string')) {
      throw new Error('@asty-web-app/compass: bootstrap payload must be string[]')
    }
    return data as string[]
  } finally {
    clearTimeout(t)
  }
}

async function pingAll(
  origins: string[],
  path: string,
  timeoutMs: number,
): Promise<Candidate[]> {
  const tasks = origins.map(async (origin): Promise<Candidate> => {
    const t0 = performance.now()
    try {
      await pingOne(`${origin}${path}`, timeoutMs)
      return { origin, latencyMs: performance.now() - t0 }
    } catch {
      return { origin, latencyMs: null }
    }
  })
  return Promise.all(tasks)
}

async function pingOne(url: string, timeoutMs: number): Promise<void> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), timeoutMs)
  try {
    // Any response — including 404, 405, 401 — means the node
    // answered, so the RTT measurement is valid. The only failure
    // worth treating as "dead" is a network error or our own abort.
    await fetch(url, {
      method: 'HEAD',
      cache: 'no-store',
      signal: ctl.signal,
      credentials: 'omit',
    })
  } finally {
    clearTimeout(t)
  }
}

function hostToOrigin(host: string, scheme: string, port?: number): string {
  // Pass-through hosts that already carry a scheme — they may also
  // carry their own port, and second-guessing the caller is worse
  // than the few edge cases this misses.
  if (/^https?:\/\//i.test(host)) return host.replace(/\/$/, '')
  // Host already has an explicit ":port" — keep as-is.
  const trimmed = host.replace(/\/$/, '')
  if (/:\d+$/.test(trimmed)) return `${scheme}${trimmed}`
  return port ? `${scheme}${trimmed}:${port}` : `${scheme}${trimmed}`
}

function fallback(origin: string): SelectionResult {
  return { origin, candidates: [{ origin, latencyMs: null }] }
}

// defaultFallback returns the page's own origin in browsers, '' in
// non-browser hosts (tests, SSR). The empty string makes apiURL-style
// joiners produce relative URLs — usually the right baseline when no
// explicit fallback was supplied.
function defaultFallback(): string {
  if (typeof globalThis !== 'undefined') {
    const loc = (globalThis as typeof globalThis & { location?: Location }).location
    if (loc && typeof loc.origin === 'string') return loc.origin
  }
  return ''
}

// No persistent cache. Selection lives in install()'s module scope
// for the lifetime of the page; a reload re-runs the bootstrap so a
// long-dead node doesn't get re-picked from sessionStorage history.
