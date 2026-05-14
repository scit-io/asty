import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { DataTable, type Column } from '@/components/data-table'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'
import type { Node } from '@/types'
import { toast } from 'sonner'

const statusVariant = (s: Node['status']): 'default' | 'secondary' | 'destructive' | 'outline' => {
  switch (s) {
    case 'ready': return 'default'
    case 'down': return 'destructive'
    case 'draining':
    case 'drained':
    case 'paused': return 'secondary'
    default: return 'outline'
  }
}

const percent = (used: number, total: number) => total > 0 ? Math.round((used / total) * 100) : 0

// Nodes list page (/nodes). DataTable handles search/sort/pagination;
// per-row Drain toggle calls api.drainNode and optimistically updates
// the store so the switch reflects the new state before the SSE
// catches up.
export default function Nodes() {
  const navigate = useNavigate()
  const nodes = useClusterStore((s) => s.nodes)
  const updateNodeStatus = useClusterStore((s) => s.updateNodeStatus)
  const [pending, setPending] = useState<Record<string, boolean>>({})

  const handleDrain = async (n: Node, enable: boolean) => {
    setPending((p) => ({ ...p, [n.id]: true }))
    try {
      await api.drainNode(n.id, enable)
      updateNodeStatus(n.id, enable ? 'draining' : 'ready')
      toast.success(`${enable ? 'Draining' : 'Resuming'} ${n.id}`)
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setPending((p) => ({ ...p, [n.id]: false }))
    }
  }

  const columns: Column<Node>[] = [
    {
      key: 'id', label: 'Node',
      sort: (a, b) => a.id.localeCompare(b.id),
      render: (n) => <span className="font-mono text-sm">{n.id}</span>,
    },
    { key: 'dc', label: 'DC', render: (n) => n.datacenter },
    { key: 'ip', label: 'IP', render: (n) => <span className="font-mono text-sm">{n.ip || '—'}</span> },
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (n) => <Badge variant={statusVariant(n.status)}>{n.status}</Badge>,
    },
    {
      key: 'cpu', label: 'CPU',
      sort: (a, b) => percent(a.cpu_total - a.cpu_available, a.cpu_total) - percent(b.cpu_total - b.cpu_available, b.cpu_total),
      render: (n) => `${percent(n.cpu_total - n.cpu_available, n.cpu_total)}%`,
    },
    {
      key: 'mem', label: 'Memory',
      sort: (a, b) => percent(a.memory_total - a.memory_available, a.memory_total) - percent(b.memory_total - b.memory_available, b.memory_total),
      render: (n) => `${percent(n.memory_total - n.memory_available, n.memory_total)}%`,
    },
    {
      key: 'allocs', label: 'Allocations',
      sort: (a, b) => a.allocations_running - b.allocations_running,
      render: (n) => <span className="font-mono text-sm">{n.allocations_running} / {n.allocations_planned}</span>,
    },
    {
      key: 'drain', label: 'Drain',
      render: (n) => (
        <Switch
          checked={n.status === 'draining' || n.status === 'drained'}
          disabled={pending[n.id]}
          onCheckedChange={(checked) => handleDrain(n, checked)}
          onClick={(e) => e.stopPropagation()}
        />
      ),
    },
  ]

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[{ label: 'Cluster', to: '/' }, { label: 'Nodes' }]} />
      <DataTable
        rows={nodes}
        columns={columns}
        search={{ placeholder: 'Search by ID or IP…', match: (n, q) => n.id.toLowerCase().includes(q.toLowerCase()) || (n.ip ?? '').includes(q) }}
        onRowClick={(n) => navigate(`/nodes/${n.id}`)}
        rowKey={(n) => n.id}
        emptyMessage="No nodes registered yet."
      />
    </div>
  )
}
