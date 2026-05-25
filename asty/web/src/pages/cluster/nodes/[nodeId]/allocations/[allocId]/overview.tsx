import { useParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { LoadingBlock } from '@/components/loading-block'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Activity, Clock, Cpu, HardDrive, MemoryStick, RefreshCw, RotateCw, StopCircle, Tag, Wrench } from 'lucide-react'
import { formatMB, formatMHz } from '@/lib/format'
import { AllocationHeader } from '@/components/allocation-header'
import { MetricsChart } from '@/components/metrics-chart'
import { PageShell } from '@/components/page-shell'
import { ResourceTabs } from '@/components/resource-tabs'
import { Tile } from '@/components/tile'
import { useAllocationActions } from '@/features/allocations/use-allocation-actions'
import { useT } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useAllocationTabs } from '@/pages/cluster/nodes/[nodeId]/allocations/[allocId]/tabs'
import { useClusterStore } from '@/store/cluster'

// Allocation Overview (/nodes/:id/allocations/:allocId) — first tab
// of the allocation section. CPU/Memory/RPS time series come from
// the allocation SSE's `metrics` event (per-tick sample of the
// allocation's process metrics + the gateway's per-(node, service)
// RPS attribution); the SPA accumulates them in the store.
export default function AllocationDetail() {
  const t = useT()
  const { nodeId, allocId } = useParams<{ nodeId: string; allocId: string }>()
  const tabs = useAllocationTabs(nodeId ?? '', allocId ?? '')
  const subscribeAllocation = useClusterStore((s) => s.subscribeAllocation)
  const cached = useClusterStore((s) => allocId ? s.allocationCache[allocId] : undefined)
  const node = useClusterStore((s) => nodeId ? s.nodeCache[nodeId]?.node : undefined)
  const allocation = cached?.allocation || null
  const svcDef = cached?.service || null
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const { act, pending } = useAllocationActions()

  useSubscribe(subscribeAllocation, nodeId, allocId)

  const cpuTotal = svcDef?.Resources.CPU ?? 100
  const memTotal = svcDef?.Resources.Memory ?? 0
  const diskTotal = node?.disk_total ?? 0
  const rps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  const uptimeStartedAt = allocation?.status === 'running' ? allocation.started_at : ''
  const diskUnit = node?.disk_type === 'ssd' || node?.disk_type === 'hdd' ? node.disk_type.toUpperCase() : ''
  const busy = allocation ? !!pending[allocation.id] : false

  if (!allocation) {
    return (
      <PageShell bare>
        <Skeleton className="h-8 w-64 mb-4" />
        <LoadingBlock />
      </PageShell>
    )
  }

  return (
    <PageShell>
      <AllocationHeader allocation={allocation} />

      <ResourceTabs items={tabs} />

      <section className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title={t('tile.cpu')} icon={<Cpu className="h-4 w-4" />}
            usage={allocation.cpu_usage} total={cpuTotal} format={formatMHz} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title={t('tile.ram')} icon={<MemoryStick className="h-4 w-4" />}
            usage={allocation.memory_usage} total={memTotal} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title={t('tile.disk')} icon={<HardDrive className="h-4 w-4" />}
            usage={allocation.disk_usage} total={diskTotal} unit={diskUnit} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" bar
            title={t('tile.rps')} icon={<Activity className="h-4 w-4" />}
            value={Math.round(rps)} hint={t('common.requests_per_second')} />

          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.allocation_cpu')} data={cpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.allocation_ram')} data={memoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.allocation_rps')} data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <Tile variant="stat" size="sm" title={t('tile.version')} icon={<Tag className="h-4 w-4" />}
            value={allocation.version || '—'}
            hint={t('tile.hint.pid', { pid: allocation.pid || '—' })} />

          <Tile variant="timestamp" title={t('tile.uptime')} icon={<Clock className="h-4 w-4" />}
            timestamp={uptimeStartedAt} />

          <Tile variant="stat" title={t('tile.restarts')} icon={<RefreshCw className="h-4 w-4" />}
            value={allocation.restarts}
            hint={t('tile.hint.consecutive_failures', { n: allocation.consecutive_failures })} />

          <Tile variant="actions" title={t('tile.maintenance')} icon={<Wrench className="h-4 w-4" />}
            actions={
              // gap-2 + flex-1 + min-w-0 on each Button so long labels
              // (e.g. "Перезапустить" / "Остановить") truncate with an
              // ellipsis instead of overflowing the narrow card. The
              // icon keeps its shrink-0 from Button's base styles; the
              // text span owns `truncate` so only the label shortens.
              <div className="flex gap-2 w-full">
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="outline" disabled={busy} className="flex-1 min-w-0">
                      <RotateCw className="h-4 w-4" />
                      <span className="truncate">{t('alloc.action.restart')}</span>
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>{t('alloc.restart.title', { service: allocation.service_name })}</AlertDialogTitle>
                      <AlertDialogDescription>{t('alloc.restart.description')}</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
                      <AlertDialogAction onClick={() => act('restart', allocation)}>{t('alloc.action.restart')}</AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="destructive" disabled={busy} className="flex-1 min-w-0">
                      <StopCircle className="h-4 w-4" />
                      <span className="truncate">{t('alloc.action.stop')}</span>
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>{t('alloc.stop.title', { service: allocation.service_name })}</AlertDialogTitle>
                      <AlertDialogDescription>{t('alloc.stop.description')}</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
                      <AlertDialogAction onClick={() => act('stop', allocation)}>{t('alloc.action.stop')}</AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            } />
        </div>
      </section>
    </PageShell>
  )
}
