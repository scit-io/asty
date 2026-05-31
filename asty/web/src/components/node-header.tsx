import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
import { routes } from '@/lib/routes'
import { useT, nodeStatusKey } from '@/lib/i18n'
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
  // Pass node when the page has it; pass nodeId only on pages that
  // can't justify a parallel SSE (e.g. /nodes/{id}/logs already owns
  // its log stream). The lite header drops the status dot and ip/dc
  // strip but keeps the title + breadcrumbs.
  node?: Node | null
  nodeId?: string
  // Crumbs the page-specific tail expects (e.g. 'Allocations'); the
  // Cluster › Nodes › {id} prefix is built here so all three pages
  // share it verbatim.
  tail?: Crumb[]
}

// NodeHeader renders the canonical split-row header for any page
// inside /nodes/{id}: breadcrumbs left, ip/datacenter as the big
// title + status dot + host on a muted subline, right-aligned.
// Breadcrumbs still carry the node id (that's the routable identity);
// the headline trades it for ip/dc so the visual emphasis matches
// what operators actually look at — the address.
export function NodeHeader({ node, nodeId, tail = [] }: NodeHeaderProps) {
  const t = useT()
  const id = node?.id ?? nodeId
  if (!id) return null
  const crumbs: Crumb[] = [
    { label: t('section.cluster'), to: routes.cluster },
    { label: t('tabs.nodes'), to: routes.nodes },
    tail.length === 0
      ? { label: id }
      : { label: id, to: routes.node(id) },
    ...tail,
  ]

  // Title falls back to the bare id when we don't have the full Node
  // yet (lite header on log pages); avoids a blank h1 mid-load.
  const title = node ? `${node.ip} / ${node.datacenter}` : id

  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 sm:gap-8">
      <Breadcrumbs items={crumbs} />
      <div className="space-y-2 w-full sm:w-auto">
        <div className="flex items-center gap-3 sm:gap-4 justify-end">
          <h1 className="text-2xl sm:text-3xl font-bold font-mono">{title}</h1>
          {node && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className={`w-3 h-3 rounded-full ${statusDot(node.status)}`} />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="capitalize">{t(nodeStatusKey(node.status))}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        {node?.host && (
          <div className="text-sm sm:text-base text-muted-foreground text-right font-mono">
            {node.host}
          </div>
        )}
      </div>
    </div>
  )
}
