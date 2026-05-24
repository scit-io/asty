import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { TimeStack } from '@/components/time-stack'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { deployStatusVariant } from '@/lib/variants'
import { useClusterStore } from '@/store/cluster'
import type { DeploymentRecord } from '@/types'

// historyColumns lifted out of the component so SSE refreshes don't
// break DataTable's sort/page state by giving it a new array
// reference for the columns prop on every render.
const historyColumns: Column<DeploymentRecord>[] = [
  {
    key: 'version', label: 'Version',
    sort: (a, b) => a.version.localeCompare(b.version),
    render: (r) => <span className="font-mono text-xs">{r.version}</span>,
  },
  {
    key: 'strategy', label: 'Strategy',
    sort: (a, b) => a.strategy.localeCompare(b.strategy),
    render: (r) => <span className="text-xs">{r.strategy}</span>,
  },
  {
    key: 'status', label: 'Status',
    sort: (a, b) => a.status.localeCompare(b.status),
    render: (r) => <Badge variant={deployStatusVariant(r.status)}>{r.status}</Badge>,
  },
  {
    key: 'progress', label: 'Progress',
    sort: (a, b) => a.progress - b.progress,
    render: (r) => <span>{r.progress}%</span>,
  },
  {
    key: 'started', label: 'Started',
    sort: (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
    render: (r) => <TimeStack date={new Date(r.started_at)} />,
  },
  {
    key: 'completed', label: 'Completed',
    sort: (a, b) => new Date(a.completed_at ?? 0).getTime() - new Date(b.completed_at ?? 0).getTime(),
    render: (r) => {
      if (!r.completed_at) return <span className="text-sm text-muted-foreground">—</span>
      return <TimeStack date={new Date(r.completed_at)} />
    },
  },
]

// Deploy history tab — reads from the same per-service cache the
// Overview's "Deploy a new version" card uses (single EventSource +
// REST load in cluster store).
export default function ServiceDeployHistory() {
  const { name } = useParams<{ name: string }>()
  const { serviceCache, subscribeService } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const history = cached?.deployHistory ?? []

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: 'Deploy history' }]} />
      <ResourceTabs items={serviceTabs(name)} />

      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={history}
            columns={historyColumns}
            search={{
              placeholder: 'Search by version, status or strategy…',
              match: (r, q) => {
                const needle = q.toLowerCase()
                return r.version.toLowerCase().includes(needle)
                  || r.status.toLowerCase().includes(needle)
                  || r.strategy.toLowerCase().includes(needle)
              },
            }}
            rowKey={(r) => r.id}
            emptyMessage="No deployments recorded."
          />
        </CardContent>
      </Card>
    </PageShell>
  )
}
