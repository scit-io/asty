import { Badge } from '@/components/ui/badge'
import { useT } from '@/lib/i18n'

// StreamState mirrors the per-resource SSE lifecycle in
// store/cluster.ts so the badge truthfully reflects whether new
// lines can still arrive:
//   streaming    — connected, frames flowing.
//   reconnecting — transient drop, exponential backoff scheduled.
//   dead         — STREAM_MAX_RETRIES exhausted; the visible buffer
//                  is the last known state. The badge becomes a
//                  click-to-reconnect affordance.
export type StreamState = 'streaming' | 'reconnecting' | 'dead'

// StreamStatus draws the SSE-connection badge. Three states: green
// (pulsing dot) for streaming, amber for the transient backoff
// window, rose for the terminal dead state with a button to trigger
// a fresh connection attempt.
export function StreamStatus({ state, onReconnect }: { state: StreamState; onReconnect: () => void }) {
  const t = useT()
  if (state === 'streaming') {
    return (
      <Badge
        variant="success"
        className="gap-1.5 border border-emerald-500/40 bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-300"
      >
        <span className="relative inline-flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
        </span>
        {t('logs.stream.streaming')}
      </Badge>
    )
  }
  if (state === 'reconnecting') {
    return (
      <Badge
        variant="warning"
        className="gap-1.5 border border-amber-500/40 bg-amber-500/15 text-amber-700 hover:bg-amber-500/20 dark:text-amber-300"
      >
        <span className="inline-flex h-2 w-2 rounded-full bg-amber-500" />
        {t('logs.stream.reconnecting')}
      </Badge>
    )
  }
  return (
    <button
      type="button"
      onClick={onReconnect}
      title={t('logs.stream.reconnect_title')}
      className="inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-full border border-rose-500/40 bg-rose-500/15 px-2.5 py-0.5 text-xs font-semibold text-rose-700 transition-colors hover:bg-rose-500/25 dark:text-rose-300"
    >
      <span className="inline-flex h-2 w-2 rounded-full bg-rose-500" />
      {t('logs.stream.disconnected')}
    </button>
  )
}
