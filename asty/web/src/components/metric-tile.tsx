import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import type { ReactNode } from 'react'

interface MetricTileProps {
  title: string
  usage: number
  total: number
  unit: string
  icon?: ReactNode
  // chart is optional — when present rendered below the headline.
  chart?: ReactNode
  // formatter overrides default number rendering. Use to swap MB → GB,
  // bytes → MB, etc., when the raw number is unwieldy.
  format?: (n: number) => string
}

const defaultFormat = (n: number) => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return n.toFixed(0)
}

// MetricTile shows a "usage / total" pair with a percentage bar and
// an optional chart slot. The canonical building block for Cluster /
// Node / Allocation overview pages — same component, different data.
export function MetricTile({ title, usage, total, unit, icon, chart, format }: MetricTileProps) {
  const fmt = format ?? defaultFormat
  const pct = total > 0 ? Math.min(100, (usage / total) * 100) : 0

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        <div className="flex items-baseline gap-2">
          <span className="text-2xl font-semibold">{fmt(usage)}</span>
          <span className="text-sm text-muted-foreground">
            / {fmt(total)} {unit}
          </span>
        </div>
        {total > 0 && <Progress value={pct} className="h-1.5" />}
        {chart && <div className="pt-1">{chart}</div>}
      </CardContent>
    </Card>
  )
}
