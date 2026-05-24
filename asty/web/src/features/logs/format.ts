import type { LogEvent } from '@/types'

// LEVEL_STYLE — Tailwind class fragments for each zerolog level. The
// pill uses the foreground colour for the text and a soft tint of the
// same hue for the background, so the level reads instantly without
// drowning the row in colour.
export const LEVEL_STYLE: Record<string, { label: string; pill: string }> = {
  trace: { label: 'TRACE', pill: 'bg-muted text-muted-foreground' },
  debug: { label: 'DEBUG', pill: 'bg-sky-500/15 text-sky-600 dark:text-sky-300' },
  info:  { label: 'INFO',  pill: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300' },
  warn:  { label: 'WARN',  pill: 'bg-amber-500/20 text-amber-700 dark:text-amber-300' },
  error: { label: 'ERROR', pill: 'bg-rose-500/20 text-rose-600 dark:text-rose-300' },
  fatal: { label: 'FATAL', pill: 'bg-rose-600 text-white' },
}

export function levelStyle(level?: string) {
  if (!level) return { label: 'LOG', pill: 'bg-muted text-muted-foreground' }
  return LEVEL_STYLE[level.toLowerCase()] ?? { label: level.toUpperCase(), pill: 'bg-muted text-muted-foreground' }
}

// COMPONENT_PALETTE — a small fixed palette the chip colour cycles
// through, keyed by a hash of the component name. Same name → same
// colour across reloads and pages, which makes scanning logs for one
// subsystem fast.
const COMPONENT_PALETTE = [
  'bg-indigo-500/15 text-indigo-700 dark:text-indigo-300',
  'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300',
  'bg-violet-500/15 text-violet-700 dark:text-violet-300',
  'bg-pink-500/15 text-pink-700 dark:text-pink-300',
  'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300',
  'bg-orange-500/15 text-orange-700 dark:text-orange-300',
  'bg-teal-500/15 text-teal-700 dark:text-teal-300',
  'bg-fuchsia-500/15 text-fuchsia-700 dark:text-fuchsia-300',
]

export function componentStyle(component?: string) {
  if (!component) return ''
  let hash = 0
  for (let i = 0; i < component.length; i++) {
    hash = (hash * 31 + component.charCodeAt(i)) | 0
  }
  return COMPONENT_PALETTE[Math.abs(hash) % COMPONENT_PALETTE.length]
}

// formatTime renders a Unix-second timestamp as HH:MM:SS in the
// browser locale. Falls back to "--:--:--" so the column width never
// collapses when an entry comes in without a timestamp.
export function formatTime(ts?: number): string {
  if (!ts) return '--:--:--'
  const d = new Date(ts * 1000)
  return d.toLocaleTimeString([], { hour12: false })
}

// parseEvent turns one SSE data payload into a LogEvent. Three shapes
// are accepted: the canonical {level,component,message,...} object,
// the legacy {line,timestamp} stdout frame, and a bare string (some
// older agents publish raw text). Anything unparseable becomes
// {line: <raw>}.
export function parseEvent(data: string): LogEvent {
  try {
    const obj = JSON.parse(data)
    if (typeof obj === 'string') return { line: obj }
    if (obj && typeof obj === 'object') return obj as LogEvent
    return { line: data }
  } catch {
    return { line: data }
  }
}
