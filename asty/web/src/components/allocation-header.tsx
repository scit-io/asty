import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
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
  allocation: Allocation
  // Crumbs the page-specific tail expects (e.g. 'Logs'); the
  // Cluster › Nodes › {id} › Allocations › {service} prefix is built
  // here so both alloc pages share it verbatim.
  tail?: Crumb[]
}

// AllocationHeader renders the canonical split-row header for any
// page inside /nodes/{id}/allocations/{allocId}: breadcrumbs left,
// service-name big title + status dot + allocation id right-aligned.
export function AllocationHeader({ allocation, tail = [] }: AllocationHeaderProps) {
  const base: Crumb[] = [
    { label: 'Cluster', to: '/' },
    { label: 'Nodes', to: '/nodes' },
    { label: allocation.node_id, to: `/nodes/${allocation.node_id}` },
    { label: 'Allocations', to: `/nodes/${allocation.node_id}/allocations` },
  ]
  const leaf: Crumb = tail.length === 0
    ? { label: allocation.service_name }
    : { label: allocation.service_name, to: `/nodes/${allocation.node_id}/allocations/${allocation.id}` }
  const crumbs: Crumb[] = [...base, leaf, ...tail]

  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
      <Breadcrumbs items={crumbs} />
      <div className="space-y-2 w-full sm:w-auto">
        <div className="flex items-center gap-3 sm:gap-4 justify-end">
          <h1 className="text-2xl sm:text-3xl font-bold">{allocation.service_name}</h1>
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
        </div>
        <p className="text-muted-foreground font-mono text-xs sm:text-sm text-right">
          {allocation.id}
        </p>
      </div>
    </div>
  )
}
