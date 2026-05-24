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
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { ServiceHeader } from '@/components/service-header'
import { UsageCell } from '@/components/usage-cell'
import { DataTable, type Column } from '@/components/data-table'
import { api } from '@/api/client'
import { formatMB, formatMHz, formatPercent } from '@/lib/format'
import { routes } from '@/lib/routes'
import { serviceTabs } from '@/pages/services/[name]/tabs'
import { allocHealthVariant, allocStatusVariant } from '@/lib/variants'
import { useClusterStore } from '@/store/cluster'
import type { Allocation } from '@/types'

// All allocations for a single service, regardless of node. Subscribes
// to the service stream (which already returns the alloc list); the
// columns mirror node-allocations.tsx with the Node column shown
// instead of Service since we're scoped to one service.
export default function ServiceAllocations() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const { serviceCache, subscribeService, services } = useClusterStore()
  const cached = name ? serviceCache[name] : undefined
  const allocations = cached?.allocations || []
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const res = name ? services.find((s) => s.Name === name)?.Resources : undefined

  useEffect(() => {
    if (!name) return
    return subscribeService(name)
  }, [name, subscribeService])

  const act = async (kind: 'restart' | 'stop', a: Allocation) => {
    setPending((p) => ({ ...p, [a.id]: true }))
    try {
      await (kind === 'restart' ? api.restartAllocation(a.node_id, a.id) : api.stopAllocation(a.node_id, a.id))
      toast.success(`${kind === 'restart' ? 'Restarted' : 'Stopped'} ${a.id.slice(0, 8)}`)
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
    { key: 'id', label: 'Allocation', render: (a) => <span className="font-mono text-xs">{a.id.slice(0, 12)}</span> },
    {
      key: 'node', label: 'Node',
      sort: (a, b) => a.node_id.localeCompare(b.node_id),
      render: (a) => <span className="font-mono font-medium">{a.node_id}</span>,
    },
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (a) => <Badge variant={allocStatusVariant(a.status)}>{a.status}</Badge>,
    },
    {
      key: 'health', label: 'Health',
      render: (a) => <Badge variant={allocHealthVariant(a.health_status)}>{a.health_status || 'unknown'}</Badge>,
    },
    {
      key: 'cpu', label: 'CPU',
      sort: (a, b) => a.cpu_usage - b.cpu_usage,
      render: (a) => (
        <UsageCell
          icon={Cpu}
          primary={formatPercent(a.cpu_usage)}
          secondary={res ? `${formatMHz(Math.round((a.cpu_usage / 100) * res.CPU))} / ${formatMHz(res.CPU)}` : undefined}
        />
      ),
    },
    {
      key: 'mem', label: 'Memory',
      sort: (a, b) => a.memory_usage - b.memory_usage,
      render: (a) => {
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
    { key: 'disk', label: 'Disk', sort: (a, b) => a.disk_usage - b.disk_usage, render: (a) => <span className="text-sm">{formatMB(a.disk_usage)}</span> },
    { key: 'restarts', label: 'Restarts', sort: (a, b) => a.restarts - b.restarts, render: (a) => a.restarts },
    {
      key: 'uptime', label: 'Uptime',
      render: (a) => <span className="text-sm">{uptimeLabel(a.started_at, a.status)}</span>,
    },
  ]

  if (!name) return null
  return (
    <PageShell>
      <ServiceHeader name={name} service={cached?.service ?? null} tail={[{ label: 'Allocations' }]} />
      <ResourceTabs items={serviceTabs(name)} />
      {!cached ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <Card>
          <CardContent className="pt-6">
            <DataTable
              rows={allocations}
              columns={columns}
              search={{ placeholder: 'Search by node…', match: (a, q) => a.node_id.toLowerCase().includes(q.toLowerCase()) }}
              onRowClick={(a) => navigate(routes.allocation(a.node_id, a.id))}
              rowKey={(a) => a.id}
              emptyMessage="No allocations for this service."
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
