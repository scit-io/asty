import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { MoreHorizontal, RotateCw, StopCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { toast } from 'sonner'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { ResourceTabs } from '@/components/resource-tabs'
import { DataTable, type Column } from '@/components/data-table'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'
import type { Allocation } from '@/types'

const healthVariant = (h: Allocation['health_status']) => h === 'healthy' ? 'default' : h === 'unhealthy' ? 'destructive' : 'outline'
const statusVariant = (s: Allocation['status']) =>
  s === 'running' ? 'default' : s === 'failed' ? 'destructive' : s === 'pending' || s === 'starting' ? 'secondary' : 'outline'

// Per-node allocations list. Same DataTable contract as /nodes but
// scoped to one node; per-row dropdown lets the operator
// restart/stop without leaving the page.
export default function NodeAllocations() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const navigate = useNavigate()
  const { nodeCache, subscribeNode } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const allocations = cached?.allocations || []
  const [pending, setPending] = useState<Record<string, boolean>>({})

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
      render: (a) => <span className="font-mono text-sm">{a.service_name}</span>,
    },
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (a) => <Badge variant={statusVariant(a.status)}>{a.status}</Badge>,
    },
    { key: 'version', label: 'Version', render: (a) => <span className="font-mono text-xs">{a.version || '—'}</span> },
    {
      key: 'health', label: 'Health',
      render: (a) => <Badge variant={healthVariant(a.health_status)}>{a.health_status || 'unknown'}</Badge>,
    },
    {
      key: 'cpu', label: 'CPU%',
      sort: (a, b) => a.cpu_usage - b.cpu_usage,
      render: (a) => `${a.cpu_usage}%`,
    },
    {
      key: 'mem', label: 'Memory',
      sort: (a, b) => a.memory_usage - b.memory_usage,
      render: (a) => `${a.memory_usage} MB`,
    },
    {
      key: 'disk', label: 'Disk',
      sort: (a, b) => a.disk_usage - b.disk_usage,
      render: (a) => `${a.disk_usage} MB`,
    },
    {
      key: 'restarts', label: 'Restarts',
      sort: (a, b) => a.restarts - b.restarts,
      render: (a) => a.restarts,
    },
    {
      key: 'uptime', label: 'Uptime',
      render: (a) => a.started_at && a.status === 'running'
        ? formatDistanceToNow(new Date(a.started_at), { addSuffix: false })
        : '—',
    },
  ]

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4">
      <Breadcrumbs items={[
        { label: 'Cluster', to: '/' },
        { label: 'Nodes', to: '/nodes' },
        { label: node?.id || nodeId || '', to: `/nodes/${nodeId}` },
        { label: 'Allocations' },
      ]} />
      {nodeId && (
        <ResourceTabs items={[
          { to: `/nodes/${nodeId}`, label: 'Overview' },
          { to: `/nodes/${nodeId}/allocations`, label: 'Allocations' },
          { to: `/nodes/${nodeId}/logs`, label: 'Logs' },
        ]} />
      )}

      {!cached ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <DataTable
          rows={allocations}
          columns={columns}
          search={{ placeholder: 'Search by service name…', match: (a, q) => a.service_name.toLowerCase().includes(q.toLowerCase()) }}
          onRowClick={(a) => navigate(`/nodes/${nodeId}/allocations/${a.id}`)}
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
      )}
    </div>
  )
}
