import { Breadcrumbs } from '@/components/breadcrumbs'
import { LogsView } from '@/components/logs-view'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_SECTION_TABS } from '@/components/header'
import { API_BASE } from '@/api/client'

// Cluster-wide logs page (/logs). Backend emits cluster_event SSE on
// the root URL; the dedicated /logs route also serves SSE under the
// same content-negotiation rule.
export default function ClusterLogs() {
  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <ResourceTabs items={CLUSTER_SECTION_TABS} />
      <Breadcrumbs items={[{ label: 'Cluster', to: '/' }, { label: 'Logs' }]} />
      <LogsView title="Cluster events" streamUrl={`${API_BASE}/logs`} />
    </div>
  )
}
