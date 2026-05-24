import { useParams } from 'react-router-dom'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/features/logs'
import { apiPaths } from '@/lib/routes'
import { useSubscribe } from '@/lib/use-subscribe'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single node. Owns two SSE streams: one for log
// frames (LogsView), one for the node snapshot (subscribeNode). The
// second feeds the header / breadcrumbs so a direct deep-link doesn't
// keep showing the bare nodeId instead of the full node card data.
export default function NodeLogs() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const subscribeNode = useClusterStore((s) => s.subscribeNode)
  const node = useClusterStore((s) => nodeId
    ? s.nodeCache[nodeId]?.node ?? s.nodes.find((n) => n.id === nodeId) ?? null
    : null)
  useSubscribe(subscribeNode, nodeId)
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
