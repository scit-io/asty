import { useParams } from 'react-router-dom'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/features/logs'
import { apiPaths } from '@/lib/routes'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
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
    <PageShell tall>
      <NodeHeader node={node} nodeId={nodeId} tail={[{ label: 'Logs' }]} />
      <ResourceTabs items={nodeTabs(nodeId)} />
      <div className="min-h-0 flex-1">
        <LogsView title={`Logs · ${nodeId}`} streamUrl={apiPaths.nodeLogs(nodeId)} />
      </div>
    </PageShell>
  )
}
