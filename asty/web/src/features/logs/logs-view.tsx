import { useEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { BACKOFF_BASE_MS, BACKOFF_MAX_MS, STREAM_MAX_RETRIES } from '@/lib/constants'
import { useT } from '@/lib/i18n'
import { parseEvent } from './format'
import { LevelFilter, type LevelFilterValue } from './level-filter'
import { LogRow } from './log-row'
import { StreamStatus, type StreamState } from './stream-status'
import { TailButton } from './tail-button'
import type { LogEvent } from '@/types'

interface LogsViewProps {
  // streamUrl is hit with `new EventSource(streamUrl)`. The backend's
  // content-negotiation switches to SSE because EventSource sets
  // Accept: text/event-stream automatically.
  streamUrl: string
  title?: string
  maxLines?: number
}

// DEFAULT_MAX matches the server-side ring buffer (logBufferLines=1000)
// so the UI's window is the same as the history endpoint can serve.
// Virtualisation keeps only the visible slice mounted, so 1000 is
// just the upper bound on the in-memory buffer — never the DOM size.
const DEFAULT_MAX = 1000

// FLUSH_MS bounds how often incoming SSE frames are applied to React
// state. A chatty service can push hundreds of events per second; the
// raw `setState`-per-event path re-renders the list each time, which
// is what makes a tab unresponsive. Buffering in a ref and flushing
// at ~6 fps keeps the renderer cheap while still feeling live.
const FLUSH_MS = 150

// SCROLL_STICKY_PX — how close to the bottom the viewport must be for
// auto-scroll to keep glueing to the tail. The moment a user scrolls
// up past this threshold we stop following so they can read history
// without the stream yanking them back.
const SCROLL_STICKY_PX = 40

// ROW_ESTIMATE_PX — initial guess for row height before each rendered
// row is actually measured. A small underestimate is harmless: virtual
// items are remeasured via ResizeObserver on mount, the virtual total
// size adjusts, and the scrollbar settles. Most rows land at 20-24px;
// error/fields rows expand to 32-48px and are measured the same way.
const ROW_ESTIMATE_PX = 22

// LogEntry pairs each parsed event with a monotonic id that becomes
// the React key. Stable ids let memo'd LogRow skip every row that's
// already on screen on each flush, and let the virtualiser keep
// element identity across scrolls.
interface LogEntry {
  id: number
  ev: LogEvent
}

// Module-level counter, shared across LogsView instances. Wraparound
// at 2^53 is hypothetical for any real session.
let entryCounter = 0

// LogsView is the single component every page uses to stream logs. It
// owns one EventSource, parses each SSE frame as a LogEvent, and hands
// it to a structured row renderer so levels are colour-coded, the
// component is chipped, error chains stand out, and remaining
// structured fields appear as small key=value tags.
//
// Only the rows currently visible in the viewport are mounted —
// @tanstack/react-virtual translates scroll offset into the
// (start, end) slice it actually renders, plus a small overscan for
// smooth scrolling. The container's scrollHeight is the virtual total
// from getTotalSize() so the native scrollbar behaves identically to
// a fully-mounted list.
export function LogsView({ streamUrl, title, maxLines = DEFAULT_MAX }: LogsViewProps) {
  const t = useT()
  const resolvedTitle = title ?? t('logs.title')
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [streamState, setStreamState] = useState<StreamState>('reconnecting')
  // reconnectKey increments when the operator clicks the dead-state
  // badge — it's a useEffect dep, so bumping it forces the EventSource
  // to be reopened with a fresh retryCount = 0.
  const [reconnectKey, setReconnectKey] = useState(0)
  const [filter, setFilter] = useState<LevelFilterValue>('all')
  const [tailing, setTailing] = useState(true)
  // unseen counts log lines that arrived after the user scrolled up.
  // The tail button uses it to show how far behind the live edge the
  // reader has drifted — and to colour-code the urgency.
  const [unseen, setUnseen] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const pendingRef = useRef<LogEntry[]>([])
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const tailingRef = useRef(true)

  // Keep a ref mirror of `tailing` so the scroll-flush logic inside
  // the SSE effect reads the latest value without re-creating the
  // effect on every toggle.
  useEffect(() => { tailingRef.current = tailing }, [tailing])

  const visible = useMemo(() => {
    if (filter === 'all') return entries
    return entries.filter((e) => (e.ev.level ?? '').toLowerCase() === filter)
  }, [entries, filter])

  const virtualizer = useVirtualizer({
    count: visible.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_ESTIMATE_PX,
    overscan: 10,
    getItemKey: (i) => visible[i].id,
  })

  useEffect(() => {
    let cancelled = false
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let es: EventSource | null = null

    const flush = () => {
      flushTimerRef.current = null
      const batch = pendingRef.current
      if (batch.length === 0) return
      pendingRef.current = []
      setEntries((prev) => {
        const combined = prev.concat(batch)
        return combined.length > maxLines
          ? combined.slice(combined.length - maxLines)
          : combined
      })
      if (tailingRef.current) {
        // One scroll per flush, not per event — keeps the work flat
        // when bursts arrive faster than the renderer can keep up.
        // Native scrollTop on the container (whose scrollHeight = the
        // virtualiser's total) snaps to the tail without depending on
        // a DOM sentinel.
        requestAnimationFrame(() => {
          const el = scrollRef.current
          if (el) el.scrollTop = el.scrollHeight
        })
      } else {
        // Reader is scrolled up — count what they're missing so the
        // tail button can light up amber/rose with the backlog size.
        setUnseen((n) => n + batch.length)
      }
    }

    const scheduleFlush = () => {
      if (flushTimerRef.current !== null) return
      flushTimerRef.current = setTimeout(flush, FLUSH_MS)
    }

    const open = () => {
      if (cancelled) return
      setStreamState('reconnecting')
      es = new EventSource(streamUrl)
      es.onopen = () => {
        retryCount = 0
        setStreamState('streaming')
      }
      es.onmessage = (event) => {
        pendingRef.current.push({ id: ++entryCounter, ev: parseEvent(event.data) })
        scheduleFlush()
      }
      es.onerror = () => {
        es?.close()
        if (cancelled) return
        retryCount++
        // Give up after STREAM_MAX_RETRIES (~10 min outage with the
        // backoff ceiling) — matches the per-resource SSE lifecycle
        // in store/cluster.ts so a permanently broken backend can't
        // have the browser hammering it forever. The badge flips to
        // 'dead' and exposes a click-to-reconnect affordance.
        if (retryCount > STREAM_MAX_RETRIES) {
          setStreamState('dead')
          return
        }
        setStreamState('reconnecting')
        retryTimer = setTimeout(open, Math.min(BACKOFF_BASE_MS * Math.pow(2, retryCount - 1), BACKOFF_MAX_MS))
      }
    }

    open()
    return () => {
      cancelled = true
      if (retryTimer) clearTimeout(retryTimer)
      if (flushTimerRef.current !== null) clearTimeout(flushTimerRef.current)
      pendingRef.current = []
      es?.close()
    }
  }, [streamUrl, maxLines, reconnectKey])

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget
    const nearBottom = el.scrollHeight - el.clientHeight - el.scrollTop <= SCROLL_STICKY_PX
    if (nearBottom !== tailing) {
      setTailing(nearBottom)
      // Reaching the bottom on your own counts the same as pressing
      // tail ↓ — the backlog is now seen.
      if (nearBottom) setUnseen(0)
    }
  }

  const resumeTail = () => {
    setTailing(true)
    setUnseen(0)
    requestAnimationFrame(() => {
      const el = scrollRef.current
      if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    })
  }

  const virtualItems = virtualizer.getVirtualItems()
  const totalSize = virtualizer.getTotalSize()

  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{resolvedTitle}</CardTitle>
        <div className="flex items-center gap-2">
          <span className="text-[11px] tabular-nums text-muted-foreground">
            {filter === 'all'
              ? t('logs.counter.max', { visible: visible.length, max: maxLines })
              : t('logs.counter.max_filtered', { visible: visible.length, total: entries.length, max: maxLines })}
          </span>
          {!tailing && (
            <TailButton unseen={unseen} onClick={resumeTail} />
          )}
          <LevelFilter value={filter} onChange={setFilter} />
          <StreamStatus state={streamState} onReconnect={() => setReconnectKey((k) => k + 1)} />
        </div>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col">
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex-1 min-h-0 overflow-x-hidden overflow-y-auto rounded-md border bg-muted/40 p-2 font-mono text-xs"
        >
          {visible.length === 0 ? (
            <div className="p-2 text-muted-foreground">{t('logs.empty')}</div>
          ) : (
            // height MUST be inline — virtualiser computes it from
            // measured/estimated row sizes and it changes every render.
            <div className="relative w-full" style={{ height: totalSize }}>
              {virtualItems.map((item) => (
                <div
                  key={item.key}
                  ref={virtualizer.measureElement}
                  data-index={item.index}
                  className="absolute top-0 left-0 w-full"
                  // transform MUST be inline — recomputed per scroll
                  // from the row's virtual start offset.
                  style={{ transform: `translateY(${item.start}px)` }}
                >
                  <LogRow ev={visible[item.index].ev} />
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
