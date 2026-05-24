import { memo } from 'react'
import { cn } from '@/lib/utils'
import { componentStyle, formatTime, levelStyle } from './format'
import type { LogEvent } from '@/types'

// LogRow renders a single event. Raw stdout frames (no level/message,
// just `line`) render as a thin, full-width text row to keep them
// distinguishable from structured agent/server entries.
// Memo'd on the `ev` reference: LogsView assigns a stable id per event
// and never mutates the parsed object, so default shallow compare
// skips the render for every already-mounted row. Only the freshly-
// appended rows from the current flush actually execute their body.
export const LogRow = memo(function LogRow({ ev }: { ev: LogEvent }) {
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
})

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
