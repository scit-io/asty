import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
import { routes } from '@/lib/routes'
import type { Allocation } from '@/types'

// statusDot mirrors the allocation status palette used by the table
// badges so the page-header dot agrees with the row colour.
const statusDot = (s?: Allocation['status']): string => {
  switch (s) {
    case 'running': return 'bg-green-500'
    case 'failed': return 'bg-red-500'
    case 'pending':
    case 'starting': return 'bg-yellow-500 animate-pulse'
    case 'stopped':
    default: return 'bg-gray-400'
  }
}

interface AllocationHeaderProps {
  // Pass the full allocation when the page already has it; pass
  // nodeId+allocId only on pages that own a different live stream
  // (logs). The lite header drops the status dot but keeps title +
  // breadcrumbs from the URL params.
  allocation?: Allocation | null
  nodeId?: string
  allocId?: string
  // Crumbs the page-specific tail expects (e.g. 'Logs').
  tail?: Crumb[]
}

// AllocationHeader renders the canonical split-row header for any
// page inside /nodes/{id}/allocations/{allocId}: breadcrumbs left,
// service-name big title + status dot + allocation id right-aligned.
export function AllocationHeader({ allocation, nodeId, allocId, tail = [] }: AllocationHeaderProps) {
  const nid = allocation?.node_id ?? nodeId
  const aid = allocation?.id ?? allocId
  if (!nid || !aid) return null
  const title = allocation?.service_name ?? aid
  const base: Crumb[] = [
    { label: 'Cluster', to: routes.cluster },
    { label: 'Nodes', to: routes.nodes },
    { label: nid, to: routes.node(nid) },
    { label: 'Allocations', to: routes.nodeAllocations(nid) },
  ]
  const leaf: Crumb = tail.length === 0
    ? { label: title }
    : { label: title, to: routes.allocation(nid, aid) }
  const crumbs: Crumb[] = [...base, leaf, ...tail]

  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
      <Breadcrumbs items={crumbs} />
      <div className="space-y-2 w-full sm:w-auto">
        <div className="flex items-center gap-3 sm:gap-4 justify-end">
          <h1 className="text-2xl sm:text-3xl font-bold">{title}</h1>
          {allocation && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className={`w-3 h-3 rounded-full ${statusDot(allocation.status)}`} />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{allocation.status}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        <p className="text-muted-foreground font-mono text-xs sm:text-sm text-right">
          {aid}
        </p>
      </div>
    </div>
  )
}
