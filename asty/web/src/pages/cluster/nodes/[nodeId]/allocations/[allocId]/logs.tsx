import { useParams } from 'react-router-dom'
import { AllocationHeader } from '@/components/allocation-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/features/logs'
import { apiPaths } from '@/lib/routes'
import { allocationTabs } from '@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single allocation. The page owns exactly one
// EventSource (LogsView). The allocation header pulls whatever is
// already cached from a prior visit; on a direct deep-link it falls
// back to a lite header built from URL params — we don't open a
// second SSE just to label the title.
export default function AllocationLogs() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const allocation = useClusterStore((s) => allocId ? s.allocationCache[allocId]?.allocation ?? null : null)
  if (!nodeId || !allocId) return null
  const label = allocation?.service_name || allocId
  return (
    <PageShell tall>
      <AllocationHeader allocation={allocation} nodeId={nodeId} allocId={allocId} tail={[{ label: 'Logs' }]} />
      <ResourceTabs items={allocationTabs(nodeId, allocId)} />
      <div className="min-h-0 flex-1">
        <LogsView title={`Logs · ${label}`} streamUrl={apiPaths.allocationLogs(nodeId, allocId)} />
      </div>
    </PageShell>
  )
}
