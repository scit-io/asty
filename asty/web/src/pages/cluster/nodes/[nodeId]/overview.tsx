import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Switch } from '@/components/ui/switch'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Clock,
  Cpu,
  Database,
  HardDrive,
  Layers,
  MemoryStick,
  Plug,
  Radio,
  Signal,
  Wrench,
} from 'lucide-react'
import { toast } from 'sonner'
import { formatCount, formatMB, formatMHz } from '@/lib/format'
import { MetricsChart } from '@/components/metrics-chart'
import { NodeHeader } from '@/components/node-header'
import { ResourceTabs } from '@/components/resource-tabs'
import { ResourcesBlock } from '@/components/resources-block'
import { Tile } from '@/components/tile'
import { api } from '@/api/client'
import { useClusterStore } from '@/store/cluster'

// Node Overview (/nodes/:id) — first tab of the node section.
// Maintenance section (drain switch) + Asty/NATS sub-blocks land
// here; allocations and logs moved to their own routes.
export default function NodeDetail() {
  const { nodeId } = useParams<{ nodeId: string }>()
  const { nodeCache, subscribeNode, updateNodeStatus } = useClusterStore()
  const cached = nodeId ? nodeCache[nodeId] : undefined
  const node = cached?.node || null
  const cpuMetrics = cached?.cpuMetrics || []
  const memoryMetrics = cached?.memoryMetrics || []
  const rpsMetrics = cached?.rpsMetrics || []
  const [showDrainDialog, setShowDrainDialog] = useState(false)

  useEffect(() => {
    if (!nodeId) return
    return subscribeNode(nodeId)
  }, [nodeId, subscribeNode])

  const handleDrain = async (enable: boolean) => {
    if (!nodeId) return
    try {
      await api.drainNode(nodeId, enable)
      updateNodeStatus(nodeId, enable ? 'draining' : 'ready')
      toast.success(enable ? 'Draining node' : 'Node resumed')
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    }
  }

  if (!node) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <Skeleton className="h-8 w-64 mb-4" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  const rps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  const draining = node.status === 'draining' || node.status === 'drained'
  const diskUnit = node.disk_type === 'ssd' || node.disk_type === 'hdd' ? node.disk_type.toUpperCase() : ''
  const drainHint = node.status === 'ready' ? 'Normal'
    : node.status === 'draining' ? 'Migrating…'
    : node.status === 'drained' ? 'Drained'
    : node.status

  // hasAsty / hasNats decide whether to render the agent and NATS
  // sub-blocks. NATS monitoring (-m 8222) is opt-in; without it every
  // nats_* field is zero, so an "all zeros" reading means "no NATS to
  // show" rather than "NATS is dead". Asty is always present on a
  // running agent, but we still gate on at least one non-zero sample
  // so a fresh node doesn't render an all-zero block before the first
  // heartbeat.
  const hasAsty = node.self_cpu_percent > 0 || node.self_memory_mb > 0 || node.self_disk_mb > 0
  const hasNats = node.nats_cpu_percent > 0 || node.nats_memory_mb > 0 ||
    node.nats_connections > 0 || node.nats_subscriptions > 0 ||
    node.nats_in_msgs > 0 || node.nats_out_msgs > 0 ||
    node.nats_jetstream_messages > 0 || node.nats_jetstream_bytes > 0

  return (
    <div className="container mx-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
      <NodeHeader node={node} />

      <ResourceTabs items={[
        { to: `/nodes/${node.id}`, label: 'Overview' },
        { to: `/nodes/${node.id}/allocations`, label: 'Allocations' },
        { to: `/nodes/${node.id}/logs`, label: 'Logs' },
      ]} />

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Node</h2>
        <div className="grid grid-cols-12 gap-3">
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="CPU" icon={<Cpu className="h-4 w-4" />}
            usage={node.cpu_total - node.cpu_available} total={node.cpu_total} format={formatMHz} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Memory" icon={<MemoryStick className="h-4 w-4" />}
            usage={node.memory_total - node.memory_available} total={node.memory_total} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="metric"
            title="Disk" icon={<HardDrive className="h-4 w-4" />}
            usage={node.disk_total - node.disk_available} total={node.disk_total}
            unit={diskUnit} format={formatMB} />
          <Tile className="col-span-6 lg:col-span-3" variant="stat" bar
            title="RPS" icon={<Activity className="h-4 w-4" />}
            value={Math.round(rps)} hint="Requests per second" />

          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node CPU" data={cpuMetrics} color="hsl(var(--chart-1))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node Memory" data={memoryMetrics} color="hsl(var(--chart-2))" />
          <MetricsChart className="col-span-12 md:col-span-4"
            title="Node RPS" data={rpsMetrics} color="hsl(var(--chart-3))" unit=" rps" />

          <Tile className="col-span-6 lg:col-span-3" variant="stat"
            title="Allocations" icon={<Layers className="h-4 w-4" />}
            value={`${node.allocations_running} / ${node.allocations_planned}`} hint="running / planned" />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title="Started At" icon={<Clock className="h-4 w-4" />}
            timestamp={node.created_at} />
          <Tile className="col-span-6 lg:col-span-3" variant="timestamp"
            title="Last Heartbeat" icon={<Signal className="h-4 w-4" />}
            timestamp={node.last_seen} />
          <Tile className="col-span-6 lg:col-span-3" variant="actions"
            title="Maintenance" icon={<Wrench className="h-4 w-4" />}
            hint={drainHint}
            actions={
              <>
                <div className="text-sm font-bold">Drain</div>
                <Switch
                  checked={draining}
                  onCheckedChange={(checked) => checked ? setShowDrainDialog(true) : handleDrain(false)}
                  disabled={node.status === 'draining'}
                />
              </>
            } />
        </div>
      </section>

      {hasAsty && (
        <ResourcesBlock title="Asty"
          data={{
            cpuUsage: node.self_cpu_percent, cpuTotal: 100,
            memoryUsage: node.self_memory_mb, memoryTotal: node.memory_total,
            diskUsage: node.self_disk_mb, diskTotal: node.disk_total,
          }} />
      )}

      {hasNats && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold">NATS</h2>
          <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
            <Tile variant="metric" title="CPU" icon={<Cpu className="h-4 w-4" />}
              usage={node.nats_cpu_percent} total={100} format={formatMHz} />
            <Tile variant="metric" title="Memory" icon={<MemoryStick className="h-4 w-4" />}
              usage={node.nats_memory_mb} total={node.memory_total} format={formatMB} />
            <Tile variant="metric" title="Disk" icon={<HardDrive className="h-4 w-4" />}
              usage={node.nats_disk_mb} total={node.disk_total} format={formatMB} />
            <Tile variant="stat" title="Connections" icon={<Plug className="h-4 w-4" />}
              value={node.nats_connections} hint="current clients" />
            <Tile variant="stat" title="Subscriptions" icon={<Radio className="h-4 w-4" />}
              value={node.nats_subscriptions} hint="active subjects" />
            <Tile variant="stat" title="Slow Consumers" icon={<AlertTriangle className="h-4 w-4" />}
              value={node.nats_slow_consumers} hint="lifetime count" />
            <Tile variant="stat" title="Incoming Messages" icon={<ArrowDown className="h-4 w-4" />}
              value={formatCount(node.nats_in_msgs)} hint="since NATS start" />
            <Tile variant="stat" title="Outgoing Messages" icon={<ArrowUp className="h-4 w-4" />}
              value={formatCount(node.nats_out_msgs)} hint="since NATS start" />
            <Tile variant="stat" title="JetStream Messages" icon={<Database className="h-4 w-4" />}
              value={formatCount(node.nats_jetstream_messages)} hint="JetStream total" />
          </div>
        </section>
      )}

      <AlertDialog open={showDrainDialog} onOpenChange={setShowDrainDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain node {node.id}?</AlertDialogTitle>
            <AlertDialogDescription>
              All running allocations on this node will be migrated to peers and the node will stop accepting new placements.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => { setShowDrainDialog(false); handleDrain(true) }}>
              Drain
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
