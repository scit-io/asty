import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Cpu, MemoryStick } from 'lucide-react'
import { DataTable, type Column } from '@/components/data-table'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { UsageCell } from '@/components/usage-cell'
import { CLUSTER_TABS } from '@/pages/cluster/tabs'
import { NodeDrainDialog } from '@/components/node-drain-dialog'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import { formatMB, formatMHz } from '@/lib/format'
import { nodeStatusSwitchClass } from '@/lib/node-status'
import { useSubscribe } from '@/lib/use-subscribe'
import { useClusterStore } from '@/store/cluster'
import type { Node } from '@/types'
import { toast } from 'sonner'
import { nodeStatusVariant } from '@/lib/variants'

const percent = (used: number, total: number) => total > 0 ? Math.round((used / total) * 100) : 0

// Nodes list page (/nodes). DataTable handles search / sort /
// pagination + memoisation. Each column declares its `deps` so
// DataTable can skip the cell render when those values are unchanged
// — an SSE tick that only moves nats_in_msgs (not displayed) costs
// zero re-renders downstream.
export default function Nodes() {
  const navigate = useNavigate()
  const subscribeNodes = useClusterStore((s) => s.subscribeNodes)
  const nodes = useClusterStore((s) => s.nodes)
  const updateNodeStatus = useClusterStore((s) => s.updateNodeStatus)
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const [drainTargetId, setDrainTargetId] = useState<string | null>(null)

  useSubscribe(subscribeNodes)

  const handleDrain = useCallback(async (id: string, enable: boolean) => {
    setPending((p) => ({ ...p, [id]: true }))
    try {
      await api.drainNode(id, enable)
      updateNodeStatus(id, enable ? 'draining' : 'ready')
      toast.success(`${enable ? 'Draining' : 'Resuming'} ${id}`)
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setPending((p) => ({ ...p, [id]: false }))
    }
  }, [updateNodeStatus])

  const onRowClick = useCallback((n: Node) => navigate(routes.node(n.id)), [navigate])
  const rowKey = useCallback((n: Node) => n.id, [])
  const onDialogChange = useCallback((open: boolean) => { if (!open) setDrainTargetId(null) }, [])
  const onDialogConfirm = useCallback(() => {
    setDrainTargetId((id) => {
      if (id) handleDrain(id, true)
      return null
    })
  }, [handleDrain])

  // Memo so DataTable's searchEl cache survives SSE flushes — the
  // page wouldn't otherwise pass a stable `search` reference.
  const search = useMemo(() => ({
    placeholder: 'Search by ID or IP…',
    match: (n: Node, q: string) => n.id.toLowerCase().includes(q.toLowerCase()) || (n.ip ?? '').includes(q),
  }), [])

  const columns = useMemo<Column<Node>[]>(() => [
    {
      key: 'id', label: 'Node',
      sort: (a, b) => a.id.localeCompare(b.id),
      render: (n) => <span className="font-mono font-medium">{n.id}</span>,
      deps: (n) => [n.id],
    },
    {
      key: 'dc', label: 'DC',
      render: (n) => n.datacenter,
      deps: (n) => [n.datacenter],
    },
    {
      key: 'ip', label: 'IP',
      render: (n) => <span className="font-mono text-sm">{n.ip || '—'}</span>,
      deps: (n) => [n.ip],
    },
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (n) => <Badge variant={nodeStatusVariant(n.status)}>{n.status}</Badge>,
      deps: (n) => [n.status],
    },
    {
      key: 'cpu', label: 'CPU',
      sort: (a, b) => percent(a.cpu_total - a.cpu_available, a.cpu_total) - percent(b.cpu_total - b.cpu_available, b.cpu_total),
      render: (n) => {
        const used = n.cpu_total - n.cpu_available
        const pct = percent(used, n.cpu_total)
        return <UsageCell icon={Cpu} primary={`${pct}%`} secondary={`${formatMHz(used)} / ${formatMHz(n.cpu_total)}`} />
      },
      deps: (n) => [n.cpu_total, n.cpu_available],
    },
    {
      key: 'mem', label: 'RAM',
      sort: (a, b) => percent(a.memory_total - a.memory_available, a.memory_total) - percent(b.memory_total - b.memory_available, b.memory_total),
      render: (n) => {
        const used = n.memory_total - n.memory_available
        const pct = percent(used, n.memory_total)
        return <UsageCell icon={MemoryStick} primary={`${pct}%`} secondary={`${formatMB(used)} / ${formatMB(n.memory_total)}`} />
      },
      deps: (n) => [n.memory_total, n.memory_available],
    },
    {
      key: 'allocs', label: 'Allocations', className: 'text-right',
      sort: (a, b) => a.allocations_running - b.allocations_running,
      render: (n) => <span className="text-sm"><b>{n.allocations_running}</b> / {n.allocations_planned}</span>,
      deps: (n) => [n.allocations_running, n.allocations_planned],
    },
    {
      key: 'drain', label: 'Drain',
      render: (n) => (
        <Switch
          checked={n.status === 'draining' || n.status === 'drained'}
          disabled={pending[n.id]}
          className={nodeStatusSwitchClass(n.status)}
          onCheckedChange={(checked) => checked ? setDrainTargetId(n.id) : handleDrain(n.id, false)}
          onClick={(e) => e.stopPropagation()}
        />
      ),
      // Closes over `pending` and `handleDrain` — both stable per
      // render, but pending[n.id] varies per row and per user action.
      deps: (n) => [n.status, pending[n.id]],
    },
  ], [pending, handleDrain])

  return (
    <PageShell>
      <h2 className="text-lg font-semibold">Cluster</h2>
      <ResourceTabs items={CLUSTER_TABS} />
      <Card>
        <CardContent className="pt-6">
          <DataTable
            rows={nodes}
            columns={columns}
            search={search}
            onRowClick={onRowClick}
            rowKey={rowKey}
            emptyMessage="No nodes registered yet."
          />
        </CardContent>
      </Card>
      <NodeDrainDialog
        open={drainTargetId !== null}
        nodeId={drainTargetId ?? ''}
        onOpenChange={onDialogChange}
        onConfirm={onDialogConfirm}
      />
    </PageShell>
  )
}
