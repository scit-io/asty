import { useCallback, useMemo, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Cpu, HardDrive, MemoryStick, MoreHorizontal, RotateCw, StopCircle } from 'lucide-react'
import { DataTable, type CellSpec, type Column } from '@/components/data-table'
import { AllocIdBadge } from '@/components/alloc-id-badge'
import { NodeIdentityTooltip } from '@/components/node-identity-tooltip'
import { UsageCell } from '@/components/usage-cell'
import { useClusterStore } from '@/store/cluster'
import { formatMB, formatMHz, formatPercent } from '@/lib/format'
import { routes } from '@/lib/routes'
import { uptimeLabel } from '@/lib/uptime'
import { useT, allocStatusKey, allocHealthKey } from '@/lib/i18n'
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
  // closure that ignores the arg. Caller should stabilise this
  // reference (useCallback / useRef) so the columns memo below stays
  // stable across SSE flushes.
  resources: (a: Allocation) => Resources | undefined
  emptyMessage: string
  searchPlaceholder: string
}

// AllocationsTable powers both the per-node and per-service alloc
// list pages. Each column declares its `deps` — the values its render
// reads from `row` plus any outer state — and DataTable's CellMemo
// skips renders when those values are unchanged. No per-cell
// components: the universal mechanism in DataTable covers every
// table the same way.
export function AllocationsTable({
  rows, scope, resources, emptyMessage, searchPlaceholder,
}: AllocationsTableProps) {
  const t = useT()
  const navigate = useNavigate()
  const { act, pending } = useAllocationActions()
  // nodeByID maps node_id → Node for the dc/ip/host tooltip in the
  // Node column. Select the array reference (stable while the store
  // hasn't published a new list) and derive the Map in a memo —
  // returning `new Map(...)` directly from the selector would build a
  // fresh object every render and trip Zustand into an infinite loop.
  const nodes = useClusterStore((s) => s.nodes)
  const nodeByID = useMemo(
    () => new Map(nodes.map((n) => [n.id, n])),
    [nodes],
  )

  // act() needs the full alloc but the action cell only sees its id —
  // read the live row from a ref so the wrappers stay stable.
  const rowsRef = useRef(rows)
  rowsRef.current = rows
  const onRestart = useCallback((id: string) => {
    const a = rowsRef.current.find((x) => x.id === id)
    if (a) act('restart', a)
  }, [act])
  const onStop = useCallback((id: string) => {
    const a = rowsRef.current.find((x) => x.id === id)
    if (a) act('stop', a)
  }, [act])
  const onRowClick = useCallback(
    (a: Allocation) => navigate(routes.allocation(a.node_id, a.id)),
    [navigate],
  )

  const columns = useMemo<Column<Allocation>[]>(() => [
    ...(scope === 'service'
      ? [
        {
          key: 'id', label: t('allocs.col.allocation'),
          render: (a: Allocation) => <AllocIdBadge id={a.id} />,
          deps: (a: Allocation) => [a.id],
        },
        {
          key: 'node', label: t('allocs.col.node'),
          sort: (a: Allocation, b: Allocation) => a.node_id.localeCompare(b.node_id),
          render: (a: Allocation) => {
            const n = nodeByID.get(a.node_id)
            return (
              <span className="inline-flex items-center gap-1.5 font-medium">
                {a.node_id}
                {/* stopPropagation: the row has an onClick that
                    navigates to the allocation page; clicking the
                    tooltip icon shouldn't trigger that. */}
                <span onClick={(e) => e.stopPropagation()}>
                  <NodeIdentityTooltip dc={n?.datacenter} ip={n?.ip} host={n?.host} />
                </span>
              </span>
            )
          },
          deps: (a: Allocation) => {
            const n = nodeByID.get(a.node_id)
            return [a.node_id, n?.datacenter, n?.ip, n?.host]
          },
        },
      ]
      : [
        {
          key: 'service', label: t('allocs.col.service'),
          sort: (a: Allocation, b: Allocation) => a.service_name.localeCompare(b.service_name),
          render: (a: Allocation) => <span className="font-medium">{a.service_name}</span>,
          deps: (a: Allocation) => [a.service_name],
        },
      ]),
    {
      key: 'status', label: t('allocs.col.status'),
      sort: (a, b) => a.status.localeCompare(b.status),
      render: (a) => <Badge variant={allocStatusVariant(a.status)}>{t(allocStatusKey(a.status))}</Badge>,
      deps: (a) => [a.status],
    },
    ...(scope === 'node'
      ? [{
        key: 'version', label: t('allocs.col.version'),
        render: (a: Allocation) => <span className="text-xs">{a.version || '—'}</span>,
        deps: (a: Allocation) => [a.version],
      }]
      : []),
    {
      key: 'health', label: t('allocs.col.health'),
      render: (a) => <Badge variant={allocHealthVariant(a.health_status)}>{t(allocHealthKey(a.health_status))}</Badge>,
      deps: (a) => [a.health_status],
    },
    {
      key: 'cpu', label: t('allocs.col.cpu'),
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
      deps: (a) => [a.cpu_usage, resources(a)?.CPU],
    },
    {
      key: 'mem', label: t('allocs.col.ram'),
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
      deps: (a) => [a.memory_usage, resources(a)?.Memory],
    },
    {
      key: 'disk', label: t('allocs.col.disk'),
      sort: (a, b) => a.disk_usage - b.disk_usage,
      render: (a) => {
        const res = resources(a)
        const budget = res?.Disk ?? 0
        const pct = budget > 0 ? Math.round((a.disk_usage / budget) * 100) : null
        return (
          <UsageCell
            icon={HardDrive}
            primary={pct !== null ? `${pct}%` : formatMB(a.disk_usage)}
            secondary={budget > 0 ? `${formatMB(a.disk_usage)} / ${formatMB(budget)}` : undefined}
          />
        )
      },
      deps: (a) => [a.disk_usage, resources(a)?.Disk],
    },
    {
      key: 'restarts', label: t('allocs.col.restarts'),
      sort: (a, b) => a.restarts - b.restarts,
      render: (a) => a.restarts,
      deps: (a) => [a.restarts],
    },
    {
      key: 'uptime', label: t('allocs.col.uptime'),
      render: (a) => <span className="text-sm">{uptimeLabel(a.started_at, a.status)}</span>,
      // `t` participates in deps so a locale flip invalidates the
      // cell memo — uptime's wire fields don't change on SSE ticks,
      // so without this the unit suffix (s/m/h/d ↔ с/м/ч/д) would
      // stay frozen at the previous locale.
      deps: (a) => [a.started_at, a.status, t],
    },
  ], [scope, resources, t])

  const search = useMemo(() => ({
    placeholder: searchPlaceholder,
    match: (a: Allocation, q: string) => {
      const needle = q.toLowerCase()
      return scope === 'service'
        ? a.node_id.toLowerCase().includes(needle)
        : a.service_name.toLowerCase().includes(needle)
    },
  }), [scope, searchPlaceholder])

  const rowKey = useCallback((a: Allocation) => a.id, [])
  // Actions column — Radix DropdownMenu, the heaviest cell. Its deps
  // are the alloc id (to identify the row) and the pending flag for
  // that id (drives disabled state on the trigger).
  const actions = useMemo<CellSpec<Allocation>>(() => ({
    render: (a) => (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" disabled={pending[a.id]}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => onRestart(a.id)}>
            <RotateCw className="h-4 w-4 mr-2" /> {t('alloc.action.restart')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onStop(a.id)} className="text-destructive">
            <StopCircle className="h-4 w-4 mr-2" /> {t('alloc.action.stop')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    ),
    deps: (a) => [a.id, pending[a.id]],
  }), [pending, onRestart, onStop, t])

  return (
    <DataTable
      rows={rows}
      columns={columns}
      search={search}
      onRowClick={onRowClick}
      rowKey={rowKey}
      emptyMessage={emptyMessage}
      actions={actions}
    />
  )
}
