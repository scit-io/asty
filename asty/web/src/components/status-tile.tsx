import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { ReactNode } from 'react'

interface StatusTileProps {
  title: string
  value: ReactNode
  // hint is the smaller text under the headline (e.g. "/ 5 total",
  // an IP address, or a free-form context line).
  hint?: ReactNode
  icon?: ReactNode
  className?: string
}

// StatusTile is the compact sibling of MetricTile — for things that
// don't have a usage/total relationship: leader identity, healthy %,
// nodes active count, etc. Same visual rhythm so the dashboard reads
// as a grid: title left, icon right, bold value, muted hint.
export function StatusTile({ title, value, hint, icon, className }: StatusTileProps) {
  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        {icon && <span className="text-muted-foreground">{icon}</span>}
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
        {hint && <p className="text-xs text-muted-foreground mt-1">{hint}</p>}
      </CardContent>
    </Card>
  )
}
