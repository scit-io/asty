import { useParams } from 'react-router-dom'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { LogsView } from '@/components/logs-view'
import { API_BASE } from '@/api/client'

// Logs scoped to a single node. The agent's NATSWriter publishes
// agent logs under asty.v1.agent.{nodeID}.logs.*; the orchestrator
// exposes them on /nodes/{id}/logs via content-negotiated SSE.
export default function NodeLogs() {
  const { nodeId } = useParams<{ nodeId: string }>()
  if (!nodeId) return null
  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[
        { label: 'Cluster', to: '/' },
        { label: 'Nodes', to: '/nodes' },
        { label: nodeId, to: `/nodes/${nodeId}` },
        { label: 'Logs' },
      ]} />
      <ResourceTabs items={[
        { to: `/nodes/${nodeId}`, label: 'Overview' },
        { to: `/nodes/${nodeId}/allocations`, label: 'Allocations' },
        { to: `/nodes/${nodeId}/logs`, label: 'Logs' },
      ]} />
      <LogsView title={`Logs · ${nodeId}`} streamUrl={`${API_BASE}/nodes/${nodeId}/logs`} />
    </div>
  )
}
