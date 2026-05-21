import { useEffect, useMemo, useRef, useState } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { componentStyle, formatTime, levelStyle, parseEvent } from '@/components/logs-format'
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
// Each row is a small flex container — 1000 of them is comfortable for
// any modern browser.
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

const LEVELS = ['trace', 'debug', 'info', 'warn', 'error', 'fatal'] as const
type Level = (typeof LEVELS)[number]

// LogsView is the single component every page uses to stream logs. It
// owns one EventSource, parses each SSE frame as a LogEvent, and hands
// it to a structured row renderer so levels are colour-coded, the
// component is chipped, error chains stand out, and remaining
// structured fields appear as small key=value tags.
export function LogsView({ streamUrl, title = 'Logs', maxLines = DEFAULT_MAX }: LogsViewProps) {
  const [events, setEvents] = useState<LogEvent[]>([])
  const [streaming, setStreaming] = useState(false)
  const [filter, setFilter] = useState<Level | 'all'>('all')
  const [tailing, setTailing] = useState(true)
  // unseen counts log lines that arrived after the user scrolled up.
  // The tail button uses it to show how far behind the live edge the
  // reader has drifted — and to colour-code the urgency.
  const [unseen, setUnseen] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const endRef = useRef<HTMLDivElement>(null)
  const pendingRef = useRef<LogEvent[]>([])
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const tailingRef = useRef(true)

  // Keep a ref mirror of `tailing` so the scroll-flush logic inside
  // setEvents reads the latest value without re-creating the effect.
  useEffect(() => { tailingRef.current = tailing }, [tailing])

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
      setEvents((prev) => {
        const combined = prev.concat(batch)
        return combined.length > maxLines
          ? combined.slice(combined.length - maxLines)
          : combined
      })
      if (tailingRef.current) {
        // One scroll per flush, not per event — keeps the work flat
        // when bursts arrive faster than the renderer can keep up.
        requestAnimationFrame(() => endRef.current?.scrollIntoView({ behavior: 'auto' }))
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
      es = new EventSource(streamUrl)
      es.onopen = () => {
        retryCount = 0
        setStreaming(true)
      }
      es.onmessage = (event) => {
        pendingRef.current.push(parseEvent(event.data))
        scheduleFlush()
      }
      es.onerror = () => {
        es?.close()
        setStreaming(false)
        if (cancelled) return
        retryCount++
        retryTimer = setTimeout(open, Math.min(3000 * Math.pow(2, retryCount - 1), 60000))
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
  }, [streamUrl, maxLines])

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
    requestAnimationFrame(() => endRef.current?.scrollIntoView({ behavior: 'smooth' }))
  }

  const visible = useMemo(() => {
    if (filter === 'all') return events
    return events.filter((e) => (e.level ?? '').toLowerCase() === filter)
  }, [events, filter])

  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{title}</CardTitle>
        <div className="flex items-center gap-2">
          <span className="text-[11px] tabular-nums text-muted-foreground">
            {visible.length}{filter === 'all' ? '' : ` / ${events.length}`} · max {maxLines}
          </span>
          {!tailing && (
            <TailButton unseen={unseen} onClick={resumeTail} />
          )}
          <LevelFilter value={filter} onChange={setFilter} />
          <StreamStatus streaming={streaming} />
        </div>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col">
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex-1 min-h-0 overflow-x-hidden overflow-y-auto rounded-md border bg-muted/40 p-2 font-mono text-xs"
        >
          {visible.length === 0 ? (
            <div className="p-2 text-muted-foreground">No log lines yet…</div>
          ) : (
            visible.map((e, i) => <LogRow key={i} ev={e} />)
          )}
          <div ref={endRef} />
        </div>
      </CardContent>
    </Card>
  )
}

// TailButton brings the reader back to the live edge. Three visual
// states, all tied to how far the reader has drifted:
//   - unseen === 0  → neutral, just an action hint.
//   - 1..49         → amber, light pulse, count badge.
//   - >=50          → rose, stronger pulse, count badge.
// Reaching the bottom by scroll also resets unseen via onScroll, so
// the colour escalates only when the reader is actively behind.
function TailButton({ unseen, onClick }: { unseen: number; onClick: () => void }) {
  const { tone, dot } = tailStyle(unseen)
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex h-8 cursor-pointer items-center gap-1.5 rounded-md border px-3 text-xs transition-colors',
        tone,
      )}
    >
      {dot && <span className={dot} />}
      <span>tail ↓</span>
      {unseen > 0 && <span className="font-semibold tabular-nums">+{unseen > 999 ? '999+' : unseen}</span>}
    </button>
  )
}

