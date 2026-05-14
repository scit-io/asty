import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { DataTable, type Column } from '@/components/data-table'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { formatMB, formatMHz } from '@/lib/format'
import { useClusterStore } from '@/store/cluster'
import type { ServiceDefinition } from '@/types'

// Services list (/services). Backs onto useClusterStore().services
// which already carries the runtime ServiceWithUsage shape from the
// snapshot SSE.
export default function Services() {
  const navigate = useNavigate()
  const subscribeServices = useClusterStore((s) => s.subscribeServices)
  const services = useClusterStore((s) => s.services)

  useEffect(() => subscribeServices(), [subscribeServices])

  const columns: Column<ServiceDefinition>[] = [
    {
      key: 'name', label: 'Service',
      sort: (a, b) => a.Name.localeCompare(b.Name),
      render: (s) => <span className="font-mono text-sm">{s.Name}</span>,
    },
    {
      key: 'type', label: 'Type',
      sort: (a, b) => a.Type.localeCompare(b.Type),
      render: (s) => <Badge variant={s.Type === 'system' ? 'secondary' : 'default'}>{s.Type}</Badge>,
    },
    {
      key: 'copies', label: 'Copies',
      sort: (a, b) => (a.current_copies ?? 0) - (b.current_copies ?? 0),
      render: (s) => s.Type === 'service'
        ? <span><b>{s.current_copies ?? 0}</b> / min {s.min_copies ?? 0}</span>
        : <span className="text-muted-foreground">{s.current_copies ?? 0}</span>,
    },
    {
      key: 'cpu', label: 'CPU avg',
      sort: (a, b) => (a.avg_cpu_percent ?? 0) - (b.avg_cpu_percent ?? 0),
      render: (s) => `${Math.round(s.avg_cpu_percent ?? 0)}% (${formatMHz(s.Resources.CPU)} limit)`,
    },
    {
      key: 'mem', label: 'Memory avg',
      sort: (a, b) => (a.avg_memory_mb ?? 0) - (b.avg_memory_mb ?? 0),
      render: (s) => `${formatMB(s.avg_memory_mb ?? 0)} / ${formatMB(s.Resources.Memory)}`,
    },
    {
      key: 'cooldown', label: 'Cooldown',
      render: (s) => (
        <div className="flex gap-1">
          {s.cooldown_up_active && <Badge variant="secondary">up</Badge>}
          {s.cooldown_down_active && <Badge variant="secondary">down</Badge>}
          {!s.cooldown_up_active && !s.cooldown_down_active && <span className="text-muted-foreground text-xs">—</span>}
        </div>
      ),
    },
    {
      key: 'last', label: 'Last action',
      sort: (a, b) => (a.last_action_at ?? 0) - (b.last_action_at ?? 0),
      render: (s) => s.last_action
        ? <span className="text-xs">{s.last_action} · {s.last_action_at ? new Date(s.last_action_at * 1000).toLocaleTimeString() : ''}</span>
        : <span className="text-muted-foreground text-xs">—</span>,
    },
  ]

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[{ label: 'Cluster', to: '/' }, { label: 'Services' }]} />
      <DataTable
        rows={services}
        columns={columns}
        search={{ placeholder: 'Search by name…', match: (s, q) => s.Name.toLowerCase().includes(q.toLowerCase()) }}
        onRowClick={(s) => navigate(`/services/${s.Name}`)}
        rowKey={(s) => s.Name}
        emptyMessage="No services loaded yet."
      />
    </div>
  )
}
