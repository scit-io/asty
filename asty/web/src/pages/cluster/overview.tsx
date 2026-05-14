import { useMemo } from 'react'
import { Server, Activity, Shield, Heart } from 'lucide-react'
import { MetricsChart } from '@/components/metrics-chart'
import { StatusTile } from '@/components/status-tile'
import { ResourcesBlock } from '@/components/resources-block'
import { ResourceTabs } from '@/components/resource-tabs'
import { CLUSTER_SECTION_TABS } from '@/components/header'
import { useClusterStore } from '@/store/cluster'

// Cluster overview — the model's top page. Three concentric layers:
// the 4 base resource tiles, the 4 status tiles, and the Asty + NATS
// sub-blocks. The nodes table and cluster logs that used to live here
// have moved to /nodes and /logs respectively.
export default function Cluster() {
  const nodes = useClusterStore((s) => s.nodes)
  const services = useClusterStore((s) => s.services)
  const clusterStatus = useClusterStore((s) => s.clusterStatus)
  const clusterCpuMetrics = useClusterStore((s) => s.clusterCpuMetrics)
  const clusterMemoryMetrics = useClusterStore((s) => s.clusterMemoryMetrics)
  const clusterRpsMetrics = useClusterStore((s) => s.clusterRpsMetrics)

  const aggregates = useMemo(() => {
    let cpuT = 0, cpuA = 0, memT = 0, memA = 0, diskT = 0, diskA = 0
    let selfCPU = 0, selfMem = 0, selfDisk = 0
    let natsCPU = 0, natsMem = 0, natsConn = 0, natsJSBytes = 0
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
      natsJSBytes += n.nats_jetstream_bytes
    }
    const lastRps = clusterRpsMetrics.length ? clusterRpsMetrics[clusterRpsMetrics.length - 1].value : 0
    return {
      cluster: { cpuUsage: cpuT - cpuA, cpuTotal: cpuT, memoryUsage: memT - memA, memoryTotal: memT, diskUsage: diskT - diskA, diskTotal: diskT, rps: lastRps },
      asty:    { cpuUsage: selfCPU, cpuTotal: 100 * nodes.length, memoryUsage: selfMem, memoryTotal: memT, diskUsage: selfDisk, diskTotal: diskT },
      nats:    { cpuUsage: natsCPU, cpuTotal: 100 * nodes.length, memoryUsage: natsMem, memoryTotal: memT, diskUsage: Math.round(natsJSBytes / (1024 * 1024)), diskTotal: diskT, connections: natsConn },
    }
  }, [nodes, clusterRpsMetrics])

  const services_active = services.filter((s) => (s.current_copies ?? 0) > 0).length
  const nodesHealthy = clusterStatus?.cluster.nodes_healthy ?? 0
  const nodesTotal = clusterStatus?.cluster.nodes_total ?? 0
  const healthPct = nodesTotal > 0 ? Math.round((nodesHealthy / nodesTotal) * 100) : 0

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-6">
      <ResourceTabs items={CLUSTER_SECTION_TABS} />

      <ResourcesBlock title="Cluster" data={aggregates.cluster} />

      <div className="grid gap-3 md:grid-cols-3">
        <MetricsChart title="Cluster CPU" data={clusterCpuMetrics} color="hsl(var(--chart-1))" />
        <MetricsChart title="Cluster Memory" data={clusterMemoryMetrics} color="hsl(var(--chart-2))" />
        <MetricsChart title="Cluster RPS" data={clusterRpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />
      </div>

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusTile title="Nodes" icon={<Server className="h-4 w-4" />}
          value={`${nodesHealthy} / ${nodesTotal}`} hint="active / total" />
        <StatusTile title="Services" icon={<Activity className="h-4 w-4" />}
          value={`${services_active} / ${services.length}`} hint="active / loaded" />
        <StatusTile title="Leader" icon={<Shield className="h-4 w-4" />}
          value={<span className="text-base font-mono">{clusterStatus?.cluster.leader || '—'}</span>}
          hint={clusterStatus?.cluster.leader_ip || '—'} />
        <StatusTile title="Health" icon={<Heart className="h-4 w-4" />}
          value={`${healthPct}%`} hint={`${nodesHealthy} of ${nodesTotal} healthy`} />
      </div>

      <ResourcesBlock title="Asty" data={aggregates.asty} />
      <ResourcesBlock title="NATS"
        data={{ ...aggregates.nats, rps: undefined }} />

      <p className="text-xs text-muted-foreground">
        NATS connections across cluster: {aggregates.nats.connections}.
        Per-node msg-rate available via Prometheus query: <code>rate(asty_node_nats_in_msgs_total[1m])</code>.
      </p>
    </div>
  )
}
