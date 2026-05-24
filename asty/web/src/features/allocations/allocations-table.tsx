import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Cpu, MemoryStick, MoreHorizontal, RotateCw, StopCircle } from 'lucide-react'
import { DataTable, type Column } from '@/components/data-table'
import { UsageCell } from '@/components/usage-cell'
import { formatMB, formatMHz, formatPercent } from '@/lib/format'
import { routes } from '@/lib/routes'
import { uptimeLabel } from '@/lib/uptime'
import { allocHealthVariant, allocStatusVariant } from '@/lib/variants'
import { useAllocationActions } from './use-allocation-actions'
import type { Allocation, ServiceDefinition } from '@/types'

type Resources = ServiceDefinition['Resources']

interface AllocationsTableProps {
  rows: Allocation[]
  // scope decides which "context" column shows (Service for node-
  // scoped pages — we're already on a node, the row is the service;
  // Node for service-scoped pages — the row is which node hosts the
  // copy), plus minor differences (Version column only on node
  // scope, search predicate, empty message).
  scope: 'node' | 'service'
  // resources(a) returns the service's resource limits for that
  // allocation. On node scope: lookup by a.service_name. On service
  // scope: same value for every row — pages that know it pass a
  // closure that ignores the arg.
  resources: (a: Allocation) => Resources | undefined
  emptyMessage: string
  searchPlaceholder: string
}

// AllocationsTable powers both the per-node and per-service alloc
// list pages. DataTable wraps the search + sort + pagination; the
// columns + row actions stay one source of truth. Hover-row actions
// open a restart/stop dropdown wired through useAllocationActions.
export function AllocationsTable({
  rows, scope, resources, emptyMessage, searchPlaceholder,
}: AllocationsTableProps) {
  const navigate = useNavigate()
  const { act, pending } = useAllocationActions()

  const columns: Column<Allocation>[] = [
    ...(scope === 'service'
      ? [
        {
          key: 'id', label: 'Allocation',
          render: (a: Allocation) => <span className="font-mono text-xs">{a.id.slice(0, 12)}</span>,
        },
        {
          key: 'node', label: 'Node',
          sort: (a: Allocation, b: Allocation) => a.node_id.localeCompare(b.node_id),
          render: (a: Allocation) => <span className="font-mono font-medium">{a.node_id}</span>,
        },
      ]
      : [
        {
          key: 'service', label: 'Service',
          sort: (a: Allocation, b: Allocation) => a.service_name.localeCompare(b.service_name),
          render: (a: Allocation) => <span className="font-medium">{a.service_name}</span>,
        },
      ]),
    {
      key: 'status', label: 'Status',
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (a) => <Badge variant={allocStatusVariant(a.status)}>{a.status}</Badge>,
    },
    ...(scope === 'node'
      ? [{
        key: 'version', label: 'Version',
        render: (a: Allocation) => <span className="font-mono text-xs">{a.version || '—'}</span>,
      }]
      : []),
    {
      key: 'health', label: 'Health',
      render: (a) => <Badge variant={allocHealthVariant(a.health_status)}>{a.health_status || 'unknown'}</Badge>,
    },
    {
      key: 'cpu', label: 'CPU',
      sort: (a, b) => a.cpu_usage - b.cpu_usage,
      render: (a) => {
        const res = resources(a)
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
      key: 'mem', label: 'RAM',
      sort: (a, b) => a.memory_usage - b.memory_usage,
      render: (a) => {
        const res = resources(a)
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
    <DataTable
      rows={rows}
      columns={columns}
      search={{
        placeholder: searchPlaceholder,
        match: (a, q) => {
          const needle = q.toLowerCase()
          return scope === 'service'
            ? a.node_id.toLowerCase().includes(needle)
            : a.service_name.toLowerCase().includes(needle)
        },
      }}
      onRowClick={(a) => navigate(routes.allocation(a.node_id, a.id))}
      rowKey={(a) => a.id}
      emptyMessage={emptyMessage}
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
  )
}
