import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { TimeStack } from '@/components/time-stack'
import { useT, deployStatusKey } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useServiceTabs } from '@/pages/services/[name]/tabs'
import { deployStatusVariant } from '@/lib/variants'
import { useClusterStore } from '@/store/cluster'
import type { DeploymentRecord } from '@/types'

// Deploy history tab — reads from the same per-service cache the
// Overview's "Deploy a new version" card uses (single EventSource +
// REST load in cluster store).
export default function ServiceDeployHistory() {
  const t = useT()
  const { name } = useParams<{ name: string }>()
  const tabs = useServiceTabs(name ?? '')
  const subscribeService = useClusterStore((s) => s.subscribeService)
  const cached = useClusterStore((s) => name ? s.serviceCache[name] : undefined)
  const history = cached?.deployHistory ?? []

  useSubscribe(subscribeService, name)

  const columns = useMemo<Column<DeploymentRecord>[]>(() => [
    {
      key: 'version', label: t('deploy.col.version'),
      sort: (a, b) => a.version.localeCompare(b.version),
      render: (r) => <span className="font-mono text-xs">{r.version}</span>,
    },
    {
      key: 'strategy', label: t('deploy.col.strategy'),
      sort: (a, b) => a.strategy.localeCompare(b.strategy),
      render: (r) => <span className="text-xs">{r.strategy}</span>,
    },
    {
      key: 'status', label: t('deploy.col.status'),
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (r) => <Badge variant={deployStatusVariant(r.status)}>{t(deployStatusKey(r.status))}</Badge>,
    },
    {
      key: 'progress', label: t('deploy.col.progress'),
      sort: (a, b) => a.progress - b.progress,
      render: (r) => <span>{r.progress}%</span>,
    },
    {
      key: 'started', label: t('deploy.col.started'),
      sort: (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
      render: (r) => <TimeStack date={new Date(r.started_at)} />,
    },
    {
      key: 'completed', label: t('deploy.col.completed'),
      sort: (a, b) => new Date(a.completed_at ?? 0).getTime() - new Date(b.completed_at ?? 0).getTime(),
      render: (r) => {
        if (!r.completed_at) return <span className="text-sm text-muted-foreground">—</span>
        return <TimeStack date={new Date(r.completed_at)} />
      },
    },
  ], [t])

  const search = useMemo(() => ({
    placeholder: t('deploy.search_placeholder'),
    match: (r: DeploymentRecord, q: string) => {
      const needle = q.toLowerCase()
      return r.version.toLowerCase().includes(needle)
        || r.status.toLowerCase().includes(needle)
        || r.strategy.toLowerCase().includes(needle)
    },
  }), [t])

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: t('tabs.deploy_history') }]} />
      <ResourceTabs items={tabs} />

      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={history}
            columns={columns}
            search={search}
            rowKey={(r) => r.id}
            emptyMessage={t('deploy.empty')}
          />
        </CardContent>
      </Card>
    </PageShell>
  )
}
