import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
import type { Node } from '@/types'

// statusDot picks the colour pellet by node lifecycle. Matches the
// table badges' palette so the page-header dot agrees with the
// table row when you drill in from /nodes.
const statusDot = (s?: Node['status']): string => {
  switch (s) {
    case 'ready': return 'bg-green-500'
    case 'draining': return 'bg-yellow-500 animate-pulse'
    case 'drained':
    case 'paused': return 'bg-yellow-500'
    case 'down': return 'bg-red-500'
    default: return 'bg-gray-400'
  }
}

interface NodeHeaderProps {
  node: Node
  // Crumbs the page-specific tail expects (e.g. 'Allocations'); the
  // Cluster › Nodes › {id} prefix is built here so all three pages
  // share it verbatim.
  tail?: Crumb[]
}

// NodeHeader renders the canonical split-row header for any page
// inside /nodes/{id}: breadcrumbs left, node-id big title + status
// dot + ip / datacenter line right-aligned.
export function NodeHeader({ node, tail = [] }: NodeHeaderProps) {
  const crumbs: Crumb[] = [
    { label: 'Cluster', to: '/' },
    { label: 'Nodes', to: '/nodes' },
    tail.length === 0
      ? { label: node.id }
      : { label: node.id, to: `/nodes/${node.id}` },
    ...tail,
  ]

  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
      <Breadcrumbs items={crumbs} />
      <div className="space-y-2 w-full sm:w-auto">
        <div className="flex items-center gap-3 sm:gap-4 justify-end">
          <h1 className="text-2xl sm:text-3xl font-bold font-mono">{node.id}</h1>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div className={`w-3 h-3 rounded-full ${statusDot(node.status)}`} />
              </TooltipTrigger>
              <TooltipContent>
                <p className="capitalize">{node.status}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div className="text-sm sm:text-base text-muted-foreground text-right">
          <span className="font-mono">{node.ip}</span> / {node.datacenter}
        </div>
      </div>
    </div>
  )
}
