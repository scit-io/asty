import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Cpu, MemoryStick, MoreHorizontal, RotateCw, StopCircle } from 'lucide-react'
import { uptimeLabel } from '@/lib/uptime'
import { toast } from 'sonner'
import { NodeHeader } from '@/components/node-header'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { UsageCell } from '@/components/usage-cell'
import { DataTable, type Column } from '@/components/data-table'
import { api } from '@/api/client'
import { routes } from '@/lib/routes'
import { nodeTabs } from '@/pages/cluster/nodes/[nodeId]/tabs'
import { formatMB, formatMHz, formatPercent } from '@/lib/format'
import { allocHealthVariant, allocStatusVariant } from '@/lib/variants'
import { useClusterStore } from '@/store/cluster'
import type { Allocation } from '@/types'

// Per-node allocations list. Same DataTable contract as /nodes but
// scoped to one node; per-row dropdown lets the operator
// restart/stop without leaving the page.
export default function NodeAllocations() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const { nodeCache, subscribeNode, services } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const allocations = cached?.allocations || []
  const [pending, setPending] = useState<Record<string, boolean>>({})

  const limits = (name: string) => services.find((s) => s.Name === name)?.Resources

  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  const act = async (kind: 'restart' | 'stop', a: Allocation) => {
    if (!nodeId) return
    setPending((p) => ({ ...p, [a.id]: true }))
    try {
      await (kind === 'restart' ? api.restartAllocation(nodeId, a.id) : api.stopAllocation(nodeId, a.id))
      toast.success(`${kind === 'restart' ? 'Restarted' : 'Stopped'} ${a.service_name}`)
      // Stop deletes the slot; the reconciler may backfill on a
      // different node. Send the operator to the nodes list so they
      // can spot where the replacement landed.
      if (kind === 'stop') {
        navigate(routes.nodes)
      }
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setPending((p) => ({ ...p, [a.id]: false }))
    }
  }

  const columns: Column<Allocation>[] = [
    {
      key: 'service', label: 'Service',
      sort: (a, b) => a.service_name.localeCompare(b.service_name),
      render: (a) => <span className="font-medium">{a.service_name}</span>,
    },
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (a) => <Badge variant={allocStatusVariant(a.status)}>{a.status}</Badge>,
    },
    { key: 'version', label: 'Version', render: (a) => <span className="font-mono text-xs">{a.version || '—'}</span> },
    {
      key: 'health', label: 'Health',
      render: (a) => <Badge variant={allocHealthVariant(a.health_status)}>{a.health_status || 'unknown'}</Badge>,
    },
    {
      key: 'cpu', label: 'CPU',
      sort: (a, b) => a.cpu_usage - b.cpu_usage,
      render: (a) => {
        const res = limits(a.service_name)
        return (
          <UsageCell
            icon={Cpu}
            primary={formatPercent(a.cpu_usage)}
            secondary={res ? `${formatMHz(Math.round((a.cpu_usage / 100) * res.CPU))} / ${formatMHz(res.CPU)}` : undefined}
          />
        )
      },
    },
    {
      key: 'mem', label: 'Memory',
      sort: (a, b) => a.memory_usage - b.memory_usage,
      render: (a) => {
        const res = limits(a.service_name)
        const pct = res ? Math.round((a.memory_usage / res.Memory) * 100) : null
        return (
          <UsageCell
            icon={MemoryStick}
            primary={pct !== null ? `${pct}%` : formatMB(a.memory_usage)}
            secondary={res ? `${formatMB(a.memory_usage)} / ${formatMB(res.Memory)}` : undefined}
          />
        )
      },
    },
    {
      key: 'disk', label: 'Disk',
      sort: (a, b) => a.disk_usage - b.disk_usage,
      render: (a) => <span className="text-sm">{formatMB(a.disk_usage)}</span>,
    },
    {
      key: 'restarts', label: 'Restarts',
      sort: (a, b) => a.restarts - b.restarts,
      render: (a) => a.restarts,
    },
    {
      key: 'uptime', label: 'Uptime',
      render: (a) => <span className="text-sm">{uptimeLabel(a.started_at, a.status)}</span>,
    },
  ]

  return (
    <PageShell>
      {node && <NodeHeader node={node} tail={[{ label: 'Allocations' }]} />}
      {nodeId && (
        <ResourceTabs items={nodeTabs(nodeId)} />
      )}

      {!cached ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <DataTable
              rows={allocations}
              columns={columns}
              search={{ placeholder: 'Search by service name…', match: (a, q) => a.service_name.toLowerCase().includes(q.toLowerCase()) }}
              onRowClick={(a) => navigate(routes.allocation(nodeId!, a.id))}
              rowKey={(a) => a.id}
              emptyMessage="No allocations on this node."
              actions={(a) => (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="sm" disabled={pending[a.id]}>
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => act('restart', a)}>
                      <RotateCw className="h-4 w-4 mr-2" /> Restart
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => act('stop', a)} className="text-destructive">
                      <StopCircle className="h-4 w-4 mr-2" /> Stop
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            />
          </CardContent>
        </Card>
      )}
    </PageShell>
  )
}
