import { useParams } from 'react-router-dom'
import { NodeHeader } from '@/components/node-header'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/components/logs-view'
import { API_PREFIX } from '@/api/client'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single node. The page owns exactly one
// EventSource (LogsView). NodeHeader reads whatever is already
// cached from a prior visit and falls back to a lite header on a
// direct deep-link — we don't open a second SSE for the title.
export default function NodeLogs() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const node = useClusterStore((s) => nodeId
    ? s.nodeCache[nodeId]?.node ?? s.nodes.find((n) => n.id === nodeId) ?? null
    : null)
  if (!nodeId) return null
  return (
    <div className="container mx-auto flex h-full flex-col gap-4 overflow-hidden p-4 sm:p-6">
      <NodeHeader node={node} nodeId={nodeId} tail={[{ label: 'Logs' }]} />
      <ResourceTabs items={[
        { to: `/nodes/${nodeId}`, label: 'Overview' },
        { to: `/nodes/${nodeId}/allocations`, label: 'Allocations' },
        { to: `/nodes/${nodeId}/logs`, label: 'Logs' },
      ]} />
      <div className="min-h-0 flex-1">
        <LogsView title={`Logs · ${nodeId}`} streamUrl={`${API_PREFIX}/nodes/${nodeId}/logs`} />
      </div>
    </div>
  )
}
