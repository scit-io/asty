import { select } from './select'
import { createApiFetch } from './apiFetch'
import type {
  InstallOptions,
  CompassClient,
  SelectionResult,
} from './types'

// Module-scoped state — single source of truth for the currently
// preferred origin, the candidate ranking, and the listeners
// observing failover events. Initialised by install(); read by the
// public functions exported alongside install().
let selection: SelectionResult | null = null
let preferred: string | null = null
let nativeFetch: typeof globalThis.fetch | null = null
let NativeES: typeof EventSource | null = null
let logDebug = false
const listeners = new Set<(origin: string) => void>()

// install runs the bootstrap pass and captures the native fetch and
// EventSource implementations. Side-effect free on the rest of the
// global object: nothing on window gets replaced, no globals are
// written. The SPA imports `fetch`, `EventSource`, and `origin` from
// this package and uses them directly.
//
// Returns a CompassClient for diagnostics (origin getter, subscribe).
export async function install(opts: InstallOptions): Promise<CompassClient> {
  logDebug = !!opts.debug
  const t0 = performance.now()
  const result = await select(opts)
  selection = result
  preferred = result.origin
  nativeFetch = globalThis.fetch.bind(globalThis)
  NativeES = globalThis.EventSource
  if (logDebug) {
    console.info('[compass] selected', result.origin,
      'in', Math.round(performance.now() - t0), 'ms',
      'candidates:', result.candidates)
  }
  if (opts.onOriginChange) listeners.add(opts.onOriginChange)
  for (const l of listeners) l(result.origin)
  return {
    get origin() { return preferred ?? result.origin },
    get selection() { return result },
    subscribe(listener) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
  }
}

// origin returns the current preferred backend origin or '' when
// compass hasn't been installed yet. The empty fallback lets the SPA
// keep working with relative URLs in environments where compass is
// skipped (e.g. same-origin builds, tests).
export function origin(): string {
  return preferred ?? ''
}

// subscribe lets non-React code observe failover. For React, prefer
// reading `origin()` inside an effect keyed off subscribe's events.
export function subscribe(listener: (origin: string) => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

// fetch is a drop-in replacement for the global. SPA modules should
// import it instead of relying on window.fetch — same signature,
// same return type, but with transparent failover across the compass
// candidate list when the preferred origin 5xxs or goes unreachable.
//
//   import { fetch } from '@asty-web-app/compass'
//   const res = await fetch('/api/v1/services')
//
// When compass isn't installed (origin() === '') the call passes
// through to the original global fetch verbatim.
export async function fetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  if (!selection || !nativeFetch) {
    return globalThis.fetch(input, init)
  }
  if (!wrappedFetch) {
    wrappedFetch = createApiFetch({
      selection,
      fetch: nativeFetch,
      onOriginChange: (o) => publish(o, 'fetch'),
    })
  }
  return wrappedFetch(input, init)
}
let wrappedFetch: typeof globalThis.fetch | null = null

// EventSource is a drop-in replacement for the browser class. SPA
// modules construct it as they would the native one; under the hood
// it owns an internal native EventSource and rewires it to the next
// compass candidate when the current one drops, preserving
// addEventListener subscribers and on* handlers across the swap.
//
//   import { EventSource } from '@asty-web-app/compass'
//   const es = new EventSource('/dashboard/v1/stream')
//
// When compass isn't installed, falls through to the native class.
export const EventSource: typeof globalThis.EventSource =
  makeWrappedEventSource() as unknown as typeof globalThis.EventSource

// publish updates the cached preferred origin and notifies
// subscribers. Called from both the fetch and the EventSource paths
// so they can't drift apart. Suffix is a string for debug logging.
function publish(origin: string, source: string): void {
  if (preferred === origin) return
  preferred = origin
  if (logDebug) console.info('[compass]', source, 'failover →', origin)
  for (const fn of listeners) fn(origin)
}

