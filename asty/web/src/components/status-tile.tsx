import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { ReactNode } from 'react'

interface StatusTileProps {
  title: string
  value: ReactNode
  // hint is the smaller text under the headline (e.g. "/ 5 total",
  // an IP address, or a free-form context line).
  hint?: ReactNode
  icon?: ReactNode
}

// StatusTile is the compact sibling of MetricTile — for things that
// don't have a usage/total relationship: leader identity, healthy %,
// nodes active count, etc. Same visual rhythm so the dashboard reads
// as a grid.
export function StatusTile({ title, value, hint, icon }: StatusTileProps) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-semibold">{value}</div>
        {hint && <div className="text-sm text-muted-foreground mt-1">{hint}</div>}
      </CardContent>
    </Card>
  )
}