function tailStyle(unseen: number) {
  if (unseen >= 50) {
    return {
      tone: 'border-rose-500/50 bg-rose-500/15 text-rose-700 hover:bg-rose-500/25 dark:text-rose-300',
      dot: 'h-1.5 w-1.5 animate-pulse rounded-full bg-rose-500',
    }
  }
  if (unseen > 0) {
    return {
      tone: 'border-amber-500/50 bg-amber-500/15 text-amber-700 hover:bg-amber-500/25 dark:text-amber-300',
      dot: 'h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500',
    }
  }
  return {
    tone: 'border-border bg-background text-muted-foreground hover:bg-muted',
    dot: '',
  }
}

// StreamStatus draws the SSE-connection badge. The component-level
// `streaming` state flips on EventSource onopen / onerror — green for
// a live tail with a pulsing dot, amber for the exponential-backoff
// reconnect window.
function StreamStatus({ streaming }: { streaming: boolean }) {
  if (streaming) {
    return (
      <Badge
        variant="success"
        className="gap-1.5 border border-emerald-500/40 bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-300"
      >
        <span className="relative inline-flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
        </span>
        streaming
      </Badge>
    )
  }
  return (
    <Badge
      variant="warning"
      className="gap-1.5 border border-amber-500/40 bg-amber-500/15 text-amber-700 hover:bg-amber-500/20 dark:text-amber-300"
    >
      <span className="inline-flex h-2 w-2 rounded-full bg-amber-500" />
      reconnecting
    </Badge>
  )
}

// LogRow renders a single event. Raw stdout frames (no level/message,
// just `line`) render as a thin, full-width text row to keep them
// distinguishable from structured agent/server entries.
function LogRow({ ev }: { ev: LogEvent }) {
  const ts = ev.timestamp ?? ev.time
  if (ev.line && !ev.level && !ev.message) {
    return (
      <div className="flex items-baseline gap-2 px-2 py-0.5 hover:bg-muted/50">
        <span className="shrink-0 text-muted-foreground/70">{formatTime(ts)}</span>
        {ev.component && <ComponentChip name={ev.component} />}
        <span className="min-w-0 break-words text-foreground/90">{ev.line}</span>
      </div>
    )
  }

  const lvl = levelStyle(ev.level)
  return (
    <div className="flex items-baseline gap-2 px-2 py-0.5 hover:bg-muted/50">
      <span className="shrink-0 text-muted-foreground/70">{formatTime(ts)}</span>
      <span className={cn('shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide', lvl.pill)}>
        {lvl.label}
      </span>
      {ev.component && <ComponentChip name={ev.component} />}
      <span className="min-w-0 break-words text-foreground">{ev.message || ev.line}</span>
      {ev.error && (
        <span className="ml-1 inline-block rounded bg-rose-500/15 px-1.5 py-0.5 text-[11px] break-words text-rose-700 dark:text-rose-300">
          {ev.error}
        </span>
      )}
      <FieldChips fields={ev.fields} />
    </div>
  )
}

function ComponentChip({ name }: { name: string }) {
  return (
    <span className={cn('shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium', componentStyle(name))}>
      {name}
    </span>
  )
}

function FieldChips({ fields }: { fields?: Record<string, unknown> }) {
  if (!fields) return null
  const entries = Object.entries(fields).filter(([, v]) => v !== undefined && v !== null && v !== '')
  if (entries.length === 0) return null
  return (
    <span className="ml-1 flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <span key={k} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          <span className="opacity-70">{k}=</span>
          <span className="text-foreground/80">{formatValue(v)}</span>
        </span>
      ))}
    </span>
  )
}

function formatValue(v: unknown): string {
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

function LevelFilter({ value, onChange }: { value: Level | 'all'; onChange: (v: Level | 'all') => void }) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as Level | 'all')}>
      <SelectTrigger className="h-8 w-[120px] text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">all levels</SelectItem>
        {LEVELS.map((l) => (
          <SelectItem key={l} value={l}>{l}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