// nextOrigin walks the candidate list circularly, returning the entry
// after `from`. Returns null when the list has fewer than two members
// or `from` is the only reachable origin left.
function nextOrigin(from: string): string | null {
  if (!selection) return null
  const list = selection.candidates
  if (list.length < 2) return null
  const i = list.findIndex((c) => c.origin === from)
  if (i === -1) return list[0]?.origin ?? null
  const j = (i + 1) % list.length
  if (list[j]?.origin === from) return null
  return list[j]?.origin ?? null
}

// ─── EventSource wrapper ───────────────────────────────────────────
//
// Browser EventSource auto-reconnects to the same URL forever. To
// move connections off a dead node we own an inner EventSource and
// recreate it on the next compass origin when the current one errors,
// re-attaching every subscriber. The wrapper is a plain EventTarget
// subclass — not `implements EventSource`, because lib.dom's
// overloaded addEventListener tied to the EventSource event map is
// too narrow for a generic re-emitter. We cast at the export site so
// SPA call-sites still see the standard EventSource type.

// PROBE_TIMEOUT_MS bounds the HEAD check that runs on EventSource
// reconnect errors. Short enough not to stall the wrapper noticeably
// during transient blips, long enough to ride out a fresh TCP
// handshake on a slow link.
const PROBE_TIMEOUT_MS = 1500

// isOriginAlive sends one HEAD to the given origin and returns true
// for any response (the node is reachable, the error must be SSE-
// specific) or false on network error / timeout (the node is gone).
// `credentials: 'omit'` matches the bootstrap probe in select.ts so
// the result is symmetric.
async function isOriginAlive(origin: string, timeoutMs: number): Promise<boolean> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), timeoutMs)
  try {
    await globalThis.fetch(origin, {
      method: 'HEAD',
      cache: 'no-store',
      signal: ctl.signal,
      credentials: 'omit',
    })
    return true
  } catch {
    return false
  } finally {
    clearTimeout(t)
  }
}

