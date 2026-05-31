import {
  Activity,
  Boxes,
  Cpu,
  HardDrive,
  Heart,
  HardDriveDownload,
  MemoryStick,
  Server,
  Shield,
} from 'lucide-react'
import { MetricsChart } from '@/components/metrics-chart'
import { PageShell } from '@/components/page-shell'
import { ResourcesBlock } from '@/components/resources-block'
import { ResourceTabs } from '@/components/resource-tabs'
import { useClusterTabs } from '@/pages/cluster/tabs'
import { Tile } from '@/components/tile'
import { NatsTiles } from '@/features/cluster/nats-tiles'
import { formatMB, formatMHz } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { useSubscribe } from '@/lib/use-subscribe'
import { useClusterStore } from '@/store/cluster'
import { useClusterAggregates, useServicesActiveCount } from '@/store/selectors'

// Cluster overview — the model's top page. Three concentric layers:
// the 4 base resource tiles, the 4 status tiles, and the Asty + NATS
// sub-blocks. The nodes table and cluster logs that used to live here
// have moved to /nodes and /logs respectively.
export default function Cluster() {
  const t = useT()
  const tabs = useClusterTabs()
  const subscribeCluster = useClusterStore((s) => s.subscribeCluster)
  const servicesTotal = useClusterStore((s) => s.services.length)
  const clusterStatus = useClusterStore((s) => s.clusterStatus)
  const clusterCpuMetrics = useClusterStore((s) => s.clusterCpuMetrics)
  const clusterMemoryMetrics = useClusterStore((s) => s.clusterMemoryMetrics)
  const clusterRpsMetrics = useClusterStore((s) => s.clusterRpsMetrics)
  const aggregates = useClusterAggregates()
  const servicesActive = useServicesActiveCount()

  useSubscribe(subscribeCluster)

  const nodesHealthy = clusterStatus?.cluster.nodes_healthy ?? 0
  const nodesTotal = clusterStatus?.cluster.nodes_total ?? 0
  const healthPct = nodesTotal > 0 ? Math.round((nodesHealthy / nodesTotal) * 100) : 0

  return (
    <PageShell>
      <h2 className="text-lg font-semibold">{t('section.cluster')}</h2>
      <ResourceTabs items={tabs} />

      <section className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          {/* Resource tiles — five cards (CPU / RAM / Swap / Disk /
              RPS) in their own subgrid so 5-up at lg keeps each tile
              the same width. 12-col can't divide by 5 cleanly. */}
          <div className="col-span-12 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
            <Tile variant="metric"
              title={t('tile.cpu')} icon={<Cpu className="h-4 w-4" />}
              usage={aggregates.cluster.cpuUsage} total={aggregates.cluster.cpuTotal} format={formatMHz} />
            <Tile variant="metric"
              title={t('tile.ram')} icon={<MemoryStick className="h-4 w-4" />}
              usage={aggregates.cluster.memoryUsage} total={aggregates.cluster.memoryTotal} format={formatMB} />
            <Tile variant="metric"
              title={t('tile.swap')} icon={<HardDriveDownload className="h-4 w-4" />}
              usage={aggregates.cluster.swapUsage} total={aggregates.cluster.swapTotal} format={formatMB} />
            <Tile variant="metric"
              title={t('tile.disk')} icon={<HardDrive className="h-4 w-4" />}
              usage={aggregates.cluster.diskUsage} total={aggregates.cluster.diskTotal} format={formatMB} />
            <Tile variant="stat" bar
              title={t('tile.rps')} icon={<Activity className="h-4 w-4" />}
              value={Math.round(aggregates.cluster.rps)} hint={t('common.requests_per_second')} />
          </div>

          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.cluster_cpu')} data={clusterCpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.cluster_ram')} data={clusterMemoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title={t('chart.cluster_rps')} data={clusterRpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title={t('tile.nodes')} icon={<Server className="h-4 w-4" />}
            value={`${nodesHealthy} / ${nodesTotal}`} hint={t('tile.hint.active_total')} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title={t('tile.services')} icon={<Boxes className="h-4 w-4" />}
            value={`${servicesActive} / ${servicesTotal}`} hint={t('tile.hint.active_loaded')} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" size="sm" mono
            title={t('tile.leader')} icon={<Shield className="h-4 w-4" />}
            value={clusterStatus?.cluster.leader || '—'}
            hint={(() => {
              const c = clusterStatus?.cluster
              const left = [c?.leader_ip, c?.leader_dc].filter(Boolean).join('/')
              return [left, c?.leader_host].filter(Boolean).join(' · ') || '—'
            })()} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title={t('tile.health')} icon={<Heart className="h-4 w-4" />}
            value={`${healthPct}%`} hint={t('tile.hint.x_of_y_healthy', { healthy: nodesHealthy, total: nodesTotal })} />
        </div>
      </section>

      <ResourcesBlock title="Asty" data={aggregates.asty} />

      <NatsTiles data={aggregates.nats} />
    </PageShell>
  )
}
