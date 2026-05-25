import { useParams } from 'react-router-dom'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/features/logs'
import { useT } from '@/lib/i18n'
import { apiPaths } from '@/lib/routes'
import { useSubscribe } from '@/lib/use-subscribe'
import { useNodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single node. Owns two SSE streams: one for log
// frames (LogsView), one for the node snapshot (subscribeNode). The
// second feeds the header / breadcrumbs so a direct deep-link doesn't
// keep showing the bare nodeId instead of the full node card data.
export default function NodeLogs() {
  const t = useT()
  const { nodeId } = useParams<{ nodeId: string }>()
  const tabs = useNodeTabs(nodeId ?? '')
  const subscribeNode = useClusterStore((s) => s.subscribeNode)
  const node = useClusterStore((s) => nodeId
    ? s.nodeCache[nodeId]?.node ?? s.nodes.find((n) => n.id === nodeId) ?? null
    : null)
  useSubscribe(subscribeNode, nodeId)
  if (!nodeId) return null
  return (
    <PageShell tall>
      <NodeHeader node={node} nodeId={nodeId} tail={[{ label: t('tabs.logs') }]} />
      <ResourceTabs items={tabs} />
      <div className="min-h-0 flex-1">
        <LogsView title={t('logs.title_for', { target: nodeId })} streamUrl={apiPaths.nodeLogs(nodeId)} />
      </div>
    </PageShell>
  )
}
