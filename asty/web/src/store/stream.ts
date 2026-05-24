import { BACKOFF_BASE_MS, BACKOFF_MAX_MS, STORE_FLUSH_MS, STREAM_MAX_RETRIES } from '@/lib/constants'

// ConnectionState — observable lifecycle of one SSE subscription. Not
// rendered directly anywhere; subscribers use it to wipe cached
// snapshots when the live feed drops (so the UI never paints stale
// values).
export type ConnectionState = 'connecting' | 'streaming' | 'reconnecting' | 'dead'

// Reusable EventSource lifecycle with exponential backoff reconnect.
// onState fires on every transition so the caller can mirror the
// connection status into a UI-visible field or trigger a cache wipe.
export function openStream(
  url: string,
  setup: (es: EventSource) => void,
  onState?: (state: ConnectionState) => void,
): () => void {
  let cancelled = false
  let retryCount = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let es: EventSource | null = null

  const open = () => {
    if (cancelled) return
    onState?.(retryCount === 0 ? 'connecting' : 'reconnecting')
    es = new EventSource(url)
    setup(es)
    es.onopen = () => {
      retryCount = 0
      onState?.('streaming')
    }
    es.onerror = () => {
      es?.close()
      if (cancelled) return
      retryCount++
      if (retryCount > STREAM_MAX_RETRIES) {
        onState?.('dead')
        return
      }
      onState?.('reconnecting')
      retryTimer = setTimeout(open, Math.min(BACKOFF_BASE_MS * Math.pow(2, retryCount - 1), BACKOFF_MAX_MS))
    }
  }

  open()
  return () => {
    cancelled = true
    if (retryTimer) clearTimeout(retryTimer)
    es?.close()
  }
}

// makeScheduleSet builds a per-store update scheduler. N updates within
// STORE_FLUSH_MS collapse into one zustand `set` so React renders the
// page once per snapshot, not once per event listener. The update
// functions still receive the latest running state, so reducers that
// read+merge (e.g. appendMetrics) stay correct. State (pendingUpdates,
// flushTimer) is closed over per call so each store instance owns its
// own batch — HMR / tests can re-create the store without timers
// leaking across instances.
export function makeScheduleSet<S>(set: (fn: (s: S) => Partial<S>) => void) {
  let pendingUpdates: Array<(s: S) => Partial<S>> = []
  let flushTimer: ReturnType<typeof setTimeout> | null = null

  return (fn: (s: S) => Partial<S>) => {
    pendingUpdates.push(fn)
    if (flushTimer !== null) return
    flushTimer = setTimeout(() => {
      flushTimer = null
      const fns = pendingUpdates
      pendingUpdates = []
      set((current) => {
        let next: S = current
        for (const fn of fns) {
          next = { ...next, ...fn(next) }
        }
        return next
      })
    }, STORE_FLUSH_MS)
  }
}
