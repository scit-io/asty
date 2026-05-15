import { useEffect, useMemo } from 'react'
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Boxes,
  Cpu,
  Database,
  HardDrive,
  Heart,
  MemoryStick,
  Plug,
  Radio,
  Server,
  Shield,
} from 'lucide-react'
import { MetricTile } from '@/components/metric-tile'
import { MetricsChart } from '@/components/metrics-chart'
import { StatusTile } from '@/components/status-tile'
import { ResourcesBlock } from '@/components/resources-block'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_SECTION_TABS } from '@/components/header'
import { formatCount, formatMB, formatMHz } from '@/lib/format'
import { useClusterStore } from '@/store/cluster'

// Cluster overview — the model's top page. Three concentric layers:
// the 4 base resource tiles, the 4 status tiles, and the Asty + NATS
// sub-blocks. The nodes table and cluster logs that used to live here
// have moved to /nodes and /logs respectively.
export default function Cluster() {
  const subscribeCluster = useClusterStore((s) => s.subscribeCluster)
  const nodes = useClusterStore((s) => s.nodes)
  const services = useClusterStore((s) => s.services)
  const clusterStatus = useClusterStore((s) => s.clusterStatus)
  const clusterCpuMetrics = useClusterStore((s) => s.clusterCpuMetrics)
  const clusterMemoryMetrics = useClusterStore((s) => s.clusterMemoryMetrics)
  const clusterRpsMetrics = useClusterStore((s) => s.clusterRpsMetrics)

  useEffect(() => subscribeCluster(), [subscribeCluster])

  const aggregates = useMemo(() => {
    let cpuT = 0, cpuA = 0, memT = 0, memA = 0, diskT = 0, diskA = 0
    let selfCPU = 0, selfMem = 0, selfDisk = 0
    let natsCPU = 0, natsMem = 0, natsConn = 0, natsDisk = 0
    let natsSubs = 0, natsSlow = 0, natsIn = 0, natsOut = 0, natsJSMsgs = 0
    for (const n of nodes) {
      cpuT += n.cpu_total
      cpuA += n.cpu_available
      memT += n.memory_total
      memA += n.memory_available
      diskT += n.disk_total
      diskA += n.disk_available
      selfCPU += n.self_cpu_percent
      selfMem += n.self_memory_mb
      selfDisk += n.self_disk_mb
      natsCPU += n.nats_cpu_percent
      natsMem += n.nats_memory_mb
      natsConn += n.nats_connections
      natsDisk += n.nats_disk_mb
      natsSubs += n.nats_subscriptions
      natsSlow += n.nats_slow_consumers
      natsIn += n.nats_in_msgs
      natsOut += n.nats_out_msgs
      natsJSMsgs += n.nats_jetstream_messages
    }
    const lastRps = clusterRpsMetrics.length ? clusterRpsMetrics[clusterRpsMetrics.length - 1].value : 0
    return {
      cluster: { cpuUsage: cpuT - cpuA, cpuTotal: cpuT, memoryUsage: memT - memA, memoryTotal: memT, diskUsage: diskT - diskA, diskTotal: diskT, rps: lastRps },
      asty:    { cpuUsage: selfCPU, cpuTotal: 100 * nodes.length, memoryUsage: selfMem, memoryTotal: memT, diskUsage: selfDisk, diskTotal: diskT },
      nats:    {
        cpuUsage: natsCPU, cpuTotal: 100 * nodes.length,
        memoryUsage: natsMem, memoryTotal: memT,
        diskUsage: natsDisk, diskTotal: diskT,
        connections: natsConn, subscriptions: natsSubs, slow: natsSlow,
        inMsgs: natsIn, outMsgs: natsOut, jsMessages: natsJSMsgs,
      },
    }
  }, [nodes, clusterRpsMetrics])

  // hasNats — same opt-in gate as the node-detail page: NATS
  // monitoring (-m 8222) is optional, so an all-zeros aggregate
  // means "no data" rather than "NATS is dead". Hide the entire
  // block in that case.
  const hasNats = aggregates.nats.cpuUsage > 0 || aggregates.nats.memoryUsage > 0 ||
    aggregates.nats.connections > 0 || aggregates.nats.subscriptions > 0 ||
    aggregates.nats.inMsgs > 0 || aggregates.nats.outMsgs > 0 ||
    aggregates.nats.jsMessages > 0 || aggregates.nats.diskUsage > 0

  const services_active = services.filter((s) => (s.current_copies ?? 0) > 0).length
  const nodesHealthy = clusterStatus?.cluster.nodes_healthy ?? 0
  const nodesTotal = clusterStatus?.cluster.nodes_total ?? 0
  const healthPct = nodesTotal > 0 ? Math.round((nodesHealthy / nodesTotal) * 100) : 0

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-6">
      <ResourceTabs items={CLUSTER_SECTION_TABS} />

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Cluster</h2>
        <div className="grid grid-cols-12 gap-3">
          <MetricTile className="col-span-6 lg:col-span-3"
            title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={aggregates.cluster.cpuUsage} total={aggregates.cluster.cpuTotal}
            unit="" format={formatMHz} />
          <MetricTile className="col-span-6 lg:col-span-3"
            title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={aggregates.cluster.memoryUsage} total={aggregates.cluster.memoryTotal}
            unit="" format={formatMB} />
          <MetricTile className="col-span-6 lg:col-span-3"
            title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={aggregates.cluster.diskUsage} total={aggregates.cluster.diskTotal}
            unit="" format={formatMB} />
          <StatusTile className="col-span-6 lg:col-span-3"
            title="RPS" icon={<Activity className="h-4 w-4" />}
            value={Math.round(aggregates.cluster.rps)} hint="Requests per second" />

          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster CPU" data={clusterCpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster Memory" data={clusterMemoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Cluster RPS" data={clusterRpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <StatusTile className="col-span-6 lg:col-span-3"
            title="Nodes" icon={<Server className="h-4 w-4" />}
            value={`${nodesHealthy} / ${nodesTotal}`} hint="active / total" />
          <StatusTile className="col-span-6 lg:col-span-3"
            title="Services" icon={<Boxes className="h-4 w-4" />}
            value={`${services_active} / ${services.length}`} hint="active / loaded" />
          <StatusTile className="col-span-6 lg:col-span-3"
            title="Leader" icon={<Shield className="h-4 w-4" />}
            value={<span className="text-base font-mono">{clusterStatus?.cluster.leader || '—'}</span>}
            hint={clusterStatus?.cluster.leader_ip || '—'} />
          <StatusTile className="col-span-6 lg:col-span-3"
            title="Health" icon={<Heart className="h-4 w-4" />}
            value={`${healthPct}%`} hint={`${nodesHealthy} of ${nodesTotal} healthy`} />
        </div>
      </section>

      <ResourcesBlock title="Asty" data={aggregates.asty} />

      {hasNats && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold">NATS</h2>
          <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
            <MetricTile title="CPU" icon={<Cpu className="h-4 w-4" />}
              usage={aggregates.nats.cpuUsage} total={aggregates.nats.cpuTotal}
              unit="" format={formatMHz} />
            <MetricTile title="Memory" icon={<MemoryStick className="h-4 w-4" />}
              usage={aggregates.nats.memoryUsage} total={aggregates.nats.memoryTotal}
              unit="" format={formatMB} />
            <MetricTile title="Disk" icon={<HardDrive className="h-4 w-4" />}
              usage={aggregates.nats.diskUsage} total={aggregates.nats.diskTotal}
              unit="" format={formatMB} />
            <StatusTile title="Connections" icon={<Plug className="h-4 w-4" />}
              value={aggregates.nats.connections} hint="current clients" />
            <StatusTile title="Subscriptions" icon={<Radio className="h-4 w-4" />}
              value={aggregates.nats.subscriptions} hint="active subjects" />
            <StatusTile title="Slow Consumers" icon={<AlertTriangle className="h-4 w-4" />}
              value={aggregates.nats.slow} hint="lifetime count" />
            <StatusTile title="Incoming Messages" icon={<ArrowDown className="h-4 w-4" />}
              value={formatCount(aggregates.nats.inMsgs)} hint="since NATS start" />
            <StatusTile title="Outgoing Messages" icon={<ArrowUp className="h-4 w-4" />}
              value={formatCount(aggregates.nats.outMsgs)} hint="since NATS start" />
            <StatusTile title="JetStream Messages" icon={<Database className="h-4 w-4" />}
              value={formatCount(aggregates.nats.jsMessages)} hint="JetStream total" />
          </div>
        </section>
      )}
    </div>
  )
}
