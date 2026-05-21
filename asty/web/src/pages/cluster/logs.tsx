import { LogsView } from '@/components/logs-view'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_SECTION_TABS } from '@/components/header'
import { API_PREFIX } from '@/api/client'

// Cluster-wide logs page (/logs). Backend emits cluster_event SSE on
// the root URL; the dedicated /logs route also serves SSE under the
// same content-negotiation rule.
export default function ClusterLogs() {
  return (
    <div className="container mx-auto flex h-full flex-col gap-4 overflow-hidden p-4 sm:p-6">
      <ResourceTabs items={CLUSTER_SECTION_TABS} />
      <div className="min-h-0 flex-1">
        <LogsView title="Cluster events" streamUrl={`${API_PREFIX}/logs`} />
      </div>
    </div>
  )
}
