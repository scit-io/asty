import { useParams } from 'react-router-dom'
import { AllocationHeader } from '@/components/allocation-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/features/logs'
import { useT } from '@/lib/i18n'
import { apiPaths } from '@/lib/routes'
import { useSubscribe } from '@/lib/use-subscribe'
import { useAllocationTabs } from '@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single allocation. Owns two SSE streams: one for
// log frames (LogsView), one for the allocation snapshot
// (subscribeAllocation). The second feeds the header / breadcrumbs /
// log-card title so a direct deep-link doesn't display the raw allocId
// instead of the service name.
export default function AllocationLogs() {
  const t = useT()
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const tabs = useAllocationTabs(nodeId ?? '', allocId ?? '')
  const subscribeAllocation = useClusterStore((s) => s.subscribeAllocation)
  const allocation = useClusterStore((s) => allocId ? s.allocationCache[allocId]?.allocation ?? null : null)
  useSubscribe(subscribeAllocation, nodeId, allocId)
  if (!nodeId || !allocId) return null
  // Drop the allocId fallback: showing "Logs · service-a-dev-node-6-..."
  // for a beat before it swaps to "Logs · service-a" is uglier than just
  // "Logs" until the name arrives.
  const title = allocation?.service_name
    ? t('logs.title_for', { target: allocation.service_name })
    : t('logs.title')
  return (
    <PageShell tall>
      <AllocationHeader allocation={allocation} nodeId={nodeId} allocId={allocId} tail={[{ label: t('tabs.logs'), key: 'logs' }]} />
      <ResourceTabs items={tabs} />
      <div className="min-h-0 flex-1">
        <LogsView title={title} streamUrl={apiPaths.allocationLogs(nodeId, allocId)} />
      </div>
    </PageShell>
  )
}