function makeWrappedEventSource(): {
  new (url: string | URL, init?: EventSourceInit): unknown
} {
  type AnyListener = EventListenerOrEventListenerObject
  type Pair = {
    type: string
    listener: AnyListener
    options?: AddEventListenerOptions | boolean
  }

  class CompassEventSource extends EventTarget {
    static readonly CONNECTING = 0
    static readonly OPEN = 1
    static readonly CLOSED = 2
    readonly CONNECTING = 0
    readonly OPEN = 1
    readonly CLOSED = 2

    readyState: number = 0
    url: string = ''
    readonly withCredentials: boolean

    onopen: ((this: EventSource, ev: Event) => unknown) | null = null
    onmessage: ((this: EventSource, ev: MessageEvent) => unknown) | null = null
    onerror: ((this: EventSource, ev: Event) => unknown) | null = null

    private es: EventSource | null = null
    private path: string | null = null
    private currentOrigin: string | null = null
    private listenerPairs: Pair[] = []
    private rotations = 0
    private closed = false
    private probing = false
    private initOpts: EventSourceInit | undefined

    constructor(urlInput: string | URL, opts?: EventSourceInit) {
      super()
      this.withCredentials = !!opts?.withCredentials
      this.initOpts = opts
      const raw = typeof urlInput === 'string' ? urlInput : urlInput.toString()
      // No compass yet — fall straight through to the native class.
      // Library not initialised, or a build/test where install() was
      // never called.
      if (!selection || !NativeES) {
        const native = new (NativeES ?? globalThis.EventSource)(raw, opts)
        this.adoptNative(native)
        return
      }
      const candidates = selection.candidates.map((c) => c.origin)
      const match = candidates.find(
        (o) => raw.startsWith(o + '/') || raw === o,
      )
      if (!match) {
        // Cross-origin SSE — wrapper degrades to a plain passthrough.
        const native = new NativeES(raw, opts)
        this.adoptNative(native)
        return
      }
      this.path = raw === match ? '/' : raw.slice(match.length)
      this.currentOrigin = match
      this.openInner(match + this.path)
    }

    // adoptNative wires a plain native instance into the wrapper —
    // used in the "no failover needed" branches. on* and
    // addEventListener still go through the wrapper so callers see a
    // consistent EventTarget.
    private adoptNative(native: EventSource): void {
      this.es = native
      this.url = native.url
      this.readyState = native.readyState
      native.onopen = (ev) => {
        this.readyState = native.readyState
        if (this.onopen) this.onopen.call(native, ev)
        this.dispatchEvent(new Event('open'))
      }
      native.onmessage = (ev) => {
        if (this.onmessage) this.onmessage.call(native, ev)
        this.dispatchEvent(cloneMessageEvent('message', ev))
      }
      native.onerror = (ev) => {
        this.readyState = native.readyState
        if (this.onerror) this.onerror.call(native, ev)
        this.dispatchEvent(new Event('error'))
      }
    }

    // openInner builds a fresh native instance against the current
    // compass origin, reattaches the wrapper's existing listeners,
    // and routes its events through dispatchEvent so external
    // subscribers survive the swap.
    private openInner(url: string): void {
      if (!NativeES) return
      this.url = url
      this.readyState = 0
      const es = new NativeES(url, this.initOpts)
      this.es = es
      es.onopen = (ev) => {
        this.readyState = es.readyState
        this.rotations = 0
        if (this.onopen) this.onopen.call(es, ev)
        this.dispatchEvent(new Event('open'))
      }
      es.onmessage = (ev) => {
        if (this.onmessage) this.onmessage.call(es, ev)
        this.dispatchEvent(cloneMessageEvent('message', ev))
      }
      es.onerror = (ev) => {
        this.readyState = es.readyState
        if (this.onerror) this.onerror.call(es, ev)
        this.dispatchEvent(new Event('error'))
        if (this.closed) return
        if (es.readyState === 2 /* CLOSED */) {
          this.rotateOrigin()
          return
        }
        void this.probeAndMaybeRotate()
      }
      for (const p of this.listenerPairs) {
        es.addEventListener(p.type, (ev) => {
          this.dispatchEvent(cloneMessageEvent(p.type, ev as MessageEvent))
        })
      }
    }

    private async probeAndMaybeRotate(): Promise<void> {
      if (this.probing || this.closed || !this.currentOrigin) return
      this.probing = true
      try {
        const alive = await isOriginAlive(this.currentOrigin, PROBE_TIMEOUT_MS)
        if (!alive && !this.closed) this.rotateOrigin()
      } finally {
        this.probing = false
      }
    }

    private rotateOrigin(): void {
      if (this.path === null || this.currentOrigin === null) return
      if (!selection) return
      this.rotations++
      if (this.rotations > selection.candidates.length) {
        if (logDebug) console.info('[compass] sse exhausted candidates')
        this.readyState = 2
        return
      }
      const next = nextOrigin(this.currentOrigin)
      if (!next || next === this.currentOrigin) {
        this.readyState = 2
        return
      }
      this.currentOrigin = next
      publish(next, 'sse')
      this.es?.close()
      this.openInner(next + this.path)
    }

    addEventListener(
      type: string,
      listener: AnyListener,
      options?: AddEventListenerOptions | boolean,
    ): void {
      super.addEventListener(type, listener, options)
      this.listenerPairs.push({ type, listener, options })
      this.es?.addEventListener(type, (ev) => {
        this.dispatchEvent(cloneMessageEvent(type, ev as MessageEvent))
      })
    }

    removeEventListener(
      type: string,
      listener: AnyListener,
      options?: EventListenerOptions | boolean,
    ): void {
      super.removeEventListener(type, listener, options)
      this.listenerPairs = this.listenerPairs.filter(
        (p) => !(p.type === type && p.listener === listener),
      )
    }

    close(): void {
      this.closed = true
      this.readyState = 2
      this.es?.close()
    }
  }
  return CompassEventSource
}

// cloneMessageEvent rebuilds a MessageEvent under a new `type` so the
// wrapper can re-dispatch the native one while preserving `data`,
// `lastEventId`, and `origin`. MessageEvent constructor is the only
// way to populate these after creation.
function cloneMessageEvent(type: string, src: MessageEvent): MessageEvent {
  return new MessageEvent(type, {
    data: src.data,
    lastEventId: src.lastEventId,
    origin: src.origin,
  })
}
