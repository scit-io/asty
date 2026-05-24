import { useEffect } from 'react'
import {
  Activity,
  Boxes,
  Cpu,
  HardDrive,
  Heart,
  MemoryStick,
  Server,
  Shield,
} from 'lucide-react'
import { MetricsChart } from '@/components/metrics-chart'
import { PageShell } from '@/components/page-shell'
import { ResourcesBlock } from '@/components/resources-block'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_TABS } from '@/pages/cluster/tabs'
import { Tile } from '@/components/tile'
import { NatsTiles } from '@/features/cluster/nats-tiles'
import { formatMB, formatMHz } from '@/lib/format'
import { useClusterStore } from '@/store/cluster'
import { useClusterAggregates, useServicesActiveCount } from '@/store/selectors'

// Cluster overview — the model's top page. Three concentric layers:
// the 4 base resource tiles, the 4 status tiles, and the Asty + NATS
// sub-blocks. The nodes table and cluster logs that used to live here
// have moved to /nodes and /logs respectively.
export default function Cluster() {
  const subscribeCluster = useClusterStore((s) => s.subscribeCluster)
  const servicesTotal = useClusterStore((s) => s.services.length)
  const clusterStatus = useClusterStore((s) => s.clusterStatus)
  const clusterCpuMetrics = useClusterStore((s) => s.clusterCpuMetrics)
  const clusterMemoryMetrics = useClusterStore((s) => s.clusterMemoryMetrics)
  const clusterRpsMetrics = useClusterStore((s) => s.clusterRpsMetrics)
  const aggregates = useClusterAggregates()
  const servicesActive = useServicesActiveCount()

  useEffect(() => subscribeCluster(), [subscribeCluster])

  const nodesHealthy = clusterStatus?.cluster.nodes_healthy ?? 0
  const nodesTotal = clusterStatus?.cluster.nodes_total ?? 0
  const healthPct = nodesTotal > 0 ? Math.round((nodesHealthy / nodesTotal) * 100) : 0

  return (
    <PageShell>
      <h2 className="text-lg font-semibold">Cluster</h2>
      <ResourceTabs items={CLUSTER_TABS} />

      <section className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={aggregates.cluster.cpuUsage} total={aggregates.cluster.cpuTotal} format={formatMHz} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={aggregates.cluster.memoryUsage} total={aggregates.cluster.memoryTotal} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={aggregates.cluster.diskUsage} total={aggregates.cluster.diskTotal} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" bar
            title="RPS" icon={<Activity className="h-4 w-4" />}
            value={Math.round(aggregates.cluster.rps)} hint="Requests per second" />

          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster CPU" data={clusterCpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster Memory" data={clusterMemoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster RPS" data={clusterRpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Nodes" icon={<Server className="h-4 w-4" />}
            value={`${nodesHealthy} / ${nodesTotal}`} hint="active / total" />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Services" icon={<Boxes className="h-4 w-4" />}
            value={`${servicesActive} / ${servicesTotal}`} hint="active / loaded" />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" size="sm" mono
            title="Leader" icon={<Shield className="h-4 w-4" />}
            value={clusterStatus?.cluster.leader || '—'}
            hint={clusterStatus?.cluster.leader_ip || '—'} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Health" icon={<Heart className="h-4 w-4" />}
            value={`${healthPct}%`} hint={`${nodesHealthy} of ${nodesTotal} healthy`} />
        </div>
      </section>

      <ResourcesBlock title="Asty" data={aggregates.asty} />

      <NatsTiles data={aggregates.nats} />
    </PageShell>
  )
}
