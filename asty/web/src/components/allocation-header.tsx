import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
import { Skeleton } from '@/components/ui/skeleton'
import { routes } from '@/lib/routes'
import { useT, allocStatusKey } from '@/lib/i18n'
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

// nameSlot returns the service name when known, or a Skeleton sized
// via className. Same pattern in two slots (breadcrumb leaf and H1) —
// one helper keeps them in lockstep.
function nameSlot(name: string | undefined, className: string) {
  return name ?? <Skeleton className={`inline-block align-middle ${className}`} />
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
  const t = useT()
  const nid = allocation?.node_id ?? nodeId
  const aid = allocation?.id ?? allocId
  if (!nid || !aid) return null
  // service_name is what the user reads — never fall back to aid
  // here. On a cold deep-link both slots render a Skeleton; the SSE
  // landing replaces it without flashing the raw id first.
  const name = allocation?.service_name
  const crumbs: Crumb[] = [
    { label: t('section.cluster'), to: routes.cluster, key: 'cluster' },
    { label: t('tabs.nodes'), to: routes.nodes, key: 'nodes' },
    { label: nid, to: routes.node(nid), key: 'node' },
    { label: t('tabs.allocations'), to: routes.nodeAllocations(nid), key: 'allocs' },
    {
      label: nameSlot(name, 'h-5 w-24'),
      to: tail.length === 0 ? undefined : routes.allocation(nid, aid),
      key: 'leaf',
    },
    ...tail,
  ]

  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
      <Breadcrumbs items={crumbs} />
      <div className="space-y-2 w-full sm:w-auto">
        <div className="flex items-center gap-3 sm:gap-4 justify-end">
          <h1 className="text-2xl sm:text-3xl font-bold">{nameSlot(name, 'h-8 w-40')}</h1>
          {allocation && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className={`w-3 h-3 rounded-full ${statusDot(allocation.status)}`} />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{t(allocStatusKey(allocation.status))}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        <p className="text-muted-foreground text-xs sm:text-sm text-right">
          {aid}
        </p>
      </div>
    </div>
  )
}
