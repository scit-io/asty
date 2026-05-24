import { LogsView } from '@/components/logs-view'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_TABS } from '@/pages/cluster/tabs'
import { apiPaths } from '@/lib/routes'

// Cluster-wide logs page (/logs). Backend emits cluster_event SSE on
// the root URL; the dedicated /logs route also serves SSE under the
// same content-negotiation rule.
export default function ClusterLogs() {
  return (
    <PageShell tall>
      <h2 className="text-lg font-semibold">Cluster</h2>
      <ResourceTabs items={CLUSTER_TABS} />
      <div className="min-h-0 flex-1">
        <LogsView title="Cluster events" streamUrl={apiPaths.clusterLogs} />
      </div>
    </PageShell>
  )
}
