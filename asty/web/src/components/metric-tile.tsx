import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { ReactNode } from 'react'

interface MetricTileProps {
  title: string
  usage: number
  total: number
  unit: string
  icon?: ReactNode
  chart?: ReactNode
  format?: (n: number) => string
}

const defaultFormat = (n: number) => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return n.toFixed(0)
}

// gradientBar mounts a full-width gradient under a right-aligned
// mask. Result: the visible (filled) portion shows the leftmost
// `pct%` of an emerald→amber→red gradient, so colour scales with
// actual utilisation rather than snapping at thresholds. Cleaner
// than overriding shadcn's Progress indicator because the indicator
// is translated by transform — gradient would slide with it and
// invert the meaning.
function GradientBar({ pct }: { pct: number }) {
  return (
    <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-secondary">
      <div className="absolute inset-0 bg-gradient-to-r from-emerald-500 via-amber-500 to-red-500" />
      <div className="absolute inset-y-0 right-0 bg-secondary" style={{ width: `${100 - pct}%` }} />
    </div>
  )
}

// MetricTile shows a utilisation card in the canonical shadcn dashboard
// rhythm: title (left) + icon (right), big percentage, small used/total
// hint, gradient bar at the bottom that colours with load.
export function MetricTile({ title, usage, total, unit, icon, chart, format }: MetricTileProps) {
  const fmt = format ?? defaultFormat
  const pct = total > 0 ? Math.min(100, (usage / total) * 100) : 0

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        {icon && <span className="text-muted-foreground">{icon}</span>}
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{pct.toFixed(0)}%</div>
        <p className="text-xs text-muted-foreground">
          {fmt(usage)} / {fmt(total)} {unit}
        </p>
        {total > 0 && <div className="mt-3"><GradientBar pct={pct} /></div>}
        {chart && <div className="pt-3">{chart}</div>}
      </CardContent>
    </Card>
  )
}
