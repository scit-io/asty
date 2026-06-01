import type { CompassFetchOptions } from './types'

// createApiFetch returns a fetch-shaped wrapper that prepends the
// currently-preferred origin to any relative URL and, on a network
// error or a 5xx response, transparently retries on the next candidate
// from the SelectionResult. The preferred origin is sticky: a
// successful call promotes the responding origin to "preferred" for
// subsequent calls in the same session.
//
// Absolute URLs (http:// or https://) pass through unchanged — the
// caller has already chosen an origin and the wrapper shouldn't
// second-guess.
export function createApiFetch(opts: CompassFetchOptions): typeof fetch {
  let preferred = opts.selection.origin
  const candidateOrigins = opts.selection.candidates.length
    ? opts.selection.candidates.map((c) => c.origin)
    : [opts.selection.origin]
  const shouldRetry = opts.shouldRetry ?? defaultShouldRetry
  const onOriginChange = opts.onOriginChange
  const realFetch: typeof fetch = opts.fetch ?? globalThis.fetch.bind(globalThis)

  return async function apiFetch(
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> {
    const path = extractPathWithCandidates(input, candidateOrigins)
    if (path === null) {
      // Absolute URL aimed at a third party — pass through unchanged.
      return realFetch(input, init)
    }

    // Try the preferred origin first, then the rest of the ranked list
    // minus the preferred (which we already tried).
    const order = [preferred, ...candidateOrigins.filter((c) => c !== preferred)]
    let lastErr: unknown
    let lastRes: Response | null = null
    for (const origin of order) {
      try {
        const res = await realFetch(origin + path, init)
        if (shouldRetry(res, null)) {
          lastRes = res
          continue
        }
        if (origin !== preferred) {
          preferred = origin
          onOriginChange?.(origin)
        }
        return res
      } catch (err) {
        lastErr = err
        if (!shouldRetry(null, err)) {
          throw err
        }
      }
    }
    if (lastRes !== null) return lastRes
    throw lastErr ?? new Error('@asty-web-app/compass: all candidates failed')
  }
}

// extractPath returns the path+query portion of input. Relative URLs
// are normalised to start with '/'. Absolute URLs whose origin
// matches one of our candidates get their origin stripped — that lets
// SPA callers pass `${__ASTY_ORIGIN__}/dashboard/v1/foo` and still
// participate in failover. Absolute URLs aimed at other origins
// return null so the wrapper passes them through unchanged.
function extractPathWithCandidates(
  input: RequestInfo | URL,
  candidates: string[],
): string | null {
  const raw = toUrlString(input)
  if (!/^https?:\/\//i.test(raw)) {
    return raw.startsWith('/') ? raw : '/' + raw
  }
  for (const origin of candidates) {
    if (raw.startsWith(origin + '/')) return raw.slice(origin.length)
    if (raw === origin) return '/'
  }
  return null
}

function toUrlString(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input
  if (input instanceof URL) return input.toString()
  return (input as Request).url
}

// defaultShouldRetry retries on network errors (no Response) and on
// 5xx (Response with status >= 500). Everything else — including
// 4xx — is returned to the caller unchanged.
function defaultShouldRetry(res: Response | null, _err: unknown): boolean {
  if (res === null) return true
  return res.status >= 500
}
