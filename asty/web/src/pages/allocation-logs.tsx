import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/components/logs-view'
import { useClusterStore } from '@/store/cluster'

// Logs scoped to a single allocation. The allocation cache may be
// empty on direct navigation, so we subscribe to populate it for the
// breadcrumb's service-name label.
export default function AllocationLogs() {
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const { allocationCache, subscribeAllocation } = useClusterStore()
  const allocation = allocId ? allocationCache[allocId]?.allocation : null

  useEffect(() => {
    if (!nodeId || !allocId) return
    return subscribeAllocation(nodeId, allocId)
  }, [nodeId, allocId, subscribeAllocation])

  if (!nodeId || !allocId) return null
  const label = allocation?.service_name || allocId
  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[
        { label: 'Cluster', to: '/' },
        { label: 'Nodes', to: '/nodes' },
        { label: nodeId, to: `/nodes/${nodeId}` },
        { label: 'Allocations', to: `/nodes/${nodeId}/allocations` },
        { label, to: `/nodes/${nodeId}/allocations/${allocId}` },
        { label: 'Logs' },
      ]} />
      <ResourceTabs items={[
        { to: `/nodes/${nodeId}/allocations/${allocId}`, label: 'Overview' },
        { to: `/nodes/${nodeId}/allocations/${allocId}/logs`, label: 'Logs' },
      ]} />
      <LogsView title={`Logs · ${label}`} streamUrl={`/nodes/${nodeId}/allocations/${allocId}/logs`} />
    </div>
  )
}
