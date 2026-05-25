import { LogsView } from '@/features/logs'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { useClusterTabs } from '@/pages/cluster/tabs'
import { useT } from '@/lib/i18n'
import { apiPaths } from '@/lib/routes'

// Cluster-wide logs page (/logs). Backend emits cluster_event SSE on
// the root URL; the dedicated /logs route also serves SSE under the
// same content-negotiation rule.
export default function ClusterLogs() {
  const t = useT()
  const tabs = useClusterTabs()
  return (
    <PageShell tall>
      <h2 className="text-lg font-semibold">{t('section.cluster')}</h2>
      <ResourceTabs items={tabs} />
      <div className="min-h-0 flex-1">
        <LogsView title={t('logs.cluster_events')} streamUrl={apiPaths.clusterLogs} />
      </div>
    </PageShell>
  )
}
