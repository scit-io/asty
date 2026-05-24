import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Cpu, MemoryStick } from 'lucide-react'
import { DataTable, type Column } from '@/components/data-table'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_TABS } from '@/pages/cluster/tabs'
import { NodeDrainDialog } from '@/components/node-drain-dialog'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import { formatMB, formatMHz } from '@/lib/format'
import { nodeStatusSwitchClass } from '@/lib/node-status'
import { useClusterStore } from '@/store/cluster'
import type { Node } from '@/types'
import { toast } from 'sonner'

// statusVariant maps a node lifecycle into one of the project's
// custom Badge variants so colour matches operator expectation:
// green = ready, yellow = transitioning, red = down.
const statusVariant = (s: Node['status']): 'success' | 'warning' | 'destructive' | 'secondary' => {
  switch (s) {
    case 'ready': return 'success'
    case 'down': return 'destructive'
    case 'draining':
    case 'drained': return 'warning'
    default: return 'secondary'
  }
}

const percent = (used: number, total: number) => total > 0 ? Math.round((used / total) * 100) : 0

// Nodes list page (/nodes). DataTable handles search/sort/pagination;
// per-row Drain toggle calls api.drainNode and optimistically updates
// the store so the switch reflects the new state before the SSE
// catches up.
export default function Nodes() {
  const navigate = useNavigate()
  const subscribeNodes = useClusterStore((s) => s.subscribeNodes)
  const nodes = useClusterStore((s) => s.nodes)
  const updateNodeStatus = useClusterStore((s) => s.updateNodeStatus)
  const [pending, setPending] = useState<Record<string, boolean>>({})
  // drainTarget is the node id awaiting drain confirmation. The
  // detail page's dialog is reused verbatim — opening it on a
  // toggle-on keeps the row's switch optimistic (snaps back if the
  // user cancels) and avoids forking the wording.
  const [drainTarget, setDrainTarget] = useState<Node | null>(null)

  useEffect(() => subscribeNodes(), [subscribeNodes])

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
      render: (n) => <span className="font-mono font-medium">{n.id}</span>,
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
      render: (n) => {
        const used = n.cpu_total - n.cpu_available
        const pct = percent(used, n.cpu_total)
        return (
          <div className="flex items-center gap-2">
            <Cpu className="h-4 w-4 text-muted-foreground" />
            <div className="space-y-1">
              <div className="text-sm font-medium">{pct}%</div>
              <div className="text-xs text-muted-foreground">{formatMHz(used)} / {formatMHz(n.cpu_total)}</div>
            </div>
          </div>
        )
      },
    },
    {
      key: 'mem', label: 'Memory',
      sort: (a, b) => percent(a.memory_total - a.memory_available, a.memory_total) - percent(b.memory_total - b.memory_available, b.memory_total),
      render: (n) => {
        const used = n.memory_total - n.memory_available
        const pct = percent(used, n.memory_total)
        return (
          <div className="flex items-center gap-2">
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
            <div className="space-y-1">
              <div className="text-sm font-medium">{pct}%</div>
              <div className="text-xs text-muted-foreground">{formatMB(used)} / {formatMB(n.memory_total)}</div>
            </div>
          </div>
        )
      },
    },
    {
      key: 'allocs', label: 'Allocations', className: 'text-right',
      sort: (a, b) => a.allocations_running - b.allocations_running,
      render: (n) => <span className="text-sm"><b>{n.allocations_running}</b> / {n.allocations_planned}</span>,
    },
    {
      key: 'drain', label: 'Drain',
      render: (n) => (
        <Switch
          checked={n.status === 'draining' || n.status === 'drained'}
          disabled={pending[n.id]}
          className={nodeStatusSwitchClass(n.status)}
          onCheckedChange={(checked) => checked ? setDrainTarget(n) : handleDrain(n, false)}
          onClick={(e) => e.stopPropagation()}
        />
      ),
    },
  ]

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <h2 className="text-lg font-semibold">Cluster</h2>
      <ResourceTabs items={CLUSTER_TABS} />
      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={nodes}
            columns={columns}
            search={{ placeholder: 'Search by ID or IP…', match: (n, q) => n.id.toLowerCase().includes(q.toLowerCase()) || (n.ip ?? '').includes(q) }}
            onRowClick={(n) => navigate(routes.node(n.id))}
            rowKey={(n) => n.id}
            emptyMessage="No nodes registered yet."
          />
        </CardContent>
      </Card>
      <NodeDrainDialog
        open={drainTarget !== null}
        nodeId={drainTarget?.id ?? ''}
        onOpenChange={(open) => { if (!open) setDrainTarget(null) }}
        onConfirm={() => {
          const n = drainTarget
          setDrainTarget(null)
          if (n) handleDrain(n, true)
        }}
      />
    </div>
  )
}
