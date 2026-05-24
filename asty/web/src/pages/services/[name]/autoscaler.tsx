import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { useClusterStore } from '@/store/cluster'
import type { ScalingEvent } from '@/types'

// Scaling events tab — reads from the same per-service cache the
// Overview's autoscaler card uses (single poller in cluster store).
const scalingEventColumns: Column<ScalingEvent>[] = [
  {
    key: 'action', label: 'Action',
    sort: (a, b) => a.action.localeCompare(b.action),
    render: (e) => (
      <div className="flex items-center gap-1">
        <Badge variant={e.action === 'scale_up' ? 'default' : 'secondary'}>
          {e.action === 'scale_up' ? 'Scale Up' : 'Scale Down'}
        </Badge>
        {e.reason.startsWith('manual:') && (
          <Badge variant="outline" className="text-[10px]">manual</Badge>
        )}
      </div>
    ),
  },
  {
    key: 'reason', label: 'Reason',
    sort: (a, b) => a.reason.localeCompare(b.reason),
    render: (e) => <span className="text-sm">{e.reason}</span>,
  },
  {
    key: 'copies', label: 'Copies',
    sort: (a, b) => (a.to_count - a.from_count) - (b.to_count - b.from_count),
    render: (e) => <span>{e.from_count} → {e.to_count}</span>,
  },
  {
    key: 'node', label: 'Node',
    sort: (a, b) => (a.node_id ?? '').localeCompare(b.node_id ?? ''),
    render: (e) => {
      if (e.node_id) return <span className="font-mono text-xs">{e.node_id}</span>
      // Manual scale is service-scoped (multiple copies / scheduler-
      // picked placement) — surface that explicitly so an empty
      // node_id doesn't read as missing data.
      if (e.reason.startsWith('manual:')) return <span className="text-xs text-muted-foreground italic">scheduler</span>
      return <span className="text-muted-foreground text-xs">—</span>
    },
  },
  {
    key: 'time', label: 'Time',
    sort: (a, b) => a.timestamp - b.timestamp,
    render: (e) => {
      const d = new Date(e.timestamp * 1000)
      return (
        <div className="space-y-1">
          <div className="text-sm font-medium">{d.toLocaleTimeString()}</div>
          <div className="text-xs text-muted-foreground">{d.toLocaleDateString()}</div>
        </div>
      )
    },
  },
]

export default function ServiceScalingEvents() {
  const { name } = useParams<{ name: string }>()
  const { serviceCache, subscribeService } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const events = cached?.scalingEvents ?? []

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: 'Scaling events' }]} />
      <ResourceTabs items={serviceTabs(name)} />

      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={events}
            columns={scalingEventColumns}
            search={{
              placeholder: 'Search by reason or node…',
              match: (e, q) => {
                const needle = q.toLowerCase()
                return e.reason.toLowerCase().includes(needle)
                  || (e.node_id ?? '').toLowerCase().includes(needle)
                  || e.action.toLowerCase().includes(needle)
              },
            }}
            rowKey={(e) => `${e.timestamp}-${e.action}-${e.node_id ?? ''}-${e.reason}`}
            emptyMessage="No scaling events yet."
          />
        </CardContent>
      </Card>
    </PageShell>
  )
}
