import { useEffect, useRef, useState } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

interface LogsViewProps {
  // streamUrl is hit with `new EventSource(streamUrl)`. The backend's
  // content-negotiation switches to SSE because EventSource sets
  // Accept: text/event-stream automatically.
  streamUrl: string
  title?: string
  maxLines?: number
}

const DEFAULT_MAX = 500

// LogsView replaces the inline EventSource+autoscroll dance that
// every page that streams logs used to duplicate. Drop in with a URL
// from the new API tree:
//   /logs                                    (cluster events)
//   /nodes/{id}/logs                         (one node)
//   /nodes/{id}/allocations/{id}/logs        (one allocation)
export function LogsView({ streamUrl, title = 'Logs', maxLines = DEFAULT_MAX }: LogsViewProps) {
  const [lines, setLines] = useState<string[]>([])
  const [streaming, setStreaming] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    let retryCount = 0
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let es: EventSource | null = null

    const open = () => {
      if (cancelled) return
      es = new EventSource(streamUrl)
      es.onopen = () => {
        retryCount = 0
        setStreaming(true)
      }
      es.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          const line = typeof data === 'string' ? data : (data.line ?? JSON.stringify(data))
          setLines((prev) => {
            const next = prev.length >= maxLines
              ? prev.slice(prev.length - maxLines + 1).concat(line)
              : prev.concat(line)
            return next
          })
          requestAnimationFrame(() => endRef.current?.scrollIntoView({ behavior: 'smooth' }))
        } catch {
          // skip malformed line
        }
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
      es?.close()
    }
  }, [streamUrl, maxLines])

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{title}</CardTitle>
        <Badge variant={streaming ? 'default' : 'secondary'}>
          {streaming ? 'streaming' : 'reconnecting'}
        </Badge>
      </CardHeader>
      <CardContent>
        <div className="h-[60vh] overflow-y-auto rounded-md border bg-muted/40 p-3 font-mono text-xs">
          {lines.length === 0 ? (
            <div className="text-muted-foreground">No log lines yet…</div>
          ) : (
            lines.map((line, i) => (
              <div key={i} className="whitespace-pre-wrap break-words">{line}</div>
            ))
          )}
          <div ref={endRef} />
        </div>
      </CardContent>
    </Card>
  )
}
