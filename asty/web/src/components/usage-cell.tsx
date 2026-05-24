import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'

interface UsageCellProps {
  icon: LucideIcon
  primary: ReactNode
  secondary?: ReactNode
}

// UsageCell is the resource-usage table cell shared by the Nodes,
// Allocations (per-node + per-service), and Services list views.
// Icon on the left, two-line value stack on the right — large primary
// (percent or raw value), small muted secondary (used/total). Callers
// own formatting (formatMB / formatMHz / formatPercent); UsageCell
// owns layout so the four columns stay byte-identical.
export function UsageCell({ icon: Icon, primary, secondary }: UsageCellProps) {
  return (
    <div className="flex items-center gap-2">
      <Icon className="h-4 w-4 text-muted-foreground" />
      <div className="space-y-1">
        <div className="text-sm font-medium">{primary}</div>
        {secondary !== undefined && (
          <div className="text-xs text-muted-foreground">{secondary}</div>
        )}
      </div>
    </div>
  )
}
